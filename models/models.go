package models

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type response struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// FetchIDs returns model IDs from the OpenRouter models endpoint.
// baseURL should be "https://openrouter.ai/api/v1/models" in production.
func FetchIDs(baseURL, apiKey string) ([]string, error) {
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

	ids := make([]string, len(result.Data))
	for i, m := range result.Data {
		ids[i] = m.ID
	}
	return ids, nil
}
