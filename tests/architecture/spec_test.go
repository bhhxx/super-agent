package architecture_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"super-agent/runtime/machine"
)

// specDocument owns the transition graph. It is the spec: when the machine and
// this file disagree, the machine is wrong.
const specDocument = "docs/machine.md"

// anyState is the table's wildcard state. The machine stores global rules under
// the zero State value, so the table needs a name for it.
const anyState = "any"

// diagramExcludedEvents are accepted from every state and are deliberately not
// drawn, because enumerating them would add eighteen edges to the picture
// without adding information. See the note under the diagram.
var diagramExcludedEvents = map[string]bool{
	"ErrorOccurred":   true,
	"CancelRequested": true,
	"ResetRequested":  true,
}

// documentedEdge is one row of the transition table.
type documentedEdge struct {
	state string
	event string
	next  string
}

func (e documentedEdge) String() string { return e.state + " + " + e.event }

// machineStates is every state the machine declares, in declaration order.
func machineStates() []string {
	return []string{
		"Initializing",
		"Idle",
		"WaitingLLM",
		"AdvancingQueue",
		"WaitingApproval",
		"RunningTool",
	}
}

func readSpec(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repositoryRoot(t), filepath.FromSlash(specDocument))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the spec document: %v", err)
	}
	return string(content)
}

// parseTransitionTable extracts documentedState rows from the markdown table.
//
// It accepts a line only when it splits into exactly five cells whose first
// three are a known state (or "any"), an event name, and a next state. The
// other tables in the document have different cell counts or backticked first
// cells, so they are skipped without needing to locate the table by heading.
func parseTransitionTable(t *testing.T, document string) []documentedEdge {
	t.Helper()
	states := machineStates()
	known := make(map[string]bool, len(states)+1)
	for _, state := range states {
		known[state] = true
	}
	known[anyState] = true

	nameOnly := regexp.MustCompile(`^[A-Za-z]+$`)
	var edges []documentedEdge
	for lineNumber, line := range strings.Split(document, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 5 {
			continue
		}
		for index := range cells {
			cells[index] = strings.TrimSpace(cells[index])
		}
		if !known[cells[0]] || !nameOnly.MatchString(cells[1]) || !nameOnly.MatchString(cells[2]) {
			continue
		}
		if cells[1] == "Event" { // header row
			continue
		}
		if !known[cells[2]] {
			t.Fatalf("%s:%d: documented next state %q is not a declared state", specDocument, lineNumber+1, cells[2])
		}
		edges = append(edges, documentedEdge{state: cells[0], event: cells[1], next: cells[2]})
	}
	if len(edges) == 0 {
		t.Fatalf("%s: parsed no transition table rows; the table format changed", specDocument)
	}
	return edges
}

// parseStateDiagram extracts the stateDiagram edges from the document.
func parseStateDiagram(t *testing.T, document string) []documentedEdge {
	t.Helper()
	inDiagram := false
	edgeLine := regexp.MustCompile(`^\s*([A-Za-z]+)\s*-->\s*([A-Za-z]+)\s*:\s*(.+?)\s*$`)

	var edges []documentedEdge
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inDiagram = strings.TrimSpace(line) == "```mermaid" && !inDiagram
			continue
		}
		if !inDiagram {
			continue
		}
		match := edgeLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		// "ApprovalGranted / ApprovalAlwaysGranted" is two edges on one line.
		for _, event := range strings.Split(match[3], "/") {
			edges = append(edges, documentedEdge{
				state: match[1],
				event: strings.TrimSpace(event),
				next:  match[2],
			})
		}
	}
	if len(edges) == 0 {
		t.Fatalf("%s: parsed no state diagram edges; the diagram format changed", specDocument)
	}
	return edges
}

func sampleToolCall() machine.ToolCall {
	return machine.ToolCall{ID: "call-1", Name: "bash", Input: "pwd"}
}

func sampleToolCalls() []machine.ToolCall {
	return []machine.ToolCall{
		{ID: "call-1", Name: "first", Input: "a"},
		{ID: "call-2", Name: "second", Input: "b"},
	}
}

// wellFormedEvent returns an event carrying the content its handler validates.
//
// machine.AllEvents holds zero values, which would make ToolBatchReceived fail
// its empty-batch guard and ToolResultReceived fail its call guard. A rejection
// must mean "the table does not document this edge", never "the sample was
// malformed". Mirrors the event handling in transitionSnapshot, see
// tests/runtime/transition_test.go.
func wellFormedEvent(prototype machine.Event) machine.Event {
	call := sampleToolCall()
	switch prototype.(type) {
	case machine.UserMessageSubmitted:
		return machine.UserMessageSubmitted{Content: "hi"}
	case machine.AssistantMessageReceived:
		return machine.AssistantMessageReceived{Response: machine.ModelResponse{Content: "hi"}}
	case machine.ToolBatchReceived:
		return machine.ToolBatchReceived{Content: "thinking", Calls: sampleToolCalls()}
	case machine.ToolCallNeedsApproval:
		return machine.ToolCallNeedsApproval{Call: call}
	case machine.ToolCallReadyToRun:
		return machine.ToolCallReadyToRun{Call: call}
	case machine.ToolCallDenied:
		return machine.ToolCallDenied{Call: call, Reason: "plan mode"}
	case machine.ToolResultReceived:
		return machine.ToolResultReceived{Call: call, Result: "ok"}
	case machine.ApprovalGranted:
		return machine.ApprovalGranted{Call: call}
	case machine.ApprovalAlwaysGranted:
		return machine.ApprovalAlwaysGranted{Call: call}
	case machine.ApprovalDenied:
		return machine.ApprovalDenied{Call: call}
	case machine.ErrorOccurred:
		return machine.ErrorOccurred{Err: errors.New("boom")}
	default:
		// EngineReady, ToolBatchFinished, CancelRequested, ResetRequested carry no fields.
		return prototype
	}
}

// stateSnapshot builds runtime data that satisfies the invariants of state and
// the preconditions of event, so that calling Transition measures the edge
// registry rather than guard setup.
//
// Mirrors transitionSnapshot in tests/runtime/transition_test.go; the logic is
// duplicated because external test packages cannot share helpers.
func stateSnapshot(stateName string, event machine.Event) machine.MachineSnapshot {
	data := machine.RuntimeData{State: machine.State(stateName)}
	call := sampleToolCall()
	switch typed := event.(type) {
	case machine.ApprovalGranted:
		call = typed.Call
	case machine.ApprovalAlwaysGranted:
		call = typed.Call
	case machine.ApprovalDenied:
		call = typed.Call
	case machine.ToolResultReceived:
		call = typed.Call
	case machine.ToolCallNeedsApproval:
		call = typed.Call
	case machine.ToolCallReadyToRun:
		call = typed.Call
	case machine.ToolCallDenied:
		call = typed.Call
	}
	switch data.State {
	case machine.StateAdvancingQueue:
		data.ToolBatch = &machine.ToolCallBatch{Calls: []machine.ToolCall{call}}
		if _, finished := event.(machine.ToolBatchFinished); finished {
			// ToolBatchFinished requires an exhausted queue.
			data.ToolBatch.Index = 1
		}
	case machine.StateWaitingApproval:
		request := machine.PermissionRequest{}
		data.PendingTool = &call
		data.PendingPermission = &request
		data.ToolBatch = &machine.ToolCallBatch{Calls: []machine.ToolCall{call}, Index: 1}
	case machine.StateRunningTool:
		data.CurrentTool = &call
		data.ToolBatch = &machine.ToolCallBatch{Calls: []machine.ToolCall{call}, Index: 1}
	}
	snapshot, err := machine.SnapshotFrom(data)
	if err != nil {
		panic(fmt.Sprintf("invalid %s snapshot for %s: %v", stateName, reflect.TypeOf(event).Name(), err))
	}
	return snapshot
}

// observedEdges runs every state against every declared event and records which
// pairs the machine accepts, and where they lead.
func observedEdges() map[string]string {
	observed := make(map[string]string)
	for _, state := range machineStates() {
		for _, prototype := range machine.AllEvents {
			event := wellFormedEvent(prototype)
			result, err := machine.Transition(stateSnapshot(state, event), event)
			if err != nil {
				continue
			}
			observed[documentedEdge{state: state, event: reflect.TypeOf(event).Name()}.String()] = string(result.NextState)
		}
	}
	return observed
}

// documentedEdges expands the table into one entry per concrete state.
func documentedEdges(rows []documentedEdge) (map[string]string, map[string]bool) {
	expanded := make(map[string]string)
	stateSpecific := make(map[string]bool)
	for _, row := range rows {
		if row.state == anyState {
			for _, state := range machineStates() {
				expanded[documentedEdge{state: state, event: row.event}.String()] = row.next
			}
			continue
		}
		key := row.String()
		expanded[key] = row.next
		stateSpecific[key] = true
	}
	return expanded, stateSpecific
}

func describe(edges map[string]string) string {
	keys := make([]string, 0, len(edges))
	for key := range edges {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n  ")
}

// TestDocumentedTransitionTableMatchesMachine pins the spec's transition table
// to the machine's real behaviour. It fails in both directions: an edge that is
// documented but not implemented, and an edge that is implemented but not
// documented.
func TestDocumentedTransitionTableMatchesMachine(t *testing.T) {
	document := readSpec(t)
	rows := parseTransitionTable(t, document)
	documented, _ := documentedEdges(rows)
	observed := observedEdges()

	for key, want := range documented {
		got, ok := observed[key]
		if !ok {
			t.Errorf("%s: documented but the machine rejects this event", key)
			continue
		}
		if got != want {
			t.Errorf("%s: documented next state %s, machine produced %s", key, want, got)
		}
	}
	for key := range observed {
		if _, ok := documented[key]; !ok {
			t.Errorf("%s: accepted by the machine but missing from the transition table", key)
		}
	}
	if t.Failed() {
		t.Logf("documented edges:\n  %s", describe(documented))
		t.Logf("machine edges:\n  %s", describe(observed))
	}
}

// TestDocumentedStateDiagramMatchesTransitionTable keeps the two views in
// docs/machine.md from drifting apart.
func TestDocumentedStateDiagramMatchesTransitionTable(t *testing.T) {
	document := readSpec(t)
	rows := parseTransitionTable(t, document)
	_, stateSpecific := documentedEdges(rows)

	diagram := make(map[string]bool)
	for _, edge := range parseStateDiagram(t, document) {
		key := edge.String()
		if diagram[key] {
			t.Errorf("%s: drawn twice in the state diagram", key)
		}
		if diagramExcludedEvents[edge.event] {
			t.Errorf("%s: global events must not be drawn; the table defines them", key)
		}
		diagram[key] = true
	}

	for key := range stateSpecific {
		if !diagram[key] {
			t.Errorf("%s: in the transition table but missing from the state diagram", key)
		}
	}
	for key := range diagram {
		if !stateSpecific[key] {
			t.Errorf("%s: in the state diagram but missing from the transition table", key)
		}
	}
}
