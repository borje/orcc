package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"orcc/config"
)

type Proxy struct {
	cfg    *config.Config
	target string
	client *http.Client
}

func New(cfg *config.Config) *Proxy {
	return &Proxy{
		cfg:    cfg,
		target: "https://openrouter.ai/api/v1",
		client: &http.Client{},
	}
}

func (p *Proxy) Start(addr string) error {
	return http.ListenAndServe(addr, p)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
		p.handleMessages(w, r)
		return
	}
	p.reverseProxy(w, r)
}

func (p *Proxy) handleMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	modified, changed, err := injectWebSearch(body)
	if err != nil {
		log.Printf("injectWebSearch: %v", err)
	}
	if !changed {
		modified = body
	}

	msgURL := p.target + "/messages"
	if r.URL.RawQuery != "" {
		msgURL += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequest(http.MethodPost, msgURL, bytes.NewReader(modified))
	if err != nil {
		http.Error(w, "create request", http.StatusInternalServerError)
		return
	}
	setOutboundHeaders(req, r.Header, p.cfg.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "proxy request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyResponse(w, resp)
}

func (p *Proxy) reverseProxy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	target := p.target + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequest(r.Method, target, r.Body)
	if err != nil {
		http.Error(w, "create request", http.StatusInternalServerError)
		return
	}
	setOutboundHeaders(req, r.Header, p.cfg.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "proxy request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyResponse(w, resp)
}

func setOutboundHeaders(req *http.Request, src http.Header, apiKey string) {
	req.Header = make(http.Header)
	for k, v := range src {
		if strings.EqualFold(k, "host") {
			continue
		}
		for _, val := range v {
			req.Header.Add(k, val)
		}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("x-api-key", apiKey)
}

type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.f.Flush()
	return n, err
}

func copyResponse(w http.ResponseWriter, resp *http.Response) {
	for k, v := range resp.Header {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if f, ok := w.(http.Flusher); ok {
		io.Copy(&flushWriter{w, f}, resp.Body)
	} else {
		io.Copy(w, resp.Body)
	}
}

func injectWebSearch(body []byte) ([]byte, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false, nil
	}

	toolsRaw, ok := raw["tools"]
	if !ok {
		return body, false, nil
	}

	var tools []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &tools); err != nil {
		return nil, false, err
	}

	hasWebSearch := false
	for _, tool := range tools {
		var t map[string]json.RawMessage
		if json.Unmarshal(tool, &t) == nil {
			if nameRaw, ok := t["name"]; ok {
				var name string
				if json.Unmarshal(nameRaw, &name) == nil && name == "web_search" {
					hasWebSearch = true
					break
				}
			}
		}
	}

	if !hasWebSearch {
		return body, false, nil
	}

	var newTools []json.RawMessage
	for _, tool := range tools {
		var t map[string]json.RawMessage
		if json.Unmarshal(tool, &t) == nil {
			if nameRaw, ok := t["name"]; ok {
				var name string
				if json.Unmarshal(nameRaw, &name) == nil && name == "web_search" {
					continue
				}
			}
		}
		newTools = append(newTools, tool)
	}
	newTools = append(newTools, json.RawMessage(`{"type":"openrouter:web_search"}`))

	if tcRaw, ok := raw["tool_choice"]; ok {
		var tc map[string]json.RawMessage
		if json.Unmarshal(tcRaw, &tc) == nil {
			if nameRaw, ok := tc["name"]; ok {
				var name string
				if json.Unmarshal(nameRaw, &name) == nil && name == "web_search" {
					raw["tool_choice"] = json.RawMessage(`null`)
				}
			}
		}
	}

	raw["tools"] = json.RawMessage(mustMarshal(newTools))
	out, _ := json.Marshal(raw)
	return out, true, nil
}

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic("mustMarshal: " + err.Error())
	}
	return data
}
