package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orcc/config"
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

func TestExtractParetoScore(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantModel string
		wantScore float64
		wantSet   bool
	}{
		{
			name:      "numeric score",
			input:     `{"model":"openrouter/pareto-code:0.8","messages":[]}`,
			wantModel: "openrouter/pareto-code",
			wantScore: 0.8,
			wantSet:   true,
		},
		{
			name:      "high alias",
			input:     `{"model":"openrouter/pareto-code:high","messages":[]}`,
			wantModel: "openrouter/pareto-code",
			wantScore: 0.9,
			wantSet:   true,
		},
		{
			name:      "low alias",
			input:     `{"model":"openrouter/pareto-code:low","messages":[]}`,
			wantModel: "openrouter/pareto-code",
			wantScore: 0.5,
			wantSet:   true,
		},
		{
			name:      "mid alias",
			input:     `{"model":"openrouter/pareto-code:mid","messages":[]}`,
			wantModel: "openrouter/pareto-code",
			wantScore: 0.7,
			wantSet:   true,
		},
		{
			name:      "medium alias",
			input:     `{"model":"openrouter/pareto-code:medium","messages":[]}`,
			wantModel: "openrouter/pareto-code",
			wantScore: 0.7,
			wantSet:   true,
		},
		{
			name:    "no suffix",
			input:   `{"model":"openrouter/pareto-code","messages":[]}`,
			wantSet: false,
		},
		{
			name:    "nitro suffix untouched",
			input:   `{"model":"openrouter/pareto-code:nitro","messages":[]}`,
			wantSet: false,
		},
		{
			name:    "non-pareto model untouched",
			input:   `{"model":"anthropic/claude-sonnet-4.6","messages":[]}`,
			wantSet: false,
		},
		{
			name:    "unknown suffix untouched",
			input:   `{"model":"openrouter/pareto-code:wat","messages":[]}`,
			wantSet: false,
		},
		{
			name:    "out-of-range numeric untouched",
			input:   `{"model":"openrouter/pareto-code:1.5","messages":[]}`,
			wantSet: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := extractParetoScore([]byte(tc.input))
			if err != nil {
				t.Fatalf("extractParetoScore: %v", err)
			}

			if !tc.wantSet {
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

			gotModel := jsonStr(got["model"])
			if gotModel != tc.wantModel {
				t.Errorf("model: got %q, want %q", gotModel, tc.wantModel)
			}

			var plugins []map[string]json.RawMessage
			if err := json.Unmarshal(got["plugins"], &plugins); err != nil || len(plugins) == 0 {
				t.Fatalf("plugins missing or unparseable")
			}
			p := plugins[len(plugins)-1]
			if id := jsonStr(p["id"]); id != "pareto-router" {
				t.Errorf("plugin id: got %q, want %q", id, "pareto-router")
			}
			var gotScore float64
			if err := json.Unmarshal(p["min_coding_score"], &gotScore); err != nil {
				t.Fatalf("parse min_coding_score from plugin: %v", err)
			}
			if gotScore != tc.wantScore {
				t.Errorf("min_coding_score: got %v, want %v", gotScore, tc.wantScore)
			}
		})
	}
}

func TestExtractParetoScoreInvalidJSON(t *testing.T) {
	out, changed, err := extractParetoScore([]byte(`{invalid`))
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

// ── Request optimization tests ─────────────────────────────────────────────────

func TestIsQuotaCheck(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "quota check request",
			body: `{"model":"test","max_tokens":1,"messages":[{"role":"user","content":"quota"}]}`,
			want: true,
		},
		{
			name: "not quota - max_tokens > 1",
			body: `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"quota"}]}`,
			want: false,
		},
		{
			name: "not quota - no quota text",
			body: `{"model":"test","max_tokens":1,"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "quota case insensitive",
			body: `{"model":"test","max_tokens":1,"messages":[{"role":"user","content":"QUOTA"}]}`,
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := parseRequest([]byte(tc.body))
			if req == nil {
				t.Fatal("parseRequest failed")
			}
			got := isQuotaCheck(req)
			if got != tc.want {
				t.Errorf("isQuotaCheck = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsTitleGeneration(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "sentence-case title",
			body: `{"model":"test","messages":[{"role":"user","content":"hello"}],"system":"Give a sentence-case title for this session"}`,
			want: true,
		},
		{
			name: "return json with field and coding session",
			body: `{"model":"test","messages":[{"role":"user","content":"hello"}],"system":"Return JSON with a title field for this coding session"}`,
			want: true,
		},
		{
			name: "not title - has tools",
			body: `{"model":"test","messages":[{"role":"user","content":"hello"}],"system":"Give a sentence-case title","tools":[{"name":"bash"}]}`,
			want: false,
		},
		{
			name: "not title - no system",
			body: `{"model":"test","messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "not title - no title keyword",
			body: `{"model":"test","messages":[{"role":"user","content":"hello"}],"system":"Do something else"}`,
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := parseRequest([]byte(tc.body))
			if req == nil {
				t.Fatal("parseRequest failed")
			}
			got := isTitleGeneration(req)
			if got != tc.want {
				t.Errorf("isTitleGeneration = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsPrefixDetection(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantMatch bool
		wantCmd   string
	}{
		{
			name:      "policy_spec with command",
			body:      `{"model":"test","messages":[{"role":"user","content":"<policy_spec>...</policy_spec>\nSome stuff\nCommand: ls -la"}]}`,
			wantMatch: true,
			wantCmd:   "ls -la",
		},
		{
			name:      "no policy_spec",
			body:      `{"model":"test","messages":[{"role":"user","content":"Command: ls"}]}`,
			wantMatch: false,
			wantCmd:   "",
		},
		{
			name:      "no Command: line",
			body:      `{"model":"test","messages":[{"role":"user","content":"<policy_spec>...</policy_spec>"}]}`,
			wantMatch: false,
			wantCmd:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := parseRequest([]byte(tc.body))
			if req == nil {
				t.Fatal("parseRequest failed")
			}
			gotMatch, gotCmd := isPrefixDetection(req)
			if gotMatch != tc.wantMatch || gotCmd != tc.wantCmd {
				t.Errorf("isPrefixDetection = (%v, %q), want (%v, %q)", gotMatch, gotCmd, tc.wantMatch, tc.wantCmd)
			}
		})
	}
}

func TestIsSuggestionMode(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "suggestion mode active",
			body: `{"model":"test","messages":[{"role":"user","content":"[SUGGESTION MODE: some suggestion]"}]}`,
			want: true,
		},
		{
			name: "not suggestion mode",
			body: `{"model":"test","messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := parseRequest([]byte(tc.body))
			if req == nil {
				t.Fatal("parseRequest failed")
			}
			got := isSuggestionMode(req)
			if got != tc.want {
				t.Errorf("isSuggestionMode = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTryOptimizations(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantMatch bool
		wantBody  string
	}{
		{
			name:      "quota check",
			body:      `{"model":"test","max_tokens":1,"messages":[{"role":"user","content":"quota"}]}`,
			wantMatch: true,
			wantBody:  "Quota check passed.",
		},
		{
			name:      "title generation",
			body:      `{"model":"test","messages":[{"role":"user","content":"hello"}],"system":"Give a sentence-case title"}`,
			wantMatch: true,
			wantBody:  "Conversation",
		},
		{
			name:      "suggestion mode",
			body:      `{"model":"test","messages":[{"role":"user","content":"[SUGGESTION MODE: suggest]"}]}`,
			wantMatch: true,
			wantBody:  "",
		},
		{
			name:      "prefix detection",
			body:      `{"model":"test","messages":[{"role":"user","content":"<policy_spec>\nCommand: ls"}]}`,
			wantMatch: true,
			wantBody:  "ls",
		},
		{
			name:      "normal request - no match",
			body:      `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`,
			wantMatch: false,
			wantBody:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			matched := tryOptimizations([]byte(tc.body), rec)

			if matched != tc.wantMatch {
				t.Errorf("tryOptimizations = %v, want %v", matched, tc.wantMatch)
			}

			if tc.wantMatch {
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want 200", rec.Code)
				}
				ct := rec.Header().Get("Content-Type")
				if ct != "text/event-stream" {
					t.Errorf("Content-Type = %q, want text/event-stream", ct)
				}
				if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
					t.Errorf("response body does not contain %q\n%s", tc.wantBody, rec.Body.String())
				}
			}
		})
	}
}

func TestTryOptimizationsInvalidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	matched := tryOptimizations([]byte(`{invalid`), rec)
	if matched {
		t.Error("expected no match for invalid JSON")
	}
}

func TestConfiguredModelIDs(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "openrouter/free",
		Models: config.Models{
			Opus:     "anthropic/claude-opus-4",
			Sonnet:   "anthropic/claude-sonnet-4",
			Haiku:    "anthropic/claude-haiku-4",
			Subagent: "anthropic/claude-opus-4",
		},
	}
	p := New(cfg)
	ids := p.configuredModelIDs()

	expected := []string{"openrouter/free", "anthropic/claude-opus-4", "anthropic/claude-sonnet-4", "anthropic/claude-haiku-4"}
	if !equalSlice(ids, expected) {
		t.Errorf("configuredModelIDs = %v, want %v", ids, expected)
	}
}

func TestFallbackModels(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "openrouter/free",
		Models: config.Models{
			Opus:   "anthropic/claude-opus-4",
			Sonnet: "anthropic/claude-sonnet-4",
		},
	}
	p := New(cfg)
	models := p.fallbackModels()

	if len(models) != 3 {
		t.Errorf("expected 3 fallback models, got %d", len(models))
	}

	idSet := map[string]bool{}
	for _, m := range models {
		if m["type"] != "model" {
			t.Errorf("entry missing type=model: %v", m)
		}
		if m["id"] == "" {
			t.Error("entry has empty id")
		}
		idSet[m["id"]] = true
	}

	for _, id := range []string{"openrouter/free", "anthropic/claude-opus-4", "anthropic/claude-sonnet-4"} {
		if !idSet[id] {
			t.Errorf("fallback models missing %q", id)
		}
	}
}

func TestEnsureModelsFallsBackOnFetchFailure(t *testing.T) {
	// No API key set → FetchModels will fail → falls back to configured models.
	cfg := &config.Config{
		DefaultModel: "openrouter/free",
		Models: config.Models{
			Opus:  "anthropic/claude-opus-4",
			Haiku: "anthropic/claude-haiku-4",
		},
	}
	p := New(cfg)
	models := p.ensureModels()

	if len(models) == 0 {
		t.Fatal("expected at least configured models in fallback")
	}

	idSet := map[string]bool{}
	for _, m := range models {
		idSet[m["id"]] = true
	}

	for _, id := range []string{"openrouter/free", "anthropic/claude-opus-4", "anthropic/claude-haiku-4"} {
		if !idSet[id] {
			t.Errorf("fallback missing %q", id)
		}
	}
}

func TestHandleModels(t *testing.T) {
	cfg := &config.Config{
		DefaultModel: "openrouter/free",
		Models:       config.Models{},
	}
	p := New(cfg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)

	p.handleModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp struct {
		Data []map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Error("expected at least one model in response")
	}
	if resp.Data[0]["id"] == "" {
		t.Error("model entry missing id")
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}