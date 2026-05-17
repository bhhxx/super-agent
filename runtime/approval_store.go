package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
)

type ApprovalKey struct {
	ToolName  string
	InputHash string
}

type ApprovalStore interface {
	AllowAlways(key ApprovalKey)
	IsAlwaysAllowed(key ApprovalKey) bool
	SetAutoApproveTools(enabled bool)
	AutoApproveTools() bool
}

type MemoryApprovalStore struct {
	mu               sync.Mutex
	always           map[ApprovalKey]bool
	autoApproveTools bool
}

func NewMemoryApprovalStore() *MemoryApprovalStore {
	return &MemoryApprovalStore{always: make(map[ApprovalKey]bool)}
}

func (s *MemoryApprovalStore) AllowAlways(key ApprovalKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.always == nil {
		s.always = make(map[ApprovalKey]bool)
	}
	s.always[key] = true
}

func (s *MemoryApprovalStore) IsAlwaysAllowed(key ApprovalKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.always[key]
}

func (s *MemoryApprovalStore) SetAutoApproveTools(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoApproveTools = enabled
}

func (s *MemoryApprovalStore) AutoApproveTools() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoApproveTools
}

func NewApprovalKey(call ToolCall) ApprovalKey {
	return ApprovalKey{ToolName: call.Name, InputHash: hashCanonicalInput(call.Input)}
}

func hashCanonicalInput(input string) string {
	var value any
	canonical := input
	if err := json.Unmarshal([]byte(input), &value); err == nil {
		if encoded, err := json.Marshal(value); err == nil {
			canonical = string(encoded)
		}
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
