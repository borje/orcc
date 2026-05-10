package models

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

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

// FetchModels returns models with pricing from the OpenRouter models endpoint.
// baseURL should be "https://openrouter.ai/api/v1/models" in production.
func FetchModels(baseURL, apiKey string) ([]Model, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
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

// FetchIDs returns model IDs from the OpenRouter models endpoint.
func FetchIDs(baseURL, apiKey string) ([]string, error) {
	ms, err := FetchModels(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	return ids, nil
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
