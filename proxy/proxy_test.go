package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInjectWebSearch(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantTool string
		wantTC   string
	}{
		{
			name:     "no tools",
			input:    `{"model":"test"}`,
			wantTool: "",
			wantTC:   "",
		},
		{
			name:     "no web_search",
			input:    `{"model":"test","tools":[{"name":"foo","input_schema":{}}]}`,
			wantTool: "",
			wantTC:   "",
		},
		{
			name:     "web_search found",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}}]}`,
			wantTool: "openrouter:web_search",
			wantTC:   "",
		},
		{
			name:     "web_search with other tools",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}},{"name":"bash","input_schema":{}}]}`,
			wantTool: "openrouter:web_search",
			wantTC:   "",
		},
		{
			name:     "tool_choice set to web_search",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}}],"tool_choice":{"type":"tool","name":"web_search"}}`,
			wantTool: "openrouter:web_search",
			wantTC:   "deleted",
		},
		{
			name:     "tool_choice set to other",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}}],"tool_choice":{"type":"tool","name":"bash"}}`,
			wantTool: "openrouter:web_search",
			wantTC:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := injectWebSearch([]byte(tc.input))
			if err != nil {
				t.Fatalf("injectWebSearch: %v", err)
			}

			if tc.wantTool == "" {
				if changed {
					t.Errorf("expected unchanged, got changed")
				}
				return
			}

			if !changed {
				t.Fatalf("expected changed, got unchanged")
			}

			var got map[string]json.RawMessage
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("parse output: %v", err)
			}

			toolsRaw, ok := got["tools"]
			if !ok {
				t.Fatalf("no tools in output")
			}
			var tools []json.RawMessage
			json.Unmarshal(toolsRaw, &tools)

			foundORWS := false
			for _, tool := range tools {
				var m map[string]interface{}
				json.Unmarshal(tool, &m)
				if m["type"] == tc.wantTool {
					foundORWS = true
				}
			}
			if !foundORWS {
				t.Errorf("openrouter:web_search not found in tools")
			}

			for _, tool := range tools {
				var m map[string]interface{}
				json.Unmarshal(tool, &m)
				if m["name"] == "web_search" {
					t.Errorf("original web_search still present")
				}
			}

			if tc.wantTC == "deleted" {
				if _, ok := got["tool_choice"]; ok {
					t.Errorf("tool_choice present, want deleted")
				}
			}
		})
	}
}

func TestInjectWebSearchInvalidJSON(t *testing.T) {
	// Invalid JSON returns original body unchanged (no error)
	out, changed, err := injectWebSearch([]byte(`{invalid`))
	if err != nil {
		t.Fatalf("expected no error on invalid JSON, got: %v", err)
	}
	if changed {
		t.Errorf("expected changed=false on invalid JSON")
	}
	if string(out) != "{invalid" {
		t.Errorf("expected original body returned, got: %s", string(out))
	}
}

func TestFilterServerToolUseSSE(t *testing.T) {
	// Synthetic SSE stream with openrouter:web_search at index 0, text at index 1.
	input := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"id123","name":"openrouter:web_search","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"test\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

`
	var buf strings.Builder
	filterServerToolUseSSE(strings.NewReader(input), &buf, nil)
	got := buf.String()

	// server_tool_use block renamed, not dropped.
	if !strings.Contains(got, `"name":"web_search"`) {
		t.Error("expected server_tool_use renamed to web_search")
	}
	if strings.Contains(got, "openrouter:web_search") {
		t.Error("expected openrouter:web_search removed from stream")
	}

	// web_search_tool_result injected after server_tool_use stop.
	if !strings.Contains(got, "web_search_tool_result") {
		t.Error("expected web_search_tool_result block injected")
	}
	if !strings.Contains(got, `"tool_use_id":"id123"`) {
		t.Error("expected tool_use_id to match server_tool_use id")
	}

	// Text block re-indexed from 1 → 2.
	if !strings.Contains(got, `"index":2`) {
		t.Error("expected text block re-indexed to 2")
	}
	if strings.Contains(got, `"index":1,"content_block":{"type":"text"`) {
		t.Error("expected text block index shifted away from 1")
	}
}