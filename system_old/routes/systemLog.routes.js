const express = require('express');
const router = express.Router();
const { systemLogController } = require('../controllers');
const { authenticate, authorize } = require('../middlewares/auth');

/**
 * System Logs Routes
 * ระบบบันทึก Log ตามมาตรฐาน พ.ร.บ. คอมพิวเตอร์ พ.ศ. 2550
 * All routes require admin authentication
 */

// Get filter options (log types, severity levels)
router.get(
  '/filters',
  authenticate,
  authorize('admin'),
  systemLogController.getFilterOptions
);

// Get log statistics
router.get(
  '/stats',
  authenticate,
  authorize('admin'),
  systemLogController.getLogStats
);

// Get logs timeline for charts
router.get(
  '/timeline',
  authenticate,
  authorize('admin'),
  systemLogController.getLogsTimeline
);

// Export logs as CSV
router.get(
  '/export',
  authenticate,
  authorize('admin'),
  systemLogController.exportLogs
);

// Get recent errors
router.get(
  '/errors/recent',
  authenticate,
  authorize('admin'),
  systemLogController.getRecentErrors
);

// Get recent security events
router.get(
  '/security/recent',
  authenticate,
  authorize('admin'),
  systemLogController.getRecentSecurityEvents
);

// Get all logs with filters and pagination
router.get(
  '/',
  authenticate,
  authorize('admin'),
  systemLogController.getLogs
);

// Get single log by ID
router.get(
  '/:id',
  authenticate,
  authorize('admin'),
  systemLogController.getLogById
);

// Cleanup old logs (manual trigger)
router.post(
  '/cleanup',
  authenticate,
  authorize('admin'),
  systemLogController.cleanupLogs
);

module.exports = router;