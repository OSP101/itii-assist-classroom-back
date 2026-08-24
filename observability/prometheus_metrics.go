package observability

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/realtime"

	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/expfmt"
	"gorm.io/gorm"
)

const (
	activeWindow     = 5 * time.Minute
	optionalMetricNS = "faculty_classroom"
	optionalMetricSS = "app"
	bangkokTimezone  = "Asia/Bangkok"
)

type activeSessionTracker struct {
	mu       sync.RWMutex
	lastSeen map[string]time.Time
	// lastPrune throttles the sweep. touch() runs on every authenticated
	// request while holding the only lock in this struct, so sweeping the whole
	// map there made each request O(active users) inside a global critical
	// section — with a few hundred students checking in at once, requests spend
	// their time queueing behind each other on this mutex rather than doing
	// work. The map is bounded by the active window either way, so sweeping on
	// a timer costs nothing but a few stale keys between sweeps.
	lastPrune time.Time
}

// pruneInterval is the longest a stale key may linger before a sweep removes
// it. Well below activeWindow, so active_sessions_5m stays accurate to within
// one interval even if nothing scrapes it.
const pruneInterval = 30 * time.Second

func newActiveSessionTracker() *activeSessionTracker {
	return &activeSessionTracker{
		lastSeen: map[string]time.Time{},
	}
}

func (t *activeSessionTracker) touch(kind string, id uint, now time.Time) {
	if id == 0 {
		return
	}

	// strconv rather than fmt.Sprintf: this runs on every authenticated
	// request, and Sprintf's reflection path is pure overhead for two values.
	key := kind + ":" + strconv.FormatUint(uint64(id), 10)

	t.mu.Lock()
	defer t.mu.Unlock()

	t.lastSeen[key] = now
	if now.Sub(t.lastPrune) >= pruneInterval {
		t.pruneLocked(now)
	}
}

func (t *activeSessionTracker) countActive(now time.Time) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Scrapes are infrequent, so this one always sweeps: the reported number is
	// exact at the moment it is read regardless of when the last sweep ran.
	t.pruneLocked(now)
	return float64(len(t.lastSeen))
}

func (t *activeSessionTracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-activeWindow)
	for key, seenAt := range t.lastSeen {
		if seenAt.Before(cutoff) {
			delete(t.lastSeen, key)
		}
	}
	t.lastPrune = now
}

type prometheusMetrics struct {
	db                 *gorm.DB
	once               sync.Once
	activeSessions     *activeSessionTracker
	httpRequestsTotal  *prometheus.CounterVec
	httpRequestLatency *prometheus.HistogramVec
	httpErrorsTotal    *prometheus.CounterVec
	optionalAvailable  *prometheus.GaugeVec
	loc                *time.Location
}

var metricsManager = &prometheusMetrics{
	activeSessions: newActiveSessionTracker(),
}

func InitPrometheusMetrics(db *gorm.DB) {
	metricsManager.once.Do(func() {
		metricsManager.db = db
		metricsManager.loc = loadBangkokLocation()
		metricsManager.register()
	})
}

func loadBangkokLocation() *time.Location {
	loc, err := time.LoadLocation(bangkokTimezone)
	if err != nil {
		log.Printf("warning: failed to load %s timezone for metrics, falling back to local time: %v", bangkokTimezone, err)
		return time.Local
	}
	return loc
}

func (m *prometheusMetrics) register() {
	m.httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "http_requests_total",
		Help:      "Total HTTP requests processed by the backend, labeled by route, method, and status.",
	}, []string{"path", "method", "status"})

	m.httpRequestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request duration histogram for backend routes.",
		Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"path", "method"})

	m.httpErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "http_errors_total",
		Help:      "Total HTTP error responses, separated into 4xx and 5xx classes.",
	}, []string{"path", "method", "status_class"})

	m.optionalAvailable = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "optional_metric_available",
		Help:      "Whether an optional database-backed metric could be registered from the current schema.",
	}, []string{"metric"})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "active_sessions_5m",
		Help:      "Unique authenticated users or students seen in the last 5 minutes on this backend instance.",
	}, func() float64 {
		return m.activeSessions.countActive(time.Now())
	})

	// These two are the leak detectors for the realtime hub: on a healthy
	// instance they rise and fall with actual class activity and return near
	// zero overnight. A floor that only ever climbs across days means ghost
	// connections are accumulating again.
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "realtime_connected_clients",
		Help:      "WebSocket clients currently registered in the realtime hub.",
	}, func() float64 {
		clients, _ := realtime.Stats()
		return float64(clients)
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "realtime_active_rooms",
		Help:      "Realtime hub rooms that currently have at least one subscriber.",
	}, func() float64 {
		_, rooms := realtime.Stats()
		return float64(rooms)
	})

	// Pool saturation vs. query slowness look identical from the browser — both
	// are "the page is waiting". db_connections_wait_total climbing is the
	// signal that requests are queueing for a connection rather than running.
	poolGauges := []struct {
		name  string
		help  string
		value func(inUse, idle, open int, waitCount int64) float64
	}{
		{"db_connections_in_use", "Database connections currently executing a query.", func(inUse, _, _ int, _ int64) float64 { return float64(inUse) }},
		{"db_connections_idle", "Database connections open but idle in the pool.", func(_, idle, _ int, _ int64) float64 { return float64(idle) }},
		{"db_connections_open", "Total database connections open by this backend.", func(_, _, open int, _ int64) float64 { return float64(open) }},
		{"db_connections_wait_total", "Cumulative count of requests that had to wait for a free database connection.", func(_, _, _ int, waitCount int64) float64 { return float64(waitCount) }},
	}

	for _, gauge := range poolGauges {
		value := gauge.value
		promauto.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: optionalMetricNS,
			Subsystem: optionalMetricSS,
			Name:      gauge.name,
			Help:      gauge.help,
		}, func() float64 {
			inUse, idle, open, waitCount := config.DBPoolStats()
			return value(inUse, idle, open, waitCount)
		})
	}

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "logins_today",
		Help:      "Successful login events observed today in Asia/Bangkok time.",
	}, func() float64 {
		start, end := m.todayBounds()
		return m.countSystemLogsBetween(start, end, []string{"auth.login.success", "2fa_login_success"})
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "failed_logins_today",
		Help:      "Failed login events observed today in Asia/Bangkok time.",
	}, func() float64 {
		start, end := m.todayBounds()
		return m.countSystemLogsBetween(start, end, []string{"auth.login.failed"})
	})

	m.registerOptionalGauge("attendance_today", "attendance_records", "Attendance records created today in Asia/Bangkok time.", func() float64 {
		start, end := m.todayBounds()
		var count int64
		if err := config.DB.Model(&models.AttendanceRecord{}).
			Where("created_at >= ? AND created_at < ?", start, end).
			Count(&count).Error; err != nil {
			log.Printf("warning: attendance_today metric query failed: %v", err)
			return 0
		}
		return float64(count)
	})

	m.registerOptionalGauge("queue_waiting", "queue_bookings", "Queue bookings currently waiting across active or paused queue sessions.", func() float64 {
		var count int64
		if err := config.DB.Table("queue_bookings AS qb").
			Joins("JOIN queue_sessions AS qs ON qs.id = qb.queue_session_id").
			Where("qb.status = ? AND qs.status IN ?", "waiting", []string{"active", "paused"}).
			Count(&count).Error; err != nil {
			log.Printf("warning: queue_waiting metric query failed: %v", err)
			return 0
		}
		return float64(count)
	})

	m.registerOptionalGauge("pending_grade_change_requests", "score_edit_requests", "Pending score edit requests awaiting review.", func() float64 {
		var count int64
		if err := config.DB.Model(&models.ScoreEditRequest{}).
			Where("status = ?", "pending").
			Count(&count).Error; err != nil {
			log.Printf("warning: pending_grade_change_requests metric query failed: %v", err)
			return 0
		}
		return float64(count)
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "db_pool_open_connections",
		Help:      "Current number of open database connections in the Go sql.DB pool.",
	}, func() float64 {
		stats, ok := m.sqlDBStats()
		if !ok {
			return 0
		}
		return float64(stats.OpenConnections)
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "db_pool_in_use_connections",
		Help:      "Current number of in-use database connections in the Go sql.DB pool.",
	}, func() float64 {
		stats, ok := m.sqlDBStats()
		if !ok {
			return 0
		}
		return float64(stats.InUse)
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "db_pool_idle_connections",
		Help:      "Current number of idle database connections in the Go sql.DB pool.",
	}, func() float64 {
		stats, ok := m.sqlDBStats()
		if !ok {
			return 0
		}
		return float64(stats.Idle)
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "db_server_active_connections",
		Help:      "Current number of active PostgreSQL server connections from pg_stat_activity.",
	}, func() float64 {
		var count int64
		if err := config.DB.Raw("SELECT COUNT(*) FROM pg_stat_activity").Scan(&count).Error; err != nil {
			log.Printf("warning: db_server_active_connections metric query failed: %v", err)
			return 0
		}
		return float64(count)
	})

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: optionalMetricNS,
		Subsystem: optionalMetricSS,
		Name:      "db_server_max_connections",
		Help:      "Configured PostgreSQL max_connections value.",
	}, func() float64 {
		var raw string
		if err := config.DB.Raw("SHOW max_connections").Scan(&raw).Error; err != nil {
			log.Printf("warning: db_server_max_connections metric query failed: %v", err)
			return 0
		}
		var value float64
		fmt.Sscanf(raw, "%f", &value)
		return value
	})
}

func (m *prometheusMetrics) registerOptionalGauge(metricName string, tableName string, help string, fn func() float64) {
	available := m.db != nil && m.db.Migrator().HasTable(tableName)
	if available {
		promauto.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: optionalMetricNS,
			Subsystem: optionalMetricSS,
			Name:      metricName,
			Help:      help,
		}, fn)
	}

	if m.optionalAvailable != nil {
		value := 0.0
		if available {
			value = 1.0
		} else {
			log.Printf("info: optional metric %s skipped because table %s is not present", metricName, tableName)
		}
		m.optionalAvailable.WithLabelValues(metricName).Set(value)
	}
}

func (m *prometheusMetrics) sqlDBStats() (stats struct {
	OpenConnections int
	InUse           int
	Idle            int
}, ok bool) {
	if m.db == nil {
		return stats, false
	}
	sqlDB, err := m.db.DB()
	if err != nil {
		return stats, false
	}
	raw := sqlDB.Stats()
	stats.OpenConnections = raw.OpenConnections
	stats.InUse = raw.InUse
	stats.Idle = raw.Idle
	return stats, true
}

func (m *prometheusMetrics) todayBounds() (time.Time, time.Time) {
	now := time.Now().In(m.loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, m.loc)
	return start.UTC(), start.Add(24 * time.Hour).UTC()
}

func (m *prometheusMetrics) countSystemLogsBetween(start time.Time, end time.Time, actions []string) float64 {
	if len(actions) == 0 {
		return 0
	}

	normalizedActions := append([]string(nil), actions...)
	sort.Strings(normalizedActions)

	var count int64
	if err := config.DB.Model(&models.SystemLog{}).
		Where("created_at >= ? AND created_at < ? AND action IN ?", start, end, normalizedActions).
		Count(&count).Error; err != nil {
		log.Printf("warning: system log metric query failed for actions %s: %v", strings.Join(normalizedActions, ","), err)
		return 0
	}

	return float64(count)
}

func RecordHTTPRequest(path string, method string, status int, duration time.Duration) {
	if metricsManager.httpRequestsTotal == nil || path == "" || method == "" {
		return
	}

	statusLabel := fmt.Sprintf("%d", status)
	metricsManager.httpRequestsTotal.WithLabelValues(path, method, statusLabel).Inc()
	metricsManager.httpRequestLatency.WithLabelValues(path, method).Observe(duration.Seconds())

	switch {
	case status >= http.StatusInternalServerError:
		metricsManager.httpErrorsTotal.WithLabelValues(path, method, "5xx").Inc()
	case status >= http.StatusBadRequest:
		metricsManager.httpErrorsTotal.WithLabelValues(path, method, "4xx").Inc()
	}
}

func TrackAuthenticatedPrincipal(kind string, id uint) {
	metricsManager.activeSessions.touch(kind, id, time.Now())
}

func MetricsHandler(c fiber.Ctx) error {
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	var payload bytes.Buffer
	encoder := expfmt.NewEncoder(&payload, expfmt.FmtText)
	for _, family := range metricFamilies {
		if err := encoder.Encode(family); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
	}

	c.Set(fiber.HeaderContentType, string(expfmt.FmtText))
	return c.Send(payload.Bytes())
}
