package observability

import (
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type attendanceLatencySnapshot struct {
	AvgMs float64 `json:"avgMs"`
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
	MaxMs float64 `json:"maxMs"`
}

type AttendanceMetricsSnapshot struct {
	CheckIns struct {
		Attempts    uint64 `json:"attempts"`
		Success     uint64 `json:"success"`
		Duplicates  uint64 `json:"duplicates"`
		Failures    uint64 `json:"failures"`
		WrongPin    uint64 `json:"wrongPin"`
		RateLimited uint64 `json:"rateLimited"`
	} `json:"checkIns"`
	Latency attendanceLatencySnapshot `json:"latency"`
	Pin     struct {
		AutoRotateEnabled bool   `json:"autoRotateEnabled"`
		RotationMinutes   int    `json:"rotationMinutes"`
		GraceSeconds      int    `json:"graceSeconds"`
		Rotations         uint64 `json:"rotations"`
		ManualRefreshes   uint64 `json:"manualRefreshes"`
		Collisions        uint64 `json:"collisions"`
		RedisSetFailures  uint64 `json:"redisSetFailures"`
		DBInsertFailures  uint64 `json:"dbInsertFailures"`
	} `json:"pin"`
	LastEventAt *time.Time `json:"lastEventAt,omitempty"`
}

type attendanceMetrics struct {
	mu sync.Mutex

	attempts      uint64
	success       uint64
	duplicates    uint64
	failures      uint64
	wrongPin      uint64
	rateLimited   uint64
	rotations     uint64
	manualRefresh uint64
	collisions    uint64
	redisFailures uint64
	dbFailures    uint64
	latenciesMs   []float64
	maxSamples    int
	lastEventAt   *time.Time
}

var globalAttendanceMetrics = &attendanceMetrics{
	latenciesMs: make([]float64, 0, 512),
	maxSamples:  512,
}

func RecordAttendanceCheckInAttempt() {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.attempts++
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendanceCheckInSuccess(latency time.Duration) {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.success++
	globalAttendanceMetrics.recordLatencyLocked(latency)
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendanceCheckInDuplicate(latency time.Duration) {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.duplicates++
	globalAttendanceMetrics.recordLatencyLocked(latency)
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendanceCheckInFailure(latency time.Duration) {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.failures++
	globalAttendanceMetrics.recordLatencyLocked(latency)
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendanceWrongPin(latency time.Duration) {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.wrongPin++
	globalAttendanceMetrics.failures++
	globalAttendanceMetrics.recordLatencyLocked(latency)
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendanceRateLimited() {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.rateLimited++
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendancePinRotation() {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.rotations++
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendancePinManualRefresh() {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.manualRefresh++
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendancePinCollision() {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.collisions++
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendanceRedisFailure() {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.redisFailures++
	globalAttendanceMetrics.touchLocked(time.Now())
}

func RecordAttendanceDBInsertFailure() {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	globalAttendanceMetrics.dbFailures++
	globalAttendanceMetrics.touchLocked(time.Now())
}

func SnapshotAttendanceMetrics() AttendanceMetricsSnapshot {
	globalAttendanceMetrics.mu.Lock()
	defer globalAttendanceMetrics.mu.Unlock()

	snapshot := AttendanceMetricsSnapshot{}
	snapshot.CheckIns.Attempts = globalAttendanceMetrics.attempts
	snapshot.CheckIns.Success = globalAttendanceMetrics.success
	snapshot.CheckIns.Duplicates = globalAttendanceMetrics.duplicates
	snapshot.CheckIns.Failures = globalAttendanceMetrics.failures
	snapshot.CheckIns.WrongPin = globalAttendanceMetrics.wrongPin
	snapshot.CheckIns.RateLimited = globalAttendanceMetrics.rateLimited
	snapshot.Pin.AutoRotateEnabled = AttendancePinAutoRotateEnabled()
	snapshot.Pin.RotationMinutes = AttendancePinRotationMinutes()
	snapshot.Pin.GraceSeconds = AttendancePinGraceSeconds()
	snapshot.Pin.Rotations = globalAttendanceMetrics.rotations
	snapshot.Pin.ManualRefreshes = globalAttendanceMetrics.manualRefresh
	snapshot.Pin.Collisions = globalAttendanceMetrics.collisions
	snapshot.Pin.RedisSetFailures = globalAttendanceMetrics.redisFailures
	snapshot.Pin.DBInsertFailures = globalAttendanceMetrics.dbFailures
	snapshot.LastEventAt = globalAttendanceMetrics.lastEventAt
	snapshot.Latency = buildLatencySnapshot(globalAttendanceMetrics.latenciesMs)
	return snapshot
}

func (metrics *attendanceMetrics) recordLatencyLocked(latency time.Duration) {
	latencyMs := float64(latency.Milliseconds()) + float64(latency.Nanoseconds()%int64(time.Millisecond))/1_000_000
	metrics.latenciesMs = append(metrics.latenciesMs, latencyMs)
	if len(metrics.latenciesMs) > metrics.maxSamples {
		metrics.latenciesMs = append([]float64(nil), metrics.latenciesMs[len(metrics.latenciesMs)-metrics.maxSamples:]...)
	}
}

func (metrics *attendanceMetrics) touchLocked(now time.Time) {
	nowCopy := now.UTC()
	metrics.lastEventAt = &nowCopy
}

func buildLatencySnapshot(values []float64) attendanceLatencySnapshot {
	snapshot := attendanceLatencySnapshot{}
	if len(values) == 0 {
		return snapshot
	}

	sortedValues := append([]float64(nil), values...)
	sort.Float64s(sortedValues)

	var total float64
	for _, value := range sortedValues {
		total += value
	}

	snapshot.AvgMs = round2(total / float64(len(sortedValues)))
	snapshot.P50Ms = percentile(sortedValues, 0.50)
	snapshot.P95Ms = percentile(sortedValues, 0.95)
	snapshot.P99Ms = percentile(sortedValues, 0.99)
	snapshot.MaxMs = round2(sortedValues[len(sortedValues)-1])
	return snapshot
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}

	if len(values) == 1 {
		return round2(values[0])
	}

	position := p * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return round2(values[lower])
	}

	weight := position - float64(lower)
	return round2(values[lower] + (values[upper]-values[lower])*weight)
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func AttendancePinAutoRotateEnabled() bool {
	rawValue := strings.TrimSpace(os.Getenv("ATTENDANCE_PIN_AUTO_ROTATE"))
	if rawValue == "" {
		return true
	}
	return !strings.EqualFold(rawValue, "false") && rawValue != "0"
}

func AttendancePinRotationMinutes() int {
	return readIntEnv("ATTENDANCE_PIN_ROTATION_MINUTES", 1)
}

func AttendancePinGraceSeconds() int {
	return readIntEnv("ATTENDANCE_PIN_GRACE_SECONDS", 15)
}

func readIntEnv(key string, fallback int) int {
	rawValue := strings.TrimSpace(os.Getenv(key))
	if rawValue == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(rawValue)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
