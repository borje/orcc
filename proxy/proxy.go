package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"orcc/config"
)

type Proxy struct {
	cfg    *config.Config
	target string
	client *http.Client
	debug  bool
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
	if changed {
		log.Printf("handleMessages: injected openrouter:web_search")
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

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic("mustMarshal: " + err.Error())
	}
	return data
}
