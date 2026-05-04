const SystemLog = require('../models/SystemLog');
const { User } = require('../models');
const { asyncHandler } = require('../utils');
const { ApiError } = require('../utils');

/**
 * Get system logs with filters and pagination
 * @route GET /api/logs
 * @access Admin only
 */
const getLogs = asyncHandler(async (req, res) => {
  const {
    log_type,
    severity,
    user_id,
    start_date,
    end_date,
    search,
    page = 1,
    limit = 50,
    sort_by = 'created_at',
    sort_order = 'DESC',
  } = req.query;

  const result = await SystemLog.getLogs({
    logType: log_type,
    severity,
    userId: user_id,
    startDate: start_date,
    endDate: end_date,
    search,
    page,
    limit,
    sortBy: sort_by,
    sortOrder: sort_order,
  });

  // Attach user info to logs
  const userIds = [...new Set(result.logs.map(log => log.actor_user_id).filter(Boolean))];
  const users = userIds.length > 0 
    ? await User.findAll({ 
        where: { id: userIds },
        attributes: ['id', 'email', 'full_name', 'role'],
      })
    : [];
  
  const usersMap = new Map(users.map(u => [u.id, u]));

  const logsWithUser = result.logs.map(log => {
    const logData = log.toJSON();
    if (logData.actor_user_id && usersMap.has(logData.actor_user_id)) {
      const user = usersMap.get(logData.actor_user_id);
      logData.actor_user = {
        id: user.id,
        email: user.email,
        full_name: user.full_name,
        role: user.role,
      };
    }
    return logData;
  });

  res.json({
    success: true,
    data: {
      logs: logsWithUser,
      pagination: result.pagination,
    },
  });
});

/**
 * Get single log entry by ID
 * @route GET /api/logs/:id
 * @access Admin only
 */
const getLogById = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const log = await SystemLog.findByPk(id);

  if (!log) {
    throw ApiError.notFound('Log entry not found');
  }

  const logData = log.toJSON();

  // Attach user info
  if (logData.actor_user_id) {
    const user = await User.findByPk(logData.actor_user_id, {
      attributes: ['id', 'email', 'full_name', 'role'],
    });
    if (user) {
      logData.actor_user = user.toJSON();
    }
  }

  res.json({
    success: true,
    data: logData,
  });
});

/**
 * Get log statistics
 * @route GET /api/logs/stats
 * @access Admin only
 */
const getLogStats = asyncHandler(async (req, res) => {
  const { start_date, end_date } = req.query;

  const stats = await SystemLog.getStats(start_date, end_date);

  res.json({
    success: true,
    data: stats,
  });
});

/**
 * Get logs timeline (for charts)
 * @route GET /api/logs/timeline
 * @access Admin only
 */
const getLogsTimeline = asyncHandler(async (req, res) => {
  const { 
    start_date, 
    end_date, 
    interval = 'hour', // hour, day, week
    log_type,
  } = req.query;

  const { sequelize } = require('../config/database');
  const { Op } = require('sequelize');

  // Calculate date range
  const endDate = end_date ? new Date(end_date) : new Date();
  const startDate = start_date 
    ? new Date(start_date) 
    : new Date(endDate.getTime() - 24 * 60 * 60 * 1000); // Default: last 24 hours

  const where = {
    created_at: {
      [Op.between]: [startDate, endDate],
    },
  };

  if (log_type) {
    where.log_type = log_type;
  }

  // Determine date format based on interval
  let dateFormat;
  switch (interval) {
    case 'day':
      dateFormat = '%Y-%m-%d';
      break;
    case 'week':
      dateFormat = '%Y-%u'; // Year-Week
      break;
    case 'hour':
    default:
      dateFormat = '%Y-%m-%d %H:00';
  }

  const timeline = await SystemLog.findAll({
    where,
    attributes: [
      [sequelize.fn('DATE_FORMAT', sequelize.col('created_at'), dateFormat), 'time_bucket'],
      'log_type',
      [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
    ],
    group: ['time_bucket', 'log_type'],
    order: [['time_bucket', 'ASC']],
    raw: true,
  });

  res.json({
    success: true,
    data: {
      timeline,
      interval,
      startDate,
      endDate,
    },
  });
});

/**
 * Get unique values for filters (log types, severity levels, etc.)
 * @route GET /api/logs/filters
 * @access Admin only
 */
const getFilterOptions = asyncHandler(async (req, res) => {
  res.json({
    success: true,
    data: {
      logTypes: Object.values(SystemLog.LOG_TYPES),
      severityLevels: Object.values(SystemLog.SEVERITY_LEVELS),
      httpMethods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'],
    },
  });
});

/**
 * Export logs as CSV
 * @route GET /api/logs/export
 * @access Admin only
 */
const exportLogs = asyncHandler(async (req, res) => {
  const {
    log_type,
    severity,
    user_id,
    start_date,
    end_date,
    search,
  } = req.query;

  // Get all logs matching filters (no pagination)
  const result = await SystemLog.getLogs({
    logType: log_type,
    severity,
    userId: user_id,
    startDate: start_date,
    endDate: end_date,
    search,
    limit: 10000, // Max 10000 records for export
    page: 1,
  });

  // Attach user info
  const userIds = [...new Set(result.logs.map(log => log.actor_user_id).filter(Boolean))];
  const users = userIds.length > 0 
    ? await User.findAll({ 
        where: { id: userIds },
        attributes: ['id', 'email', 'full_name'],
      })
    : [];
  
  const usersMap = new Map(users.map(u => [u.id, u]));

  // Generate CSV
  const headers = [
    'ID',
    'Log Type',
    'Severity',
    'Action',
    'HTTP Method',
    'URL',
    'Status Code',
    'Response Time (ms)',
    'IP Address',
    'User',
    'User Agent',
    'Browser',
    'OS',
    'Error Message',
    'Created At',
  ];

  const rows = result.logs.map(log => {
    const user = log.actor_user_id ? usersMap.get(log.actor_user_id) : null;
    return [
      log.id,
      log.log_type,
      log.severity,
      log.action,
      log.http_method || '',
      log.url || '',
      log.status_code || '',
      log.response_time_ms || '',
      log.ip_address || '',
      user ? `${user.full_name} (${user.email})` : '',
      log.user_agent || '',
      log.browser || '',
      log.os || '',
      log.error_message || '',
      log.created_at,
    ].map(val => {
      // Escape CSV values
      const str = String(val);
      if (str.includes(',') || str.includes('"') || str.includes('\n')) {
        return `"${str.replace(/"/g, '""')}"`;
      }
      return str;
    }).join(',');
  });

  const csv = [headers.join(','), ...rows].join('\n');

  // Add BOM for Excel UTF-8 compatibility
  const bom = '\uFEFF';

  res.setHeader('Content-Type', 'text/csv; charset=utf-8');
  res.setHeader('Content-Disposition', `attachment; filename=system_logs_${new Date().toISOString().split('T')[0]}.csv`);
  res.send(bom + csv);
});

/**
 * Get recent error logs
 * @route GET /api/logs/errors/recent
 * @access Admin only
 */
const getRecentErrors = asyncHandler(async (req, res) => {
  const { limit = 10 } = req.query;

  const result = await SystemLog.getLogs({
    logType: 'error',
    limit: parseInt(limit),
    page: 1,
    sortBy: 'created_at',
    sortOrder: 'DESC',
  });

  res.json({
    success: true,
    data: result.logs,
  });
});

/**
 * Get recent security events
 * @route GET /api/logs/security/recent
 * @access Admin only
 */
const getRecentSecurityEvents = asyncHandler(async (req, res) => {
  const { limit = 10 } = req.query;

  const result = await SystemLog.getLogs({
    logType: 'security',
    limit: parseInt(limit),
    page: 1,
    sortBy: 'created_at',
    sortOrder: 'DESC',
  });

  res.json({
    success: true,
    data: result.logs,
  });
});

/**
 * Cleanup old logs (manual trigger)
 * @route DELETE /api/logs/cleanup
 * @access Admin only
 */
const cleanupLogs = asyncHandler(async (req, res) => {
  const { retention_days = 90 } = req.body;

  // Ensure minimum 90 days retention (พ.ร.บ. คอมพิวเตอร์)
  const days = Math.max(90, parseInt(retention_days));

  const result = await SystemLog.cleanupOldLogs(days);

  // Log this action
  await SystemLog.log(
    'ADMIN_LOG_CLEANUP',
    { retention_days: days, deleted_count: result.deletedCount },
    req.user.id,
    req.ip
  );

  res.json({
    success: true,
    data: result,
    message: `Deleted ${result.deletedCount} logs older than ${days} days`,
  });
});

module.exports = {
  getLogs,
  getLogById,
  getLogStats,
  getLogsTimeline,
  getFilterOptions,
  exportLogs,
  getRecentErrors,
  getRecentSecurityEvents,
  cleanupLogs,
};