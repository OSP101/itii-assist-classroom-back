package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type headerFlags []string

func (h *headerFlags) String() string {
	return strings.Join(*h, ",")
}

func (h *headerFlags) Set(value string) error {
	*h = append(*h, value)
	return nil
}

type result struct {
	status  int
	latency time.Duration
	err     string
}

func main() {
	var headers headerFlags

	url := flag.String("url", "", "target URL")
	method := flag.String("method", http.MethodGet, "HTTP method")
	body := flag.String("body", "", "request body")
	bodyBase64 := flag.String("body-b64", "", "request body encoded as base64")
	bodyFile := flag.String("body-file", "", "path to newline-delimited request bodies")
	concurrency := flag.Int("concurrency", 50, "number of concurrent workers")
	requests := flag.Int("requests", 100, "total request count")
	timeout := flag.Duration("timeout", 15*time.Second, "per-request timeout")
	name := flag.String("name", "", "scenario name")
	flag.Var(&headers, "header", "request header in the form 'Key: Value' (repeatable)")
	flag.Parse()

	if *url == "" {
		fmt.Fprintln(os.Stderr, "-url is required")
		os.Exit(1)
	}
	if *concurrency <= 0 {
		fmt.Fprintln(os.Stderr, "-concurrency must be > 0")
		os.Exit(1)
	}
	if *requests <= 0 {
		fmt.Fprintln(os.Stderr, "-requests must be > 0")
		os.Exit(1)
	}

	reqHeaders, err := parseHeaders(headers)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	transport := &http.Transport{
		MaxIdleConns:        *concurrency * 2,
		MaxIdleConnsPerHost: *concurrency * 2,
		MaxConnsPerHost:     *concurrency * 2,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{
		Timeout:   *timeout,
		Transport: transport,
	}
	defer transport.CloseIdleConnections()

	jobs := make(chan int)
	results := make(chan result, *requests)
	var wg sync.WaitGroup

	bodies, err := resolveBodies(*body, *bodyBase64, *bodyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for worker := 0; worker < *concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for jobIndex := range jobs {
				requestBody := bodies[jobIndex%len(bodies)]
				results <- doRequest(client, strings.ToUpper(*method), *url, requestBody, reqHeaders)
			}
		}()
	}

	startedAt := time.Now()
	for i := 0; i < *requests; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(results)
	elapsed := time.Since(startedAt)

	summary := summarize(results, elapsed)
	scenarioName := *name
	if scenarioName == "" {
		scenarioName = fmt.Sprintf("%s %s", strings.ToUpper(*method), *url)
	}

	fmt.Printf("Scenario: %s\n", scenarioName)
	fmt.Printf("URL: %s\n", *url)
	fmt.Printf("Method: %s\n", strings.ToUpper(*method))
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Requests: %d\n", *requests)
	fmt.Printf("Elapsed: %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("Throughput: %.2f req/s\n", summary.throughput)
	fmt.Printf("Success: %d\n", summary.success)
	fmt.Printf("Failures: %d\n", summary.failures)
	fmt.Printf("Status codes: %s\n", summary.statusLine())
	if summary.errorLine() != "" {
		fmt.Printf("Errors: %s\n", summary.errorLine())
	}
	if len(summary.latencies) > 0 {
		fmt.Printf(
			"Latency ms: avg=%.2f min=%.2f p50=%.2f p90=%.2f p95=%.2f p99=%.2f max=%.2f\n",
			toMilliseconds(summary.avgLatency),
			toMilliseconds(summary.latencies[0]),
			toMilliseconds(percentile(summary.latencies, 0.50)),
			toMilliseconds(percentile(summary.latencies, 0.90)),
			toMilliseconds(percentile(summary.latencies, 0.95)),
			toMilliseconds(percentile(summary.latencies, 0.99)),
			toMilliseconds(summary.latencies[len(summary.latencies)-1]),
		)
	}
}

func parseHeaders(raw headerFlags) (http.Header, error) {
	headers := make(http.Header)
	for _, entry := range raw {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header %q, expected 'Key: Value'", entry)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid header %q, empty key", entry)
		}
		headers.Add(key, value)
	}
	return headers, nil
}

func doRequest(client *http.Client, method, url, body string, headers http.Header) result {
	startedAt := time.Now()
	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return result{latency: time.Since(startedAt), err: err.Error()}
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return result{latency: time.Since(startedAt), err: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return result{status: resp.StatusCode, latency: time.Since(startedAt)}
}

func normalizeBody(body string) string {
	if len(body) >= 2 && body[0] == '\'' && body[len(body)-1] == '\'' {
		return body[1 : len(body)-1]
	}
	return body
}

func resolveBodies(body, bodyBase64, bodyFile string) ([]string, error) {
	providedSources := 0
	if strings.TrimSpace(body) != "" {
		providedSources++
	}
	if strings.TrimSpace(bodyBase64) != "" {
		providedSources++
	}
	if strings.TrimSpace(bodyFile) != "" {
		providedSources++
	}
	if providedSources > 1 {
		return nil, fmt.Errorf("use only one of -body, -body-b64, or -body-file")
	}

	if strings.TrimSpace(bodyFile) != "" {
		file, err := os.Open(bodyFile)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", bodyFile, err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		const maxBodyLine = 16 * 1024 * 1024
		buffer := make([]byte, 0, 64*1024)
		scanner.Buffer(buffer, maxBodyLine)

		bodies := make([]string, 0)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			bodies = append(bodies, line)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read %s: %w", bodyFile, err)
		}
		if len(bodies) == 0 {
			return nil, fmt.Errorf("%s does not contain any request bodies", bodyFile)
		}
		return bodies, nil
	}

	if strings.TrimSpace(bodyBase64) != "" {
		decoded, err := base64.StdEncoding.DecodeString(bodyBase64)
		if err != nil {
			return nil, fmt.Errorf("invalid -body-b64: %w", err)
		}
		return []string{string(decoded)}, nil
	}

	return []string{normalizeBody(body)}, nil
}

type summary struct {
	success     int
	failures    int
	throughput  float64
	avgLatency  time.Duration
	latencies   []time.Duration
	statusCodes map[int]int
	errors      map[string]int
}

func summarize(results <-chan result, elapsed time.Duration) summary {
	summary := summary{
		statusCodes: make(map[int]int),
		errors:      make(map[string]int),
	}
	var totalLatency time.Duration

	for res := range results {
		summary.latencies = append(summary.latencies, res.latency)
		totalLatency += res.latency
		if res.err != "" {
			summary.failures++
			summary.errors[res.err]++
			continue
		}
		summary.statusCodes[res.status]++
		if res.status >= 200 && res.status < 400 {
			summary.success++
		} else {
			summary.failures++
		}
	}

	if len(summary.latencies) > 0 {
		sort.Slice(summary.latencies, func(i, j int) bool {
			return summary.latencies[i] < summary.latencies[j]
		})
		summary.avgLatency = totalLatency / time.Duration(len(summary.latencies))
	}
	if elapsed > 0 {
		summary.throughput = float64(len(summary.latencies)) / elapsed.Seconds()
	}
	return summary
}

func (s summary) statusLine() string {
	if len(s.statusCodes) == 0 {
		return "none"
	}
	keys := make([]int, 0, len(s.statusCodes))
	for status := range s.statusCodes {
		keys = append(keys, status)
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, status := range keys {
		parts = append(parts, fmt.Sprintf("%d=%d", status, s.statusCodes[status]))
	}
	return strings.Join(parts, ", ")
}

func (s summary) errorLine() string {
	if len(s.errors) == 0 {
		return ""
	}
	type errorCount struct {
		message string
		count   int
	}
	items := make([]errorCount, 0, len(s.errors))
	for message, count := range s.errors {
		items = append(items, errorCount{message: message, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].message < items[j].message
		}
		return items[i].count > items[j].count
	})
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%q=%d", item.message, item.count))
	}
	return strings.Join(parts, ", ")
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		return values[0]
	}
	if p >= 1 {
		return values[len(values)-1]
	}
	index := int(float64(len(values)-1) * p)
	return values[index]
}

func toMilliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}
