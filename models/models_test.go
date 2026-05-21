package models_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orcc/models"
)

func mockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "anthropic/claude-sonnet-4.6", "pricing": map[string]any{"prompt": "0.000003", "completion": "0.000015"}},
				{"id": "minimax/minimax-m2.7", "pricing": map[string]any{"prompt": "0.0000003", "completion": "0.0000012"}},
				{"id": "some/free-model", "pricing": map[string]any{"prompt": "0", "completion": "0"}},
			},
		})
	}))
}

func TestFetchModels(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	ms, err := models.FetchModels(srv.URL, "test-key")
	if err != nil {
		t.Fatalf("FetchModels() error: %v", err)
	}
	if len(ms) != 3 {
		t.Fatalf("got %d models, want 3", len(ms))
	}
	if ms[0].ID != "anthropic/claude-sonnet-4.6" {
		t.Errorf("ms[0].ID = %q", ms[0].ID)
	}
	if ms[0].Pricing.Prompt != "0.000003" {
		t.Errorf("ms[0].Pricing.Prompt = %q", ms[0].Pricing.Prompt)
	}
}


func TestFormatLine(t *testing.T) {
	cases := []struct {
		model   models.Model
		wantIn  string
		wantOut string
	}{
		{
			model:   models.Model{ID: "minimax/minimax-m2.7", Pricing: models.Pricing{Prompt: "0.0000003", Completion: "0.0000012"}},
			wantIn:  "$0.3",
			wantOut: "$1.2",
		},
		{
			model:   models.Model{ID: "some/free-model", Pricing: models.Pricing{Prompt: "0", Completion: "0"}},
			wantIn:  "free",
			wantOut: "",
		},
	}

	for _, c := range cases {
		line := models.FormatLine(c.model)
		if !strings.Contains(line, c.wantIn) {
			t.Errorf("FormatLine(%q) = %q, want to contain %q", c.model.ID, line, c.wantIn)
		}
		if c.wantOut != "" && !strings.Contains(line, c.wantOut) {
			t.Errorf("FormatLine(%q) = %q, want to contain %q", c.model.ID, line, c.wantOut)
		}
	}
}

func TestFetchModelsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := models.FetchModels(srv.URL, "key")
	if err == nil {
		t.Error("expected error on HTTP 500")
	}
}

func TestFetchModelsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := models.FetchModels(srv.URL, "key")
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestParsePrice(t *testing.T) {
	cases := []struct {
		raw  string
		want float64
		ok   bool
	}{
		{"0", 0, true},
		{"", 0, true},
		{"0.000003", 0.000003, true},
		{"0.000015", 0.000015, true},
		{"invalid", 0, false},
	}
	for _, c := range cases {
		got, ok := models.ParsePrice(c.raw)
		if got != c.want || ok != c.ok {
			t.Errorf("ParsePrice(%q) = (%v, %v), want (%v, %v)", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestSortByPrice(t *testing.T) {
	ms := []models.Model{
		{ID: "a", Pricing: models.Pricing{Prompt: "0.000003", Completion: "0.000015"}},
		{ID: "b", Pricing: models.Pricing{Prompt: "0", Completion: "0"}},
		{ID: "c", Pricing: models.Pricing{Prompt: "0.0000003", Completion: "0.0000012"}},
		{ID: "d", Pricing: models.Pricing{Prompt: "0.000003", Completion: "0.000001"}},
	}
	models.SortByPrice(ms)
	if ms[0].ID != "b" {
		t.Errorf("first = %q, want free model 'b'", ms[0].ID)
	}
	if ms[1].ID != "c" {
		t.Errorf("second = %q, want 'c'", ms[1].ID)
	}
	if ms[2].ID != "d" {
		t.Errorf("third = %q, want 'd' (prompt tie, lower completion)", ms[2].ID)
	}
	if ms[3].ID != "a" {
		t.Errorf("fourth = %q, want 'a'", ms[3].ID)
	}
}

func TestSortByName(t *testing.T) {
	ms := []models.Model{
		{ID: "z-model"},
		{ID: "a-model"},
		{ID: "m-model"},
	}
	models.SortByName(ms)
	if ms[0].ID != "a-model" || ms[1].ID != "m-model" || ms[2].ID != "z-model" {
		t.Errorf("SortByName order: %q", []string{ms[0].ID, ms[1].ID, ms[2].ID})
	}
}
