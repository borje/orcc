package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// ErrKeyRejected signals that OpenRouter explicitly rejected the API key
// (HTTP 401/403). Every other failure — network error, timeout, 429, 5xx — is
// transient and must NOT be treated as an invalid key, so callers can proceed
// optimistically instead of forcing a re-key on a flaky connection.
var ErrKeyRejected = errors.New("OpenRouter rejected the API key")

// ValidateKey checks an API key against the OpenRouter /key endpoint. keyURL
// should be "https://openrouter.ai/api/v1/key" in production. It returns
// ErrKeyRejected on 401/403, a generic (transient) error on any other non-2xx
// or network failure, and nil when the key is accepted.
func ValidateKey(keyURL, apiKey string) error {
	req, err := http.NewRequest(http.MethodGet, keyURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := fetchClient.Do(req)
	if err != nil {
		return fmt.Errorf("reach OpenRouter: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrKeyRejected
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("key check returned %s", resp.Status)
	}
	return nil
}

type Pricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type Model struct {
	ID      string  `json:"id"`
	Pricing Pricing `json:"pricing"`
}

type response struct {
	Data []Model `json:"data"`
}

var fetchClient = &http.Client{Timeout: 30 * time.Second}

// FetchModels returns models with pricing from the OpenRouter models endpoint.
// baseURL should be "https://openrouter.ai/api/v1/models" in production.
func FetchModels(baseURL, apiKey string) ([]Model, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, err
	}
	// Trim the catalog to models Claude Code can actually use: text output with
	// tool-calling support, ordered most-popular first. This drops image-only,
	// no-tools, and obscure entries server-side before we ever display them.
	q := req.URL.Query()
	q.Set("output_modalities", "text")
	q.Set("supported_parameters", "tools")
	q.Set("sort", "most-popular")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %s", resp.Status)
	}

	var result response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Data, nil
}

// FormatLine formats a model as "id   $X.XX/$Y.YY per M tokens".
func FormatLine(m Model) string {
	in := formatPrice(m.Pricing.Prompt)
	out := formatPrice(m.Pricing.Completion)
	if in == "free" && out == "free" {
		return fmt.Sprintf("%-55s free", m.ID)
	}
	return fmt.Sprintf("%-55s %s in / %s out per M", m.ID, in, out)
}

func formatPrice(raw string) string {
	if raw == "" || raw == "0" {
		return "free"
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f == 0 {
		return "free"
	}
	perM := f * 1_000_000
	return fmt.Sprintf("$%.4g", perM)
}

func ParsePrice(raw string) (float64, bool) {
	if raw == "" || raw == "0" {
		return 0, true
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func SortByPrice(ms []Model) {
	sort.Slice(ms, func(i, j int) bool {
		pi, _ := ParsePrice(ms[i].Pricing.Prompt)
		pj, _ := ParsePrice(ms[j].Pricing.Prompt)
		if pi != pj {
			return pi < pj
		}
		ci, _ := ParsePrice(ms[i].Pricing.Completion)
		cj, _ := ParsePrice(ms[j].Pricing.Completion)
		return ci < cj
	})
}

func SortByName(ms []Model) {
	sort.Slice(ms, func(i, j int) bool {
		return ms[i].ID < ms[j].ID
	})
}
