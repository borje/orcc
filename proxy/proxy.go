package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"orcc/config"
	"orcc/models"
)

type messagesRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	Messages  []message         `json:"messages"`
	System    json.RawMessage   `json:"system,omitempty"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type Proxy struct {
	cfg         *config.Config
	target      string
	client      *http.Client
	debug       bool
	modelsCache []map[string]string
	modelsAt    time.Time
	mu          sync.Mutex
}

func New(cfg *config.Config) *Proxy {
	return &Proxy{
		cfg:    cfg,
		debug:  cfg.Debug,
		target: "https://openrouter.ai/api/v1",
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

type Server struct {
	*http.Server
	proxy *Proxy
}

func (p *Proxy) Start(addr string) (*Server, error) {
	srv := &Server{
		Server: &http.Server{Addr: addr, Handler: p},
		proxy:  p,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("proxy server error: %v", err)
		}
	}()
	return srv, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
		p.handleMessages(w, r)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		p.handleModels(w, r)
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

	if tryOptimizations(body, w) {
		if p.debug {
			log.Printf("DEBUG request optimized (skipped upstream call)")
		}
		return
	}

	modified, changed, err := injectWebSearch(body)
	if err != nil {
		log.Printf("injectWebSearch: %v", err)
	}
	if !changed {
		modified = body
	}
	if changed {
		log.Printf("handleMessages: injected openrouter:web_search")
	}

	if scoreBody, scoreChanged, err := extractParetoScore(modified); err != nil {
		log.Printf("extractParetoScore: %v", err)
	} else if scoreChanged {
		modified = scoreBody
		changed = true
		log.Printf("handleMessages: injected min_coding_score for Pareto model")
	}

	msgURL := p.target + "/messages"
	if r.URL.RawQuery != "" {
		msgURL += "?" + r.URL.RawQuery
	}

	if p.debug {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, modified, "", "  "); err == nil {
			log.Printf("DEBUG request %s:\n%s", msgURL, pretty.String())
		} else {
			log.Printf("DEBUG request %s: %s", msgURL, string(modified))
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, msgURL, bytes.NewReader(modified))
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

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("handleMessages: upstream error body: %s", respBody)
		copyHeaders(w, resp.Header)
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	copyHeaders(w, resp.Header)
	w.WriteHeader(resp.StatusCode)

	f, _ := w.(http.Flusher)
	if changed && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		filterServerToolUseSSE(resp.Body, w, f)
	} else if f != nil {
		io.Copy(&flushWriter{w, f}, resp.Body)
	} else {
		io.Copy(w, resp.Body)
	}
}

// filterServerToolUseSSE exists because OpenRouter returns web search blocks as
// server_tool_use{name:"openrouter:web_search"}, but Claude Code only recognises
// server_tool_use{name:"web_search"} and increments its search counter only when
// it also sees a matching web_search_tool_result block. This function renames the
// tool and injects that result block, shifting subsequent indices by +1.
func filterServerToolUseSSE(body io.Reader, w io.Writer, f http.Flusher) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	// renamedIdx/renamedID track the server_tool_use block we renamed.
	renamedIdx := -1
	renamedID := ""
	// shift is +1 once we've injected the web_search_tool_result block.
	shift := 0

	var lines []string

	injectResult := func(afterIdx int) {
		resultIdx := afterIdx + 1
		startData, _ := json.Marshal(map[string]interface{}{
			"type":  "content_block_start",
			"index": resultIdx,
			"content_block": map[string]interface{}{
				"type":        "web_search_tool_result",
				"tool_use_id": renamedID,
				"content":     []interface{}{},
			},
		})
		stopData, _ := json.Marshal(map[string]interface{}{
			"type":  "content_block_stop",
			"index": resultIdx,
		})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", startData)
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", stopData)
		if f != nil {
			f.Flush()
		}
		shift = 1
		log.Printf("filterSSE: injected web_search_tool_result at index %d", resultIdx)
	}

	emit := func() {
		defer func() { lines = nil }()

		if len(lines) == 0 {
			fmt.Fprint(w, "\n")
			if f != nil {
				f.Flush()
			}
			return
		}

		var eventType, dataStr string
		for _, l := range lines {
			switch {
			case strings.HasPrefix(l, "event: "):
				eventType = l[7:]
			case strings.HasPrefix(l, "data: "):
				dataStr = l[6:]
			}
		}

		if dataStr == "" || dataStr == "[DONE]" {
			writeLines(w, f, lines)
			return
		}

		var evt map[string]json.RawMessage
		if err := json.Unmarshal([]byte(dataStr), &evt); err != nil {
			writeLines(w, f, lines)
			return
		}

		idx := jsonInt(evt["index"], -1)

		switch eventType {
		case "content_block_start":
			if idx >= 0 {
				if cb := jsonObj(evt["content_block"]); cb != nil {
					if jsonStr(cb["type"]) == "server_tool_use" && jsonStr(cb["name"]) == "openrouter:web_search" {
						cb["name"] = json.RawMessage(`"web_search"`)
						renamedIdx = idx
						renamedID = jsonStr(cb["id"])
						evt["content_block"] = mustMarshal(cb)
						log.Printf("filterSSE: renamed openrouter:web_search → web_search at index %d (id=%s)", idx, renamedID)
					}
				}
				// Shift index for blocks after the injected result.
				if shift > 0 && idx > renamedIdx {
					evt["index"] = mustMarshal(idx + shift)
				}
				rewriteDataLine(lines, evt)
			}

		case "content_block_stop":
			if idx == renamedIdx && shift == 0 {
				// Forward the stop for the renamed block, then inject the result block.
				writeLines(w, f, lines)
				injectResult(renamedIdx)
				return
			}
			if shift > 0 && idx > renamedIdx {
				evt["index"] = mustMarshal(idx + shift)
				rewriteDataLine(lines, evt)
			}

		case "content_block_delta":
			if shift > 0 && idx >= 0 && idx > renamedIdx {
				evt["index"] = mustMarshal(idx + shift)
				rewriteDataLine(lines, evt)
			}
		}

		writeLines(w, f, lines)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			emit()
		} else {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		emit()
	}
	if err := scanner.Err(); err != nil {
		log.Printf("filterSSE: scanner error: %v", err)
	}
}

func writeLines(w io.Writer, f http.Flusher, lines []string) {
	for _, l := range lines {
		fmt.Fprintf(w, "%s\n", l)
	}
	fmt.Fprint(w, "\n")
	if f != nil {
		f.Flush()
	}
}

func rewriteDataLine(lines []string, evt map[string]json.RawMessage) {
	newData, _ := json.Marshal(evt)
	for i, l := range lines {
		if strings.HasPrefix(l, "data: ") {
			lines[i] = "data: " + string(newData)
			break
		}
	}
}

func jsonInt(raw json.RawMessage, def int) int {
	if raw == nil {
		return def
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return def
	}
	return int(f)
}

func jsonStr(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	json.Unmarshal(raw, &s)
	return s
}

func jsonObj(raw json.RawMessage) map[string]json.RawMessage {
	if raw == nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func (p *Proxy) reverseProxy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	target := p.target + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
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

func copyHeaders(w http.ResponseWriter, src http.Header) {
	for k, v := range src {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
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
	copyHeaders(w, resp.Header)
	w.WriteHeader(resp.StatusCode)
	if f, ok := w.(http.Flusher); ok {
		io.Copy(&flushWriter{w, f}, resp.Body)
	} else {
		io.Copy(w, resp.Body)
	}
}

func isWebSearchTool(t map[string]json.RawMessage) bool {
	var name string
	return json.Unmarshal(t["name"], &name) == nil && name == "web_search"
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

	var newTools []json.RawMessage
	for _, tool := range tools {
		var t map[string]json.RawMessage
		if json.Unmarshal(tool, &t) == nil && isWebSearchTool(t) {
			continue
		}
		newTools = append(newTools, tool)
	}
	if len(newTools) == len(tools) {
		return body, false, nil
	}
	newTools = append(newTools, json.RawMessage(`{"type":"openrouter:web_search"}`))

	if tcRaw, ok := raw["tool_choice"]; ok {
		var tc map[string]json.RawMessage
		if json.Unmarshal(tcRaw, &tc) == nil {
			if nameRaw, ok := tc["name"]; ok {
				var name string
				if json.Unmarshal(nameRaw, &name) == nil && name == "web_search" {
					delete(raw, "tool_choice")
				}
			}
		}
	}

	raw["tools"] = json.RawMessage(mustMarshal(newTools))
	out, _ := json.Marshal(raw)
	return out, true, nil
}

// ── Request optimizations ─────────────────────────────────────────────────────

func parseRequest(body []byte) *messagesRequest {
	var req messagesRequest
	if json.Unmarshal(body, &req) != nil {
		return nil
	}
	return &req
}

func extractText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(content, &blocks) == nil {
		for _, b := range blocks {
			if t, ok := b["type"]; ok && jsonStr(t) == "text" {
				return jsonStr(b["text"])
			}
		}
	}
	return ""
}

func isQuotaCheck(req *messagesRequest) bool {
	return req.MaxTokens == 1 &&
		len(req.Messages) == 1 &&
		req.Messages[0].Role == "user" &&
		strings.Contains(strings.ToLower(extractText(req.Messages[0].Content)), "quota")
}

func isTitleGeneration(req *messagesRequest) bool {
	if len(req.Tools) > 0 || req.System == nil {
		return false
	}
	sysText := strings.ToLower(extractText(req.System))
	if !strings.Contains(sysText, "title") {
		return false
	}
	return strings.Contains(sysText, "sentence-case title") ||
		(strings.Contains(sysText, "return json") &&
			strings.Contains(sysText, "field") &&
			(strings.Contains(sysText, "coding session") || strings.Contains(sysText, "this session")))
}

func isPrefixDetection(req *messagesRequest) (bool, string) {
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		return false, ""
	}
	content := extractText(req.Messages[0].Content)
	if !strings.Contains(content, "<policy_spec>") || !strings.Contains(content, "Command:") {
		return false, ""
	}
	cmdIdx := strings.LastIndex(content, "Command:")
	return true, strings.TrimSpace(content[cmdIdx+len("Command:"):])
}

func isSuggestionMode(req *messagesRequest) bool {
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(extractText(m.Content), "[SUGGESTION MODE:") {
			return true
		}
	}
	return false
}

func tryOptimizations(body []byte, w http.ResponseWriter) bool {
	req := parseRequest(body)
	if req == nil {
		return false
	}

	if isQuotaCheck(req) {
		log.Printf("Optimization: intercepted quota probe")
		serveSSEText(w, req.Model, "Quota check passed.", 10, 5)
		return true
	}
	if ok, prefix := isPrefixDetection(req); ok {
		cmd := strings.Fields(prefix)
		if len(cmd) == 0 {
			return false
		}
		log.Printf("Optimization: fast prefix detection → %q", cmd[0])
		serveSSEText(w, req.Model, cmd[0], 100, 5)
		return true
	}
	if isTitleGeneration(req) {
		log.Printf("Optimization: skipped title generation")
		serveSSEText(w, req.Model, "Conversation", 100, 5)
		return true
	}
	if isSuggestionMode(req) {
		log.Printf("Optimization: skipped suggestion mode")
		serveSSEText(w, req.Model, "", 100, 1)
		return true
	}

	return false
}

func serveSSEText(w http.ResponseWriter, model, text string, inputTokens, outputTokens int) {
	msgID := fmt.Sprintf("msg_%x", time.Now().UnixNano())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	f, _ := w.(http.Flusher)

	writeSSE := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if f != nil {
			f.Flush()
		}
	}

	writeSSE("message_start",
		fmt.Sprintf(`{"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","content":[],"model":"%s","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%d,"output_tokens":%d}}}`,
			msgID, model, inputTokens, outputTokens))

	textJSON, _ := json.Marshal(text)
	writeSSE("content_block_start",
		fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":%s}}`, textJSON))

	writeSSE("content_block_delta",
		fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, textJSON))

	writeSSE("content_block_stop", `{"type":"content_block_stop","index":0}`)
	writeSSE("message_delta",
		fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":%d}}`, outputTokens))
	writeSSE("message_stop", `{"type":"message_stop"}`)
}

// ── Gateway model discovery ───────────────────────────────────────────────────

const modelCacheTTL = 24 * time.Hour

func (p *Proxy) handleModels(w http.ResponseWriter, r *http.Request) {
	models := p.ensureModels()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": models})
	log.Printf("handleModels: returning %d models", len(models))
}

func (p *Proxy) configuredModelIDs() []string {
	ids := []string{
		p.cfg.DefaultModel,
		p.cfg.Models.Opus,
		p.cfg.Models.Sonnet,
		p.cfg.Models.Haiku,
		p.cfg.Models.Subagent,
	}
	seen := map[string]bool{}
	uniq := ids[:0]
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	return uniq
}

func modelEntry(id string) map[string]string {
	return map[string]string{
		"type":         "model",
		"id":           id,
		"display_name": id,
		"created_at":   "2025-01-01T00:00:00Z",
	}
}

func (p *Proxy) fallbackModels() []map[string]string {
	ids := p.configuredModelIDs()
	list := make([]map[string]string, len(ids))
	for i, id := range ids {
		list[i] = modelEntry(id)
	}
	return list
}

func (p *Proxy) ensureModels() []map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.modelsCache != nil && time.Since(p.modelsAt) < modelCacheTTL {
		return p.modelsCache
	}

	fetched, err := models.FetchModels("https://openrouter.ai/api/v1/models", p.cfg.APIKey)
	if err != nil {
		log.Printf("fetch models from OpenRouter: %v (using configured models only)", err)
		return p.fallbackModels()
	}

	seen := map[string]bool{}
	list := make([]map[string]string, 0, len(fetched)+5)
	for _, m := range fetched {
		seen[m.ID] = true
		list = append(list, modelEntry(m.ID))
	}
	for _, id := range p.configuredModelIDs() {
		if !seen[id] {
			list = append(list, modelEntry(id))
		}
	}

	p.modelsCache = list
	p.modelsAt = time.Now()
	return list
}

// ── Pareto router score injection ─────────────────────────────────────────────

// paretoScoreSuffixes maps named tier aliases to min_coding_score values.
var paretoScoreSuffixes = map[string]float64{
	"low":    0.5,
	"mid":    0.7,
	"medium": 0.7,
	"high":   0.9,
}

// extractParetoScore inspects the request body's "model" field. If the model
// starts with "openrouter/pareto" and ends with a recognized score suffix
// (numeric 0.0–1.0 or a named alias), the suffix is stripped from the model
// field and min_coding_score is injected via the pareto-router plugin:
//
//	"plugins": [{"id": "pareto-router", "min_coding_score": 0.8}]
//
// The ":nitro" suffix is left intact and does not trigger score injection.
// Other suffixes are left untouched.
func extractParetoScore(body []byte) ([]byte, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false, nil
	}

	model := jsonStr(raw["model"])
	if !strings.HasPrefix(model, "openrouter/pareto") {
		return body, false, nil
	}

	lastColon := strings.LastIndex(model, ":")
	if lastColon < 0 {
		return body, false, nil
	}

	base := model[:lastColon]
	suffix := model[lastColon+1:]

	var score float64
	if s, ok := paretoScoreSuffixes[suffix]; ok {
		score = s
	} else if f, err := strconv.ParseFloat(suffix, 64); err == nil && f >= 0.0 && f <= 1.0 {
		score = f
	} else {
		// Unknown suffix (including :nitro) — pass through unchanged.
		return body, false, nil
	}

	raw["model"] = mustMarshal(base)

	plugin := map[string]interface{}{
		"id":               "pareto-router",
		"min_coding_score": score,
	}
	var plugins []json.RawMessage
	if pluginsRaw, ok := raw["plugins"]; ok {
		json.Unmarshal(pluginsRaw, &plugins)
	}
	plugins = append(plugins, mustMarshal(plugin))
	raw["plugins"] = mustMarshal(plugins)

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
