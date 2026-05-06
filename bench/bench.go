// Measures round-trip latency and TTFB: proxy vs direct OpenRouter.
// Interleaves proxy/direct calls so server load affects both equally.
// Usage: go run ./bench [runs]
package main

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"orcc/config"
)

const (
	model     = "qwen/qwen3-coder-next"
	proxyURL  = "http://127.0.0.1:3458/v1/messages"
	directURL = "https://openrouter.ai/api/v1/messages"
)

var reqBody = []byte(`{"model":"qwen/qwen3-coder-next","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)

type result struct {
	ttfbMs  float64
	totalMs float64
	status  int
}

func main() {
	runs := 10
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil && n > 0 {
			runs = n
		}
	}

	cfgPath, _ := config.DefaultPath()
	cfg, err := config.Load(cfgPath)
	if err != nil || cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "load config failed")
		os.Exit(1)
	}

	client := &http.Client{}
	fmt.Printf("Model: %s   max_tokens: 1   runs: up to %d\n", model, runs)
	fmt.Printf("%-4s  %-6s  %-10s  %-10s  %s\n", "run", "target", "TTFB", "total", "status")
	fmt.Println("----  ------  ----------  ----------  ------")

	var proxyRes, directRes []result

	for i := 0; i < runs; i++ {
		pr := fire(client, proxyURL, cfg.APIKey)
		dr := fire(client, directURL, cfg.APIKey)

		row := func(label string, r result) {
			ttfb := fmt.Sprintf("%.0fms", r.ttfbMs)
			total := fmt.Sprintf("%.0fms", r.totalMs)
			note := ""
			if r.status == 429 {
				note = " (rate limited, excluded)"
			}
			fmt.Printf("%-4d  %-6s  %-10s  %-10s  %d%s\n", i+1, label, ttfb, total, r.status, note)
		}
		row("proxy", pr)
		row("direct", dr)

		if pr.status == 200 {
			proxyRes = append(proxyRes, pr)
		}
		if dr.status == 200 {
			directRes = append(directRes, dr)
		}
	}

	fmt.Println()
	printStats("proxy ", proxyRes)
	printStats("direct", directRes)

	if len(proxyRes) > 0 && len(directRes) > 0 {
		ttfbOverhead := medianTTFB(proxyRes) - medianTTFB(directRes)
		totalOverhead := medianTotal(proxyRes) - medianTotal(directRes)
		fmt.Printf("\nProxy overhead (median)  TTFB: %+.0fms   total: %+.0fms\n", ttfbOverhead, totalOverhead)
	}
}

func fire(client *http.Client, url, apiKey string) result {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "request error: %v\n", err)
		return result{status: -1}
	}
	ttfb := time.Since(start).Seconds() * 1000
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	total := time.Since(start).Seconds() * 1000

	return result{ttfbMs: ttfb, totalMs: total, status: resp.StatusCode}
}

func printStats(label string, rs []result) {
	if len(rs) == 0 {
		fmt.Printf("%s  no successful results\n", label)
		return
	}
	ttfbs := extract(rs, func(r result) float64 { return r.ttfbMs })
	totals := extract(rs, func(r result) float64 { return r.totalMs })
	fmt.Printf("%s  TTFB  p50=%-7s p90=%-7s min=%-7s max=%s\n",
		label, ms(median(ttfbs)), ms(p90(ttfbs)), ms(min64(ttfbs)), ms(max64(ttfbs)))
	fmt.Printf("%s  total p50=%-7s p90=%-7s min=%-7s max=%s\n",
		label, ms(median(totals)), ms(p90(totals)), ms(min64(totals)), ms(max64(totals)))
}

func ms(v float64) string { return fmt.Sprintf("%.0fms", v) }

func extract(rs []result, f func(result) float64) []float64 {
	out := make([]float64, len(rs))
	for i, r := range rs {
		out[i] = f(r)
	}
	return out
}

func medianTTFB(rs []result) float64  { return median(extract(rs, func(r result) float64 { return r.ttfbMs })) }
func medianTotal(rs []result) float64 { return median(extract(rs, func(r result) float64 { return r.totalMs })) }

func median(ms []float64) float64 {
	s := sorted(ms)
	n := len(s)
	if n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[n/2]
}

func p90(ms []float64) float64 {
	s := sorted(ms)
	idx := int(math.Ceil(0.9*float64(len(s)))) - 1
	if idx < 0 {
		idx = 0
	}
	return s[idx]
}

func min64(ms []float64) float64 { s := sorted(ms); return s[0] }
func max64(ms []float64) float64 { s := sorted(ms); return s[len(s)-1] }

func sorted(ms []float64) []float64 {
	cp := make([]float64, len(ms))
	copy(cp, ms)
	sort.Float64s(cp)
	return cp
}
