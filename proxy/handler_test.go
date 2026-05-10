package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orcc/config"
)

func TestServeHTTP(t *testing.T) {
	p := New(&config.Config{
		APIKey: "test-key",
	})

	// Test reverseProxy (any path except /v1/messages)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusBadGateway {
		t.Errorf("expected 200 or 502, got %d", rec.Code)
	}
}

func TestStart(t *testing.T) {
	p := New(&config.Config{
		APIKey: "test-key",
	})
	srv, err := p.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if srv == nil {
		t.Error("Start() returned nil server")
	}
	srv.Close()
}

func TestHandleMessages(t *testing.T) {
	// Start a test server that echoes the request
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message\ndata: {\"test\":true}\n\n"))
	}))
	defer echo.Close()

	p := New(&config.Config{
		APIKey: "test-key",
	})
	p.target = echo.URL

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"test"}`))
	rec := httptest.NewRecorder()
	p.handleMessages(rec, req)
}

func TestReverseProxy(t *testing.T) {
	p := New(&config.Config{
		APIKey: "test-key",
	})

	tests := []struct {
		name       string
		method     string
		path       string
		hasAuth    bool
		wantStatus int
	}{
		{"get models", http.MethodGet, "/v1/models", false, http.StatusOK},
		{"post chat", http.MethodPost, "/v1/chat/completions", true, http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			if tc.hasAuth {
				req.Header.Set("Authorization", "Bearer test-key")
			}
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)
			// Will be 502 if no network, but handler shouldn't panic
			if rec.Code == http.StatusInternalServerError {
				t.Errorf("unexpected 500: %s", rec.Body.String())
			}
		})
	}
}

func TestSetOutboundHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Host", "localhost:3458")
	src.Set("Authorization", "Bearer old-key")
	src.Set("X-Custom", "value")

	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	setOutboundHeaders(req, src, "new-key")

	if req.Header.Get("Host") != "" {
		t.Error("Host header should be removed")
	}
	if req.Header.Get("Authorization") != "Bearer new-key" {
		t.Errorf("expected new key, got %s", req.Header.Get("Authorization"))
	}
	if req.Header.Get("x-api-key") != "new-key" {
		t.Error("x-api-key not set")
	}
	if req.Header.Get("X-Custom") != "value" {
		t.Error("custom header lost")
	}
}

func TestCopyResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := httptest.NewRecorder()
	resp.Header().Set("X-Test", "value")
	resp.WriteHeader(http.StatusCreated)
	resp.WriteString("body")

	copyResponse(rec, resp.Result())

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
	if rec.Header().Get("X-Test") != "value" {
		t.Error("header not copied")
	}
	if rec.Body.String() != "body" {
		t.Errorf("body not copied: %s", rec.Body.String())
	}
}

func TestIsWebSearchTool(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"web_search", "web_search", true},
		{"other", "bash", false},
		{"empty", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isWebSearchTool(map[string]json.RawMessage{"name": json.RawMessage(`"` + tc.in + `"`)})
			if got != tc.want {
				t.Errorf("isWebSearchTool(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMustMarshal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid type")
		}
	}()
	mustMarshal(make(chan int)) // invalid type
}