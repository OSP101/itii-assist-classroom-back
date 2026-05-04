/**
 * Prometheus Metrics Middleware
 * 
 * Collects HTTP request metrics using prom-client:
 * - http_requests_total: counter of requests by method, route, status
 * - http_request_duration_seconds: histogram of response times
 * - http_requests_in_flight: gauge of concurrent requests
 * 
 * These metrics are scraped by Prometheus at /api/metrics/prometheus
 */

const client = require('prom-client');

// Create a registry for our custom metrics
const register = new client.Registry();

// Add default Node.js metrics (event loop lag, heap, GC, etc.)
client.collectDefaultMetrics({ register, prefix: 'nodejs_' });

// ---------------------------------------------------------------------------
// Custom HTTP Metrics
// ---------------------------------------------------------------------------

// Counter: total HTTP requests by method, route, status code
const httpRequestsTotal = new client.Counter({
  name: 'http_requests_total',
  help: 'Total number of HTTP requests',
  labelNames: ['method', 'route', 'status_code'],
  registers: [register],
});

// Histogram: HTTP request duration in seconds
const httpRequestDuration = new client.Histogram({
  name: 'http_request_duration_seconds',
  help: 'HTTP request duration in seconds',
  labelNames: ['method', 'route', 'status_code'],
  // Buckets: 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s
  buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10],
  registers: [register],
});

// Gauge: currently in-flight requests
const httpRequestsInFlight = new client.Gauge({
  name: 'http_requests_in_flight',
  help: 'Number of HTTP requests currently being processed',
  registers: [register],
});

// Gauge: application uptime
const appUptime = new client.Gauge({
  name: 'app_uptime_seconds',
  help: 'Application uptime in seconds',
  registers: [register],
});

// Update uptime every 5 seconds
setInterval(() => {
  appUptime.set(process.uptime());
}, 5000);

/**
 * Normalize route path to avoid high cardinality labels.
 * E.g., /api/users/123 → /api/users/:id
 */
function normalizeRoute(req) {
  // Use Express matched route if available
  if (req.route && req.route.path) {
    return req.baseUrl + req.route.path;
  }
  // Fallback: replace numeric segments with :id
  return req.path.replace(/\/\d+/g, '/:id');
}

/**
 * Express middleware that records metrics for each request.
 * Attach this BEFORE route handlers in app.js.
 */
function metricsMiddleware(req, res, next) {
  // Skip metrics endpoint itself to avoid self-referential inflation
  if (req.path === '/api/metrics/prometheus') {
    return next();
  }

  const startTime = process.hrtime.bigint();
  httpRequestsInFlight.inc();

  // When response finishes, record metrics
  res.on('finish', () => {
    const durationNs = Number(process.hrtime.bigint() - startTime);
    const durationSec = durationNs / 1e9;
    const route = normalizeRoute(req);
    const labels = {
      method: req.method,
      route,
      status_code: res.statusCode,
    };

    httpRequestsTotal.inc(labels);
    httpRequestDuration.observe(labels, durationSec);
    httpRequestsInFlight.dec();
  });

  next();
}

module.exports = {
  register,
  metricsMiddleware,
  httpRequestsTotal,
  httpRequestDuration,
  httpRequestsInFlight,
};
