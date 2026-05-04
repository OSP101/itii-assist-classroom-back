/**
 * Monitoring Routes
 * 
 * Provides three admin-only JSON endpoints that query Prometheus
 * and return simplified metrics:
 *   GET /api/monitoring/system     → CPU, RAM, disk, network, load
 *   GET /api/monitoring/containers → container CPU, memory, restarts, status
 *   GET /api/monitoring/website    → uptime, response time, error rate, status codes
 * 
 * Also:
 *   GET  /api/metrics/prometheus   → raw Prometheus metrics (scraped by Prometheus)
 *   POST /api/monitoring/webhook   → receives Alertmanager webhook payloads
 * 
 * Security: all /monitoring/* routes require JWT + admin role.
 */

const express = require('express');
const http = require('http');
const router = express.Router();
const { authenticate, authorize } = require('../middlewares/auth');
const { register } = require('../middlewares/metrics');
const config = require('../config');
const { logger } = require('../utils');

// Prometheus URL (internal Docker network)
const PROMETHEUS_URL = process.env.PROMETHEUS_URL || 'http://itii-prometheus:9090';

// Docker Socket path (mounted read-only for container metrics)
const DOCKER_SOCKET = process.env.DOCKER_SOCKET || '/var/run/docker.sock';

// ---------------------------------------------------------------------------
// Helper: query Prometheus instant query API
// ---------------------------------------------------------------------------
async function queryPrometheus(promql) {
  try {
    const url = `${PROMETHEUS_URL}/api/v1/query?query=${encodeURIComponent(promql)}`;
    const response = await fetch(url, { signal: AbortSignal.timeout(5000) });
    const data = await response.json();

    if (data.status !== 'success') {
      throw new Error(`Prometheus query failed: ${data.error || 'unknown'}`);
    }

    return data.data.result;
  } catch (error) {
    logger.error(`Prometheus query error [${promql}]:`, error.message);
    return null;
  }
}

// ---------------------------------------------------------------------------
// Helper: extract single numeric value from Prometheus result
// ---------------------------------------------------------------------------
function extractValue(result) {
  if (!result || result.length === 0) return null;
  const val = parseFloat(result[0].value[1]);
  return isNaN(val) ? null : val;
}

// ---------------------------------------------------------------------------
// Helper: extract multiple values with labels
// ---------------------------------------------------------------------------
function extractMultiple(result, labelKey = 'name') {
  if (!result || result.length === 0) return [];
  return result.map((r) => ({
    label: r.metric[labelKey] || r.metric.instance || 'unknown',
    value: parseFloat(r.value[1]) || 0,
    metric: r.metric,
  }));
}

// ---------------------------------------------------------------------------
// GET /api/metrics/prometheus
// Raw Prometheus metrics endpoint (no auth - scraped by Prometheus itself)
// ---------------------------------------------------------------------------
router.get('/prometheus', async (req, res) => {
  try {
    res.set('Content-Type', register.contentType);
    res.end(await register.metrics());
  } catch (error) {
    res.status(500).end(error.message);
  }
});

// =========================================================================
// Protected monitoring endpoints (JWT + admin only)
// =========================================================================

// ---------------------------------------------------------------------------
// GET /api/monitoring/system
// Returns: CPU, RAM, disk, disk IO, network, load average
// ---------------------------------------------------------------------------
router.get('/system', authenticate, authorize('admin'), async (req, res) => {
  try {
    // Fire all queries in parallel for speed
    const [
      cpuResult,
      memResult,
      memTotalResult,
      memAvailResult,
      diskResult,
      diskTotalResult,
      diskAvailResult,
      diskReadResult,
      diskWriteResult,
      netRxResult,
      netTxResult,
      load1Result,
      load5Result,
      load15Result,
      uptimeResult,
    ] = await Promise.all([
      // CPU usage %
      queryPrometheus('100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)'),
      // Memory usage %
      queryPrometheus('(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100'),
      // Total memory bytes
      queryPrometheus('node_memory_MemTotal_bytes'),
      // Available memory bytes
      queryPrometheus('node_memory_MemAvailable_bytes'),
      // Disk usage %
      queryPrometheus('(1 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"})) * 100'),
      // Disk total bytes
      queryPrometheus('node_filesystem_size_bytes{mountpoint="/"}'),
      // Disk available bytes
      queryPrometheus('node_filesystem_avail_bytes{mountpoint="/"}'),
      // Disk read bytes/sec
      queryPrometheus('sum(rate(node_disk_read_bytes_total[5m]))'),
      // Disk write bytes/sec
      queryPrometheus('sum(rate(node_disk_written_bytes_total[5m]))'),
      // Network receive bytes/sec
      queryPrometheus('sum(rate(node_network_receive_bytes_total{device!~"lo|veth.*|docker.*|br-.*"}[5m]))'),
      // Network transmit bytes/sec
      queryPrometheus('sum(rate(node_network_transmit_bytes_total{device!~"lo|veth.*|docker.*|br-.*"}[5m]))'),
      // Load averages
      queryPrometheus('node_load1'),
      queryPrometheus('node_load5'),
      queryPrometheus('node_load15'),
      // Uptime seconds
      queryPrometheus('node_time_seconds - node_boot_time_seconds'),
    ]);

    const cpu = extractValue(cpuResult);
    const memPercent = extractValue(memResult);
    const memTotal = extractValue(memTotalResult);
    const memAvail = extractValue(memAvailResult);
    const diskPercent = extractValue(diskResult);
    const diskTotal = extractValue(diskTotalResult);
    const diskAvail = extractValue(diskAvailResult);

    res.json({
      success: true,
      data: {
        cpu: {
          usagePercent: cpu !== null ? parseFloat(cpu.toFixed(2)) : null,
          status: cpu === null ? 'unknown' : cpu > 85 ? 'critical' : cpu > 70 ? 'warning' : 'normal',
        },
        memory: {
          usagePercent: memPercent !== null ? parseFloat(memPercent.toFixed(2)) : null,
          totalBytes: memTotal,
          availableBytes: memAvail,
          usedBytes: memTotal && memAvail ? memTotal - memAvail : null,
          status: memPercent === null ? 'unknown' : memPercent > 90 ? 'critical' : memPercent > 75 ? 'warning' : 'normal',
        },
        disk: {
          usagePercent: diskPercent !== null ? parseFloat(diskPercent.toFixed(2)) : null,
          totalBytes: diskTotal,
          availableBytes: diskAvail,
          usedBytes: diskTotal && diskAvail ? diskTotal - diskAvail : null,
          io: {
            readBytesPerSec: extractValue(diskReadResult),
            writeBytesPerSec: extractValue(diskWriteResult),
          },
          status: diskPercent === null ? 'unknown' : diskPercent > 90 ? 'critical' : diskPercent > 80 ? 'warning' : 'normal',
        },
        network: {
          receiveBytesPerSec: extractValue(netRxResult),
          transmitBytesPerSec: extractValue(netTxResult),
        },
        load: {
          load1: extractValue(load1Result),
          load5: extractValue(load5Result),
          load15: extractValue(load15Result),
        },
        uptime: {
          seconds: extractValue(uptimeResult),
        },
        timestamp: new Date().toISOString(),
      },
    });
  } catch (error) {
    logger.error('Error fetching system metrics:', error);
    res.status(500).json({ success: false, message: 'Failed to fetch system metrics' });
  }
});

// ---------------------------------------------------------------------------
// Helper: call Docker Engine API via Unix socket
// ---------------------------------------------------------------------------
function dockerApiGet(path, timeoutMs = 5000) {
  return new Promise((resolve, reject) => {
    const options = {
      socketPath: DOCKER_SOCKET,
      path: `/v1.44${path}`,
      method: 'GET',
    };

    const req = http.get(options, (res) => {
      let data = '';
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => {
        try { resolve(JSON.parse(data)); }
        catch (e) { reject(new Error(`Docker API parse error: ${e.message}`)); }
      });
    });

    req.on('error', (err) => reject(new Error(`Docker API error: ${err.message}`)));
    req.setTimeout(timeoutMs, () => {
      req.destroy();
      reject(new Error('Docker API timeout'));
    });
  });
}

// ---------------------------------------------------------------------------
// Helper: compute CPU % from Docker stats snapshot
// ---------------------------------------------------------------------------
function calculateCpuPercent(stats) {
  const cpuDelta = stats.cpu_stats.cpu_usage.total_usage - stats.precpu_stats.cpu_usage.total_usage;
  const systemDelta = stats.cpu_stats.system_cpu_usage - stats.precpu_stats.system_cpu_usage;
  const numCpus = stats.cpu_stats.online_cpus || stats.cpu_stats.cpu_usage.percpu_usage?.length || 1;

  if (systemDelta > 0 && cpuDelta >= 0) {
    return (cpuDelta / systemDelta) * numCpus * 100;
  }
  return 0;
}

// ---------------------------------------------------------------------------
// GET /api/monitoring/containers
// Returns: per-container CPU, memory, restart count, status
// Uses Docker Engine API directly (cAdvisor has issues with containerd
// snapshotter on Docker v29+ where container `name` labels are missing)
// ---------------------------------------------------------------------------
router.get('/containers', authenticate, authorize('admin'), async (req, res) => {
  try {
    // Get list of all containers (running + stopped)
    const containerList = await dockerApiGet('/containers/json?all=true');

    // Filter only itii-* containers
    const itiiContainers = containerList.filter((c) => {
      const name = (c.Names?.[0] || '').replace(/^\//, '');
      return name.startsWith('itii-');
    });

    // Fetch stats for each running container in parallel
    const containers = await Promise.all(
      itiiContainers.map(async (c) => {
        const name = (c.Names?.[0] || '').replace(/^\//, '');
        const isRunning = c.State === 'running';

        let cpuPercent = 0;
        let memoryBytes = 0;
        let memoryLimitBytes = 0;
        let memoryPercent = 0;

        if (isRunning) {
          try {
            const stats = await dockerApiGet(`/containers/${c.Id}/stats?stream=false&one-shot=true`, 8000);

            cpuPercent = calculateCpuPercent(stats);
            memoryBytes = stats.memory_stats?.usage || 0;
            memoryLimitBytes = stats.memory_stats?.limit || 0;
            memoryPercent = memoryLimitBytes > 0 ? (memoryBytes / memoryLimitBytes) * 100 : 0;
          } catch (statErr) {
            logger.warn(`Failed to get stats for ${name}: ${statErr.message}`);
          }
        }

        return {
          name,
          cpuPercent: parseFloat(cpuPercent.toFixed(2)),
          memoryBytes,
          memoryLimitBytes,
          memoryPercent: parseFloat(memoryPercent.toFixed(2)),
          restarts: c.RestartCount || 0,
          status: isRunning ? 'running' : c.State || 'stopped',
          image: c.Image,
          created: new Date(c.Created * 1000).toISOString(),
        };
      })
    );

    // Sort: running first, then by name
    containers.sort((a, b) => {
      if (a.status === 'running' && b.status !== 'running') return -1;
      if (a.status !== 'running' && b.status === 'running') return 1;
      return a.name.localeCompare(b.name);
    });

    res.json({
      success: true,
      data: {
        containers,
        total: containers.length,
        running: containers.filter((c) => c.status === 'running').length,
        stopped: containers.filter((c) => c.status !== 'running').length,
        timestamp: new Date().toISOString(),
      },
    });
  } catch (error) {
    logger.error('Error fetching container metrics:', error);

    // Fallback: return empty but valid response if Docker socket unavailable
    res.json({
      success: true,
      data: {
        containers: [],
        total: 0,
        running: 0,
        stopped: 0,
        error: 'Docker socket unavailable — ensure /var/run/docker.sock is mounted',
        timestamp: new Date().toISOString(),
      },
    });
  }
});

// ---------------------------------------------------------------------------
// GET /api/monitoring/website
// Returns: uptime status, response time, HTTP status codes, error rate
// ---------------------------------------------------------------------------
router.get('/website', authenticate, authorize('admin'), async (req, res) => {
  try {
    const [
      uptimeResult,
      p50Result,
      p95Result,
      p99Result,
      requestRateResult,
      errorRateResult,
      statusCodesResult,
      inFlightResult,
    ] = await Promise.all([
      // Backend up status (1 = up, 0 = down)
      queryPrometheus('up{job="backend"}'),
      // Response time percentiles
      queryPrometheus('histogram_quantile(0.50, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))'),
      queryPrometheus('histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))'),
      queryPrometheus('histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))'),
      // Total request rate (req/sec)
      queryPrometheus('sum(rate(http_requests_total[5m]))'),
      // Error rate (5xx percentage)
      queryPrometheus('sum(rate(http_requests_total{status_code=~"5.."}[5m])) / sum(rate(http_requests_total[5m])) * 100'),
      // Requests by status code
      queryPrometheus('sum by (status_code) (rate(http_requests_total[5m]))'),
      // In-flight requests
      queryPrometheus('http_requests_in_flight'),
    ]);

    const isUp = extractValue(uptimeResult);
    const errorRate = extractValue(errorRateResult);

    // Parse status code breakdown
    const statusCodes = {};
    if (statusCodesResult) {
      statusCodesResult.forEach((r) => {
        const code = r.metric.status_code;
        statusCodes[code] = parseFloat(parseFloat(r.value[1]).toFixed(4));
      });
    }

    res.json({
      success: true,
      data: {
        uptime: {
          isUp: isUp === 1,
          status: isUp === 1 ? 'online' : 'offline',
        },
        responseTime: {
          p50: extractValue(p50Result),
          p95: extractValue(p95Result),
          p99: extractValue(p99Result),
          unit: 'seconds',
        },
        requestRate: {
          perSecond: extractValue(requestRateResult),
        },
        errorRate: {
          percent: errorRate !== null ? parseFloat(errorRate.toFixed(2)) : 0,
          status: errorRate === null ? 'unknown' : errorRate > 5 ? 'critical' : errorRate > 1 ? 'warning' : 'normal',
        },
        statusCodes,
        inFlightRequests: extractValue(inFlightResult),
        timestamp: new Date().toISOString(),
      },
    });
  } catch (error) {
    logger.error('Error fetching website metrics:', error);
    res.status(500).json({ success: false, message: 'Failed to fetch website metrics' });
  }
});

// ---------------------------------------------------------------------------
// POST /api/monitoring/webhook
// Receives Alertmanager webhook payloads and logs them.
// In production, forward to Slack/Discord/Teams/custom notification system.
// ---------------------------------------------------------------------------
router.post('/webhook', (req, res) => {
  const payload = req.body;

  // Log each alert
  if (payload && payload.alerts) {
    payload.alerts.forEach((alert) => {
      const level = alert.labels?.severity === 'critical' ? 'error' : 'warn';
      logger[level](`[ALERT ${alert.status.toUpperCase()}] ${alert.labels?.alertname}: ${alert.annotations?.description || alert.annotations?.summary}`);
    });
  }

  // TODO: Forward to your notification service (Slack, Discord, Line, etc.)
  // Example: await notificationService.send(payload);

  res.json({ success: true, message: 'Webhook received' });
});

module.exports = router;
