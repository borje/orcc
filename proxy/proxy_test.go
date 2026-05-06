package proxy

import (
	"encoding/json"
	"testing"
)

func TestInjectWebSearch(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantURL  string
		wantTool string
		wantTC   string
	}{
		{
			name:     "no tools",
			input:    `{"model":"test"}`,
			wantURL:  "",
			wantTool: "",
			wantTC:   "",
		},
		{
			name:     "no web_search",
			input:    `{"model":"test","tools":[{"name":"foo","input_schema":{}}]}`,
			wantURL:  "",
			wantTool: "",
			wantTC:   "",
		},
		{
			name:     "web_search found",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}}]}`,
			wantURL:  "openrouter:web_search",
			wantTool: "openrouter:web_search",
			wantTC:   "",
		},
		{
			name:     "web_search with other tools",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}},{"name":"bash","input_schema":{}}]}`,
			wantURL:  "openrouter:web_search",
			wantTool: "openrouter:web_search",
			wantTC:   "",
		},
		{
			name:     "tool_choice set to web_search",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}}],"tool_choice":{"type":"tool","name":"web_search"}}`,
			wantURL:  "openrouter:web_search",
			wantTool: "openrouter:web_search",
			wantTC:   "null",
		},
		{
			name:     "tool_choice set to other",
			input:    `{"model":"test","tools":[{"name":"web_search","input_schema":{}}],"tool_choice":{"type":"tool","name":"bash"}}`,
			wantURL:  "openrouter:web_search",
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

			if tc.wantTC == "null" {
				tcRaw, ok := got["tool_choice"]
				if !ok {
					t.Errorf("tool_choice missing")
				} else {
					var tcVal interface{}
					json.Unmarshal(tcRaw, &tcVal)
					if tcVal != nil {
						t.Errorf("tool_choice = %v, want null", tcVal)
					}
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