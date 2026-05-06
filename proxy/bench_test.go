package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"orcc/config"
)

// echo server simulates OpenRouter — returns immediately with a canned response.
var echoHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`))
})

var benchBody = []byte(`{"model":"test","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)

// BenchmarkProxyOverhead: request through proxy (local loopback) vs direct to echo server.
// The delta is the proxy's net overhead: accept + ReadAll + injectWebSearch + forward + copy.
func BenchmarkProxyOverhead(b *testing.B) {
	echo := httptest.NewServer(echoHandler)
	defer echo.Close()

	cfg := &config.Config{APIKey: "test", Port: 0}
	p := &Proxy{cfg: cfg, target: echo.URL, client: &http.Client{}}
	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	b.Run("via-proxy", func(b *testing.B) {
		client := &http.Client{}
		b.ResetTimer()
		for b.Loop() {
			req, _ := http.NewRequest(http.MethodPost, proxyServer.URL+"/v1/messages", bytes.NewReader(benchBody))
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})

	b.Run("direct", func(b *testing.B) {
		client := &http.Client{}
		b.ResetTimer()
		for b.Loop() {
			req, _ := http.NewRequest(http.MethodPost, echo.URL, bytes.NewReader(benchBody))
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

// BenchmarkInjectWebSearch: cost of the JSON rewrite path alone.
func BenchmarkInjectWebSearch(b *testing.B) {
	withWebSearch := []byte(`{"model":"test","max_tokens":1,"tools":[{"name":"web_search","input_schema":{}}],"messages":[{"role":"user","content":"hi"}]}`)
	without := benchBody

	b.Run("no-web-search", func(b *testing.B) {
		for b.Loop() {
			injectWebSearch(without)
		}
	})
	b.Run("with-web-search", func(b *testing.B) {
		for b.Loop() {
			injectWebSearch(withWebSearch)
		}
	})
}
