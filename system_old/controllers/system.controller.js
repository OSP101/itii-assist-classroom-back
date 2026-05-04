const os = require('os');
const osUtils = require('os-utils');
const { asyncHandler } = require('../utils');

/**
 * Get system metrics (CPU, RAM, Disk, etc.)
 * @route GET /api/system/metrics
 * @access Admin only
 */
const getSystemMetrics = asyncHandler(async (req, res) => {
  // Get CPU usage and free percentage in parallel (each takes ~1s to sample)
  const [cpuUsage, cpuFree] = await Promise.all([
    new Promise((resolve) => osUtils.cpuUsage((usage) => resolve(usage))),
    new Promise((resolve) => osUtils.cpuFree((free) => resolve(free))),
  ]);

  // Memory info
  const totalMemory = os.totalmem();
  const freeMemory = os.freemem();
  const usedMemory = totalMemory - freeMemory;
  const memoryUsagePercent = (usedMemory / totalMemory) * 100;

  // System info
  const systemInfo = {
    platform: os.platform(),
    arch: os.arch(),
    hostname: os.hostname(),
    type: os.type(),
    release: os.release(),
    uptime: os.uptime(),
    nodeVersion: process.version,
  };

  // CPU info
  const cpus = os.cpus();
  const cpuInfo = {
    model: cpus[0]?.model || 'Unknown',
    cores: cpus.length,
    speed: cpus[0]?.speed || 0,
  };

  // Load average (Unix only, returns [0,0,0] on Windows)
  const loadAverage = os.loadavg();

  // Process info
  const processMemory = process.memoryUsage();
  const processInfo = {
    pid: process.pid,
    uptime: process.uptime(),
    memoryUsage: {
      rss: processMemory.rss,
      heapTotal: processMemory.heapTotal,
      heapUsed: processMemory.heapUsed,
      external: processMemory.external,
    },
  };

  res.json({
    success: true,
    data: {
      cpu: {
        usage: Math.round(cpuUsage * 100 * 100) / 100, // percentage with 2 decimals
        free: Math.round(cpuFree * 100 * 100) / 100,
        info: cpuInfo,
      },
      memory: {
        total: totalMemory,
        free: freeMemory,
        used: usedMemory,
        usagePercent: Math.round(memoryUsagePercent * 100) / 100,
        // Human readable
        totalGB: Math.round((totalMemory / 1024 / 1024 / 1024) * 100) / 100,
        freeGB: Math.round((freeMemory / 1024 / 1024 / 1024) * 100) / 100,
        usedGB: Math.round((usedMemory / 1024 / 1024 / 1024) * 100) / 100,
      },
      system: systemInfo,
      loadAverage: {
        '1min': loadAverage[0],
        '5min': loadAverage[1],
        '15min': loadAverage[2],
      },
      process: processInfo,
      timestamp: new Date().toISOString(),
    },
  });
});

/**
 * Get CPU usage only (lightweight)
 * @route GET /api/system/cpu
 * @access Admin only
 */
const getCpuUsage = asyncHandler(async (req, res) => {
  const cpuUsage = await new Promise((resolve) => {
    osUtils.cpuUsage((usage) => {
      resolve(usage);
    });
  });

  const cpus = os.cpus();

  res.json({
    success: true,
    data: {
      usage: Math.round(cpuUsage * 100 * 100) / 100,
      cores: cpus.length,
      model: cpus[0]?.model || 'Unknown',
      timestamp: new Date().toISOString(),
    },
  });
});

/**
 * Get memory usage only (lightweight)
 * @route GET /api/system/memory
 * @access Admin only
 */
const getMemoryUsage = asyncHandler(async (req, res) => {
  const totalMemory = os.totalmem();
  const freeMemory = os.freemem();
  const usedMemory = totalMemory - freeMemory;
  const usagePercent = (usedMemory / totalMemory) * 100;

  res.json({
    success: true,
    data: {
      total: totalMemory,
      free: freeMemory,
      used: usedMemory,
      usagePercent: Math.round(usagePercent * 100) / 100,
      totalGB: Math.round((totalMemory / 1024 / 1024 / 1024) * 100) / 100,
      freeGB: Math.round((freeMemory / 1024 / 1024 / 1024) * 100) / 100,
      usedGB: Math.round((usedMemory / 1024 / 1024 / 1024) * 100) / 100,
      timestamp: new Date().toISOString(),
    },
  });
});

/**
 * Get server uptime and basic info
 * @route GET /api/system/info
 * @access Admin only
 */
const getServerInfo = asyncHandler(async (req, res) => {
  const uptimeSeconds = os.uptime();
  const days = Math.floor(uptimeSeconds / 86400);
  const hours = Math.floor((uptimeSeconds % 86400) / 3600);
  const minutes = Math.floor((uptimeSeconds % 3600) / 60);
  const seconds = Math.floor(uptimeSeconds % 60);

  res.json({
    success: true,
    data: {
      hostname: os.hostname(),
      platform: os.platform(),
      arch: os.arch(),
      nodeVersion: process.version,
      uptime: {
        seconds: uptimeSeconds,
        formatted: `${days}d ${hours}h ${minutes}m ${seconds}s`,
      },
      processUptime: {
        seconds: process.uptime(),
        formatted: formatUptime(process.uptime()),
      },
      timestamp: new Date().toISOString(),
    },
  });
});

/**
 * Format uptime to human readable string
 * @param {number} seconds 
 * @returns {string}
 */
function formatUptime(seconds) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = Math.floor(seconds % 60);
  return `${days}d ${hours}h ${minutes}m ${secs}s`;
}

module.exports = {
  getSystemMetrics,
  getCpuUsage,
  getMemoryUsage,
  getServerInfo,
};
