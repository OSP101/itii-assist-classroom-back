const UAParser = require('ua-parser-js');
const SystemLog = require('../models/SystemLog');

/**
 * Middleware: Request Logger
 * บันทึก Access Log สำหรับทุก Request ตามมาตรฐาน พ.ร.บ. คอมพิวเตอร์ พ.ศ. 2550
 */

// Fields to sanitize from request body (remove sensitive data)
const SENSITIVE_FIELDS = [
  'password',
  'newPassword',
  'confirmPassword',
  'currentPassword',
  'token',
  'refreshToken',
  'accessToken',
  'secret',
  'apiKey',
  'creditCard',
  'cvv',
  'ssn',
];

/**
 * Sanitize object by removing sensitive fields
 */
const sanitizeObject = (obj) => {
  if (!obj || typeof obj !== 'object') return obj;

  const sanitized = { ...obj };
  
  for (const field of SENSITIVE_FIELDS) {
    if (sanitized[field] !== undefined) {
      sanitized[field] = '[REDACTED]';
    }
  }

  // Handle nested objects
  for (const key in sanitized) {
    if (typeof sanitized[key] === 'object' && sanitized[key] !== null) {
      sanitized[key] = sanitizeObject(sanitized[key]);
    }
  }

  return sanitized;
};

/**
 * Parse User-Agent to get device info
 */
const parseUserAgent = (userAgentString) => {
  if (!userAgentString) return {};

  const parser = new UAParser(userAgentString);
  const result = parser.getResult();

  return {
    device_type: result.device.type || 'desktop',
    browser: result.browser.name ? `${result.browser.name} ${result.browser.version || ''}`.trim() : null,
    os: result.os.name ? `${result.os.name} ${result.os.version || ''}`.trim() : null,
  };
};

/**
 * Get client IP address
 */
const getClientIp = (req) => {
  return req.headers['x-forwarded-for']?.split(',')[0].trim()
    || req.headers['x-real-ip']
    || req.connection?.remoteAddress
    || req.socket?.remoteAddress
    || req.ip;
};

/**
 * Determine severity based on status code
 */
const getSeverityByStatus = (statusCode) => {
  if (statusCode >= 500) return SystemLog.SEVERITY_LEVELS.ERROR;
  if (statusCode >= 400) return SystemLog.SEVERITY_LEVELS.WARN;
  return SystemLog.SEVERITY_LEVELS.INFO;
};

/**
 * Request Logger Middleware
 * @param {Object} options - Configuration options
 * @param {boolean} options.logBody - Whether to log request body (default: false for security)
 * @param {Array} options.excludePaths - Paths to exclude from logging
 * @param {boolean} options.logAllRequests - Log all requests or just errors (default: true)
 */
const requestLogger = (options = {}) => {
  const {
    logBody = false,
    excludePaths = ['/api/health', '/api/system/metrics'],
    logAllRequests = true,
  } = options;

  return async (req, res, next) => {
    // Skip excluded paths
    if (excludePaths.some(path => req.originalUrl.startsWith(path))) {
      return next();
    }

    const startTime = Date.now();
    const requestId = `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;

    // Store request ID for tracking
    req.requestId = requestId;

    // Capture original end function
    const originalEnd = res.end;
    let responseBody = '';

    // Override end to capture response
    res.end = function(chunk, encoding) {
      if (chunk) {
        responseBody = chunk.toString();
      }
      res.end = originalEnd;
      return res.end(chunk, encoding);
    };

    // On response finish, log the request
    res.on('finish', async () => {
      try {
        const responseTime = Date.now() - startTime;
        const statusCode = res.statusCode;

        // Skip logging GET requests entirely if logAllRequests is false
        // Only log: non-GET methods (POST, PUT, DELETE, PATCH) that modify data
        // This focuses on "actions" rather than "page views"
        if (!logAllRequests && req.method === 'GET') {
          return;
        }

        const userAgentInfo = parseUserAgent(req.headers['user-agent']);

        const logData = {
          action: `${req.method} ${req.originalUrl.split('?')[0]}`,
          http_method: req.method,
          url: req.originalUrl,
          query_params: Object.keys(req.query).length > 0 ? req.query : null,
          status_code: statusCode,
          response_time_ms: responseTime,
          ip_address: getClientIp(req),
          user_agent: req.headers['user-agent'],
          referer: req.headers['referer'] || null,
          ...userAgentInfo,
          actor_user_id: req.user?.id || null,
          session_id: req.headers['x-session-id'] || null,
          auth_method: req.user ? (req.user.provider || 'jwt') : null,
          request_size: parseInt(req.headers['content-length']) || null,
          response_size: parseInt(res.getHeader('content-length')) || null,
          severity: getSeverityByStatus(statusCode),
        };

        // Log request body if enabled (sanitized)
        if (logBody && req.body && Object.keys(req.body).length > 0) {
          logData.request_body = sanitizeObject(req.body);
        }

        // Add resource info if available
        if (req.params.id) {
          logData.resource_id = req.params.id;
          // Try to extract resource type from URL
          const urlParts = req.originalUrl.split('/');
          const apiIndex = urlParts.indexOf('api');
          if (apiIndex >= 0 && urlParts[apiIndex + 1]) {
            logData.resource_type = urlParts[apiIndex + 1];
          }
        }

        // Log error details for error responses
        if (statusCode >= 400 && res.locals.error) {
          logData.error_message = res.locals.error.message;
          logData.error_stack = res.locals.error.stack;
          logData.error_code = res.locals.error.code || `HTTP_${statusCode}`;
        }

        await SystemLog.logAccess(logData);
      } catch (err) {
        // Don't let logging errors break the application
        console.error('Failed to log request:', err.message);
      }
    });

    next();
  };
};

/**
 * Auth Logger - Log authentication events
 */
const authLogger = {
  /**
   * Log successful login
   */
  logLogin: async (req, user, method = 'password') => {
    const userAgentInfo = parseUserAgent(req.headers['user-agent']);

    await SystemLog.logAuth({
      action: 'AUTH_LOGIN_SUCCESS',
      actor_user_id: user.id,
      ip_address: getClientIp(req),
      user_agent: req.headers['user-agent'],
      ...userAgentInfo,
      auth_method: method,
      detail: {
        email: user.email,
        role: user.role,
      },
      severity: SystemLog.SEVERITY_LEVELS.INFO,
    });
  },

  /**
   * Log failed login
   */
  logLoginFailed: async (req, email, reason = 'Invalid credentials') => {
    const userAgentInfo = parseUserAgent(req.headers['user-agent']);

    await SystemLog.logAuth({
      action: 'AUTH_LOGIN_FAILED',
      ip_address: getClientIp(req),
      user_agent: req.headers['user-agent'],
      ...userAgentInfo,
      detail: {
        email,
        reason,
      },
      severity: SystemLog.SEVERITY_LEVELS.WARN,
    });
  },

  /**
   * Log logout
   */
  logLogout: async (req, user) => {
    await SystemLog.logAuth({
      action: 'AUTH_LOGOUT',
      actor_user_id: user.id,
      ip_address: getClientIp(req),
      user_agent: req.headers['user-agent'],
      severity: SystemLog.SEVERITY_LEVELS.INFO,
    });
  },

  /**
   * Log token refresh
   */
  logTokenRefresh: async (req, user) => {
    await SystemLog.logAuth({
      action: 'AUTH_TOKEN_REFRESH',
      actor_user_id: user.id,
      ip_address: getClientIp(req),
      severity: SystemLog.SEVERITY_LEVELS.DEBUG,
    });
  },

  /**
   * Log password change
   */
  logPasswordChange: async (req, user) => {
    const userAgentInfo = parseUserAgent(req.headers['user-agent']);

    await SystemLog.logAuth({
      action: 'AUTH_PASSWORD_CHANGED',
      actor_user_id: user.id,
      ip_address: getClientIp(req),
      user_agent: req.headers['user-agent'],
      ...userAgentInfo,
      severity: SystemLog.SEVERITY_LEVELS.INFO,
    });
  },

  /**
   * Log password reset request
   */
  logPasswordResetRequest: async (req, email) => {
    await SystemLog.logAuth({
      action: 'AUTH_PASSWORD_RESET_REQUEST',
      ip_address: getClientIp(req),
      detail: { email },
      severity: SystemLog.SEVERITY_LEVELS.INFO,
    });
  },
};

/**
 * Security Logger - Log security-related events
 */
const securityLogger = {
  /**
   * Log rate limit exceeded
   */
  logRateLimitExceeded: async (req) => {
    await SystemLog.logSecurity({
      action: 'SECURITY_RATE_LIMIT_EXCEEDED',
      ip_address: getClientIp(req),
      url: req.originalUrl,
      user_agent: req.headers['user-agent'],
      severity: SystemLog.SEVERITY_LEVELS.WARN,
    });
  },

  /**
   * Log unauthorized access attempt
   */
  logUnauthorizedAccess: async (req, reason = 'Unauthorized') => {
    const userAgentInfo = parseUserAgent(req.headers['user-agent']);

    await SystemLog.logSecurity({
      action: 'SECURITY_UNAUTHORIZED_ACCESS',
      actor_user_id: req.user?.id || null,
      ip_address: getClientIp(req),
      url: req.originalUrl,
      user_agent: req.headers['user-agent'],
      ...userAgentInfo,
      detail: { reason },
      severity: SystemLog.SEVERITY_LEVELS.WARN,
    });
  },

  /**
   * Log forbidden access (insufficient permissions)
   */
  logForbiddenAccess: async (req, requiredRole) => {
    await SystemLog.logSecurity({
      action: 'SECURITY_FORBIDDEN_ACCESS',
      actor_user_id: req.user?.id || null,
      ip_address: getClientIp(req),
      url: req.originalUrl,
      detail: {
        userRole: req.user?.role,
        requiredRole,
      },
      severity: SystemLog.SEVERITY_LEVELS.WARN,
    });
  },

  /**
   * Log suspicious activity
   */
  logSuspiciousActivity: async (req, description) => {
    const userAgentInfo = parseUserAgent(req.headers['user-agent']);

    await SystemLog.logSecurity({
      action: 'SECURITY_SUSPICIOUS_ACTIVITY',
      actor_user_id: req.user?.id || null,
      ip_address: getClientIp(req),
      url: req.originalUrl,
      user_agent: req.headers['user-agent'],
      ...userAgentInfo,
      detail: { description },
      severity: SystemLog.SEVERITY_LEVELS.CRITICAL,
    });
  },

  /**
   * Log invalid token
   */
  logInvalidToken: async (req, reason = 'Invalid token') => {
    await SystemLog.logSecurity({
      action: 'SECURITY_INVALID_TOKEN',
      ip_address: getClientIp(req),
      url: req.originalUrl,
      user_agent: req.headers['user-agent'],
      detail: { reason },
      severity: SystemLog.SEVERITY_LEVELS.WARN,
    });
  },

  /**
   * Log brute force detection
   */
  logBruteForceDetected: async (req, attempts) => {
    await SystemLog.logSecurity({
      action: 'SECURITY_BRUTE_FORCE_DETECTED',
      ip_address: getClientIp(req),
      user_agent: req.headers['user-agent'],
      detail: { attempts },
      severity: SystemLog.SEVERITY_LEVELS.CRITICAL,
    });
  },
};

/**
 * Error Logger - Log application errors
 */
const errorLogger = {
  /**
   * Log application error
   */
  logError: async (req, error, additionalInfo = {}) => {
    await SystemLog.logError({
      action: 'ERROR_APPLICATION',
      actor_user_id: req?.user?.id || null,
      ip_address: req ? getClientIp(req) : null,
      url: req?.originalUrl || null,
      http_method: req?.method || null,
      error_message: error.message,
      error_stack: error.stack,
      error_code: error.code || error.name,
      detail: additionalInfo,
      severity: SystemLog.SEVERITY_LEVELS.ERROR,
    });
  },

  /**
   * Log database error
   */
  logDatabaseError: async (error, operation = 'unknown') => {
    await SystemLog.logError({
      action: 'ERROR_DATABASE',
      error_message: error.message,
      error_stack: error.stack,
      error_code: error.code || 'DB_ERROR',
      detail: { operation },
      severity: SystemLog.SEVERITY_LEVELS.ERROR,
    });
  },

  /**
   * Log validation error
   */
  logValidationError: async (req, errors) => {
    await SystemLog.logError({
      action: 'ERROR_VALIDATION',
      actor_user_id: req.user?.id || null,
      ip_address: getClientIp(req),
      url: req.originalUrl,
      http_method: req.method,
      detail: { errors },
      severity: SystemLog.SEVERITY_LEVELS.WARN,
    });
  },

  /**
   * Log critical error
   */
  logCriticalError: async (error, context = '') => {
    await SystemLog.logError({
      action: 'ERROR_CRITICAL',
      error_message: error.message,
      error_stack: error.stack,
      error_code: error.code || 'CRITICAL',
      detail: { context },
      severity: SystemLog.SEVERITY_LEVELS.CRITICAL,
    });
  },
};

module.exports = {
  requestLogger,
  authLogger,
  securityLogger,
  errorLogger,
  sanitizeObject,
  getClientIp,
  parseUserAgent,
};