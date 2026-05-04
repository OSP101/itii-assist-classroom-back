const express = require('express');
const router = express.Router();
const { systemController } = require('../controllers');
const { authenticate, authorize } = require('../middlewares/auth');

/**
 * System Metrics Routes
 * All routes require admin authentication
 */

// Get all system metrics
router.get(
  '/metrics',
  authenticate,
  authorize('admin'),
  systemController.getSystemMetrics
);

// Get CPU usage only
router.get(
  '/cpu',
  authenticate,
  authorize('admin'),
  systemController.getCpuUsage
);

// Get memory usage only
router.get(
  '/memory',
  authenticate,
  authorize('admin'),
  systemController.getMemoryUsage
);

// Get server info
router.get(
  '/info',
  authenticate,
  authorize('admin'),
  systemController.getServerInfo
);

module.exports = router;
