package tui_test

import (
	"reflect"
	"testing"

	"super-agent/tui"
)

func TestExtractCodeBlocks(goTest *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "no blocks",
			content: "hello world",
			want:    nil,
		},
		{
			name:    "one block",
			content: "here is some code:\n```go\nfmt.Println(\"hi\")\n```\nmore text",
			want:    []string{"fmt.Println(\"hi\")"},
		},
		{
			name:    "multiple blocks",
			content: "```python\nprint(1)\n```\ntext\n```bash\necho 2\n```",
			want:    []string{"print(1)", "echo 2"},
		},
		{
			name:    "block with no language",
			content: "```\njust code\n```",
			want:    []string{"just code"},
		},
	}

	for _, tt := range tests {
		goTest.Run(tt.name, func(t *testing.T) {
			if got := tui.ExtractCodeBlocks(tt.content); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractCodeBlocks() = %v, want %v", got, tt.want)
			}
		})
	}
}
