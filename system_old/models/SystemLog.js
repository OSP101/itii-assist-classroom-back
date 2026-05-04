const { DataTypes, Op } = require('sequelize');
const { sequelize } = require('../config/database');

/**
 * SystemLog Model
 * ระบบบันทึก Log ตามมาตรฐาน พ.ร.บ. คอมพิวเตอร์ พ.ศ. 2550
 * - เก็บข้อมูลอย่างน้อย 90 วัน
 * - รองรับ Access Logs, Error Logs, Auth Logs, Security Logs
 */

// Log Types
const LOG_TYPES = {
  ACCESS: 'access',      // การเข้าถึงระบบทั่วไป
  ERROR: 'error',        // ข้อผิดพลาดของระบบ
  AUTH: 'auth',          // การยืนยันตัวตน
  SECURITY: 'security',  // เหตุการณ์ด้านความปลอดภัย
};

// Severity Levels
const SEVERITY_LEVELS = {
  DEBUG: 'debug',
  INFO: 'info',
  WARN: 'warn',
  ERROR: 'error',
  CRITICAL: 'critical',
};

const SystemLog = sequelize.define('SystemLog', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  log_type: {
    type: DataTypes.ENUM('access', 'error', 'auth', 'security'),
    allowNull: false,
    defaultValue: 'access',
  },
  severity: {
    type: DataTypes.ENUM('debug', 'info', 'warn', 'error', 'critical'),
    allowNull: false,
    defaultValue: 'info',
  },
  actor_user_id: {
    type: DataTypes.BIGINT,
    allowNull: true,
  },
  session_id: {
    type: DataTypes.STRING(128),
    allowNull: true,
  },
  auth_method: {
    type: DataTypes.STRING(50),
    allowNull: true,
  },
  action: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  http_method: {
    type: DataTypes.STRING(10),
    allowNull: true,
  },
  url: {
    type: DataTypes.STRING(2048),
    allowNull: true,
  },
  query_params: {
    type: DataTypes.JSON,
    allowNull: true,
  },
  status_code: {
    type: DataTypes.INTEGER,
    allowNull: true,
  },
  response_time_ms: {
    type: DataTypes.INTEGER,
    allowNull: true,
  },
  detail: {
    type: DataTypes.JSON,
    allowNull: true,
  },
  error_message: {
    type: DataTypes.TEXT,
    allowNull: true,
  },
  error_stack: {
    type: DataTypes.TEXT,
    allowNull: true,
  },
  error_code: {
    type: DataTypes.STRING(50),
    allowNull: true,
  },
  resource_type: {
    type: DataTypes.STRING(100),
    allowNull: true,
  },
  resource_id: {
    type: DataTypes.STRING(255),
    allowNull: true,
  },
  request_body: {
    type: DataTypes.JSON,
    allowNull: true,
  },
  request_size: {
    type: DataTypes.INTEGER,
    allowNull: true,
  },
  response_size: {
    type: DataTypes.INTEGER,
    allowNull: true,
  },
  ip_address: {
    type: DataTypes.STRING(64),
    allowNull: true,
  },
  user_agent: {
    type: DataTypes.STRING(512),
    allowNull: true,
  },
  referer: {
    type: DataTypes.STRING(2048),
    allowNull: true,
  },
  device_type: {
    type: DataTypes.STRING(50),
    allowNull: true,
  },
  browser: {
    type: DataTypes.STRING(100),
    allowNull: true,
  },
  os: {
    type: DataTypes.STRING(100),
    allowNull: true,
  },
}, {
  tableName: 'system_logs',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: false,
  indexes: [
    { fields: ['created_at'] },
    { fields: ['log_type', 'created_at'] },
    { fields: ['actor_user_id', 'created_at'] },
  ],
});

/**
 * Static method: Create basic log entry
 */
SystemLog.log = async function(action, detail = null, actorUserId = null, ipAddress = null) {
  return this.create({
    log_type: LOG_TYPES.ACCESS,
    severity: SEVERITY_LEVELS.INFO,
    action,
    detail,
    actor_user_id: actorUserId,
    ip_address: ipAddress,
  });
};

/**
 * Static method: Log access (การเข้าถึงระบบ)
 */
SystemLog.logAccess = async function(data) {
  return this.create({
    log_type: LOG_TYPES.ACCESS,
    severity: data.severity || SEVERITY_LEVELS.INFO,
    ...data,
  });
};

/**
 * Static method: Log error (ข้อผิดพลาด)
 */
SystemLog.logError = async function(data) {
  return this.create({
    log_type: LOG_TYPES.ERROR,
    severity: data.severity || SEVERITY_LEVELS.ERROR,
    ...data,
  });
};

/**
 * Static method: Log auth (การยืนยันตัวตน)
 */
SystemLog.logAuth = async function(data) {
  return this.create({
    log_type: LOG_TYPES.AUTH,
    severity: data.severity || SEVERITY_LEVELS.INFO,
    ...data,
  });
};

/**
 * Static method: Log security (เหตุการณ์ด้านความปลอดภัย)
 */
SystemLog.logSecurity = async function(data) {
  return this.create({
    log_type: LOG_TYPES.SECURITY,
    severity: data.severity || SEVERITY_LEVELS.WARN,
    ...data,
  });
};

/**
 * Static method: Get logs with filters
 */
SystemLog.getLogs = async function(filters = {}) {
  const {
    logType,
    severity,
    userId,
    startDate,
    endDate,
    search,
    page = 1,
    limit = 50,
    sortBy = 'created_at',
    sortOrder = 'DESC',
  } = filters;

  const where = {};

  if (logType) where.log_type = logType;
  if (severity) where.severity = severity;
  if (userId) where.actor_user_id = userId;

  if (startDate || endDate) {
    where.created_at = {};
    if (startDate) where.created_at[Op.gte] = new Date(startDate);
    if (endDate) where.created_at[Op.lte] = new Date(endDate);
  }

  if (search) {
    where[Op.or] = [
      { action: { [Op.like]: `%${search}%` } },
      { url: { [Op.like]: `%${search}%` } },
      { ip_address: { [Op.like]: `%${search}%` } },
      { error_message: { [Op.like]: `%${search}%` } },
    ];
  }

  const offset = (page - 1) * limit;

  const { count, rows } = await this.findAndCountAll({
    where,
    order: [[sortBy, sortOrder]],
    limit: parseInt(limit),
    offset,
  });

  return {
    logs: rows,
    pagination: {
      total: count,
      page: parseInt(page),
      limit: parseInt(limit),
      totalPages: Math.ceil(count / limit),
    },
  };
};

/**
 * Static method: Get statistics
 */
SystemLog.getStats = async function(startDate, endDate) {
  const dateFilter = {};
  if (startDate) dateFilter[Op.gte] = new Date(startDate);
  if (endDate) dateFilter[Op.lte] = new Date(endDate);

  const where = Object.keys(dateFilter).length > 0 
    ? { created_at: dateFilter } 
    : {};

  // Count by log type
  const byType = await this.findAll({
    where,
    attributes: [
      'log_type',
      [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
    ],
    group: ['log_type'],
    raw: true,
  });

  // Count by severity
  const bySeverity = await this.findAll({
    where,
    attributes: [
      'severity',
      [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
    ],
    group: ['severity'],
    raw: true,
  });

  // Count by status code (top 10)
  const byStatusCode = await this.findAll({
    where: { ...where, status_code: { [Op.ne]: null } },
    attributes: [
      'status_code',
      [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
    ],
    group: ['status_code'],
    order: [[sequelize.fn('COUNT', sequelize.col('id')), 'DESC']],
    limit: 10,
    raw: true,
  });

  // Total count
  const total = await this.count({ where });

  // Unique IPs
  const uniqueIps = await this.count({
    where,
    distinct: true,
    col: 'ip_address',
  });

  return {
    total,
    uniqueIps,
    byType,
    bySeverity,
    byStatusCode,
  };
};

/**
 * Static method: Cleanup old logs (older than 90 days)
 * Note: For compliance, archive before deleting
 */
SystemLog.cleanupOldLogs = async function(retentionDays = 90) {
  const cutoffDate = new Date();
  cutoffDate.setDate(cutoffDate.getDate() - retentionDays);

  const deleted = await this.destroy({
    where: {
      created_at: { [Op.lt]: cutoffDate },
    },
  });

  return { deletedCount: deleted, cutoffDate };
};

// Export constants
SystemLog.LOG_TYPES = LOG_TYPES;
SystemLog.SEVERITY_LEVELS = SEVERITY_LEVELS;

module.exports = SystemLog;
