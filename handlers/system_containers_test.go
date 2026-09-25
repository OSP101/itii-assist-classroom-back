package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchContainerMetricsFromDockerAPI(t *testing.T) {
	started := time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339Nano)
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("all") != "1" {
			t.Errorf("expected all=1, got %q", r.URL.RawQuery)
		}
		w.Write([]byte(`[
			{"Id":"aaa","Names":["/itii-backend-blue-1"],"Image":"backend:vm","State":"running"},
			{"Id":"bbb","Names":["/itii-backend-green-1"],"Image":"backend:vm","State":"exited"}
		]`))
	})
	mux.HandleFunc("/containers/aaa/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"RestartCount":2,"State":{"Status":"running","StartedAt":"` + started + `",
			"FinishedAt":"0001-01-01T00:00:00Z","Health":{"Status":"healthy"}},"HostConfig":{"NanoCpus":2000000000}}`))
	})
	mux.HandleFunc("/containers/aaa/stats", func(w http.ResponseWriter, r *http.Request) {
		// 50ms of CPU over 400ms of system time on 4 CPUs = 50% of one core.
		w.Write([]byte(`{
			"cpu_stats":{"cpu_usage":{"total_usage":150000000},"system_cpu_usage":1400000000,"online_cpus":4},
			"precpu_stats":{"cpu_usage":{"total_usage":100000000},"system_cpu_usage":1000000000},
			"memory_stats":{"usage":314572800,"limit":1073741824,"stats":{"inactive_file":104857600}}
		}`))
	})
	mux.HandleFunc("/containers/bbb/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"RestartCount":0,"State":{"Status":"exited","StartedAt":"2026-09-01T00:00:00Z",
			"FinishedAt":"2026-09-02T00:00:00Z"},"HostConfig":{"NanoCpus":0}}`))
	})
	mux.HandleFunc("/containers/bbb/stats", func(w http.ResponseWriter, r *http.Request) {
		t.Error("stats must not be requested for a stopped container")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dockerAPIOnce.Do(func() {})
	dockerAPIHTTP, dockerAPIBaseURL = server.Client(), server.URL

	containers, source := fetchContainerMetrics()
	if source != "docker_api" {
		t.Fatalf("source = %q, want docker_api", source)
	}
	if len(containers) != 2 {
		t.Fatalf("got %d containers, want 2", len(containers))
	}

	running := containers[0]
	if running.Name != "itii-backend-blue-1" || running.Status != "running" || running.Health != "healthy" {
		t.Fatalf("unexpected running container: %+v", running)
	}
	if running.Restarts != 2 || running.CPULimitCores != 2 {
		t.Fatalf("restarts/cpu limit wrong: %+v", running)
	}
	if running.CPUPercent != 50 {
		t.Fatalf("cpuPercent = %v, want 50", running.CPUPercent)
	}
	if running.MemoryUsageMB != 200 || running.MemoryLimitMB != 1024 {
		t.Fatalf("memory = %v/%v MB, want 200/1024", running.MemoryUsageMB, running.MemoryLimitMB)
	}
	if running.UptimeSeconds < 89*60 || running.UptimeSeconds > 91*60 {
		t.Fatalf("uptimeSeconds = %d, want ~5400", running.UptimeSeconds)
	}
	if running.FinishedAt != "" {
		t.Fatalf("running container should not report finishedAt: %q", running.FinishedAt)
	}

	stopped := containers[1]
	if stopped.Status != "stopped" || stopped.UptimeSeconds != 0 || stopped.FinishedAt == "" {
		t.Fatalf("unexpected stopped container: %+v", stopped)
	}
}
