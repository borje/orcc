package models_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"orcc/models"
)

func TestFetchIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "anthropic/claude-sonnet-4.6", "name": "Claude Sonnet"},
				{"id": "anthropic/claude-opus-4.7", "name": "Claude Opus"},
				{"id": "openai/gpt-4o", "name": "GPT-4o"},
			},
		})
	}))
	defer srv.Close()

	ids, err := models.FetchIDs(srv.URL, "test-key")
	if err != nil {
		t.Fatalf("FetchIDs() error: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("got %d models, want 3", len(ids))
	}
	if ids[0] != "anthropic/claude-sonnet-4.6" {
		t.Errorf("ids[0] = %q, want %q", ids[0], "anthropic/claude-sonnet-4.6")
	}
}

func TestFetchIDsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := models.FetchIDs(srv.URL, "key")
	if err == nil {
		t.Error("expected error on HTTP 500")
	}
}

func TestFetchIDsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := models.FetchIDs(srv.URL, "key")
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}
