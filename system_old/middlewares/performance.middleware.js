/**
 * Performance Middleware
 * Request timeout, rate limiting, and response compression
 */

const logger = require('../utils/logger');

/**
 * Request timeout middleware
 * Prevents long-running requests from blocking the server
 */
const requestTimeout = (timeoutMs = 30000) => {
    return (req, res, next) => {
        // Set timeout
        req.setTimeout(timeoutMs, () => {
            if (!res.headersSent) {
                res.status(503).json({
                    success: false,
                    error: {
                        message: 'Request timeout - การร้องขอใช้เวลานานเกินไป กรุณาลองใหม่อีกครั้ง',
                        code: 'REQUEST_TIMEOUT',
                    },
                });
            }
        });
        
        next();
    };
};

/**
 * Simple in-memory rate limiter
 * For production, consider using Redis-based rate limiting
 */
class RateLimiter {
    constructor() {
        this.requests = new Map();
        
        // Cleanup old entries every minute
        this.cleanupInterval = setInterval(() => {
            this.cleanup();
        }, 60 * 1000);
    }

    /**
     * Check if request is allowed
     * @param {string} key - Identifier (IP or user ID)
     * @param {number} maxRequests - Max requests allowed
     * @param {number} windowMs - Time window in milliseconds
     * @returns {object} - { allowed: boolean, remaining: number, resetTime: number }
     */
    check(key, maxRequests = 100, windowMs = 60000) {
        const now = Date.now();
        const windowStart = now - windowMs;
        
        let record = this.requests.get(key);
        
        if (!record) {
            record = { count: 0, timestamps: [], resetTime: now + windowMs };
            this.requests.set(key, record);
        }
        
        // Remove old timestamps
        record.timestamps = record.timestamps.filter(t => t > windowStart);
        
        // Check if allowed
        if (record.timestamps.length >= maxRequests) {
            return {
                allowed: false,
                remaining: 0,
                resetTime: record.timestamps[0] + windowMs,
            };
        }
        
        // Add new timestamp
        record.timestamps.push(now);
        record.count = record.timestamps.length;
        
        return {
            allowed: true,
            remaining: maxRequests - record.timestamps.length,
            resetTime: record.timestamps[0] + windowMs,
        };
    }

    cleanup() {
        const now = Date.now();
        for (const [key, record] of this.requests.entries()) {
            // Remove entries older than 5 minutes
            if (record.timestamps.length === 0 || 
                record.timestamps[record.timestamps.length - 1] < now - 5 * 60 * 1000) {
                this.requests.delete(key);
            }
        }
    }

    shutdown() {
        if (this.cleanupInterval) {
            clearInterval(this.cleanupInterval);
        }
        this.requests.clear();
    }
}

const rateLimiter = new RateLimiter();

/**
 * Rate limiting middleware
 */
const rateLimit = (options = {}) => {
    const {
        maxRequests = 100,
        windowMs = 60000,
        keyGenerator = (req) => req.ip,
        message = 'คุณส่งคำขอมากเกินไป กรุณารอสักครู่แล้วลองใหม่',
    } = options;

    return (req, res, next) => {
        const key = keyGenerator(req);
        const result = rateLimiter.check(key, maxRequests, windowMs);
        
        // Set rate limit headers
        res.setHeader('X-RateLimit-Limit', maxRequests);
        res.setHeader('X-RateLimit-Remaining', result.remaining);
        res.setHeader('X-RateLimit-Reset', Math.ceil(result.resetTime / 1000));
        
        if (!result.allowed) {
            return res.status(429).json({
                success: false,
                error: {
                    message,
                    code: 'RATE_LIMIT_EXCEEDED',
                    retryAfter: Math.ceil((result.resetTime - Date.now()) / 1000),
                },
            });
        }
        
        next();
    };
};

/**
 * Slow query logging middleware
 */
const slowQueryLogger = (thresholdMs = 1000) => {
    return (req, res, next) => {
        const startTime = Date.now();
        
        // Override res.json to log slow responses
        const originalJson = res.json.bind(res);
        res.json = (data) => {
            const duration = Date.now() - startTime;
            
            if (duration > thresholdMs) {
                logger.warn(`[SLOW REQUEST] ${req.method} ${req.originalUrl} took ${duration}ms`);
            }
            
            return originalJson(data);
        };
        
        next();
    };
};

/**
 * Request ID middleware for tracing
 */
const requestId = () => {
    return (req, res, next) => {
        const id = req.headers['x-request-id'] || 
                   `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
        req.requestId = id;
        res.setHeader('X-Request-Id', id);
        next();
    };
};

/**
 * Memory usage checker
 * Warns when memory usage is high
 */
const memoryCheck = (thresholdMB = 512) => {
    return (req, res, next) => {
        const memUsage = process.memoryUsage();
        const heapUsedMB = memUsage.heapUsed / 1024 / 1024;
        
        if (heapUsedMB > thresholdMB) {
            logger.warn(`[MEMORY WARNING] Heap usage: ${heapUsedMB.toFixed(2)}MB (threshold: ${thresholdMB}MB)`);
        }
        
        next();
    };
};

module.exports = {
    requestTimeout,
    rateLimit,
    rateLimiter,
    slowQueryLogger,
    requestId,
    memoryCheck,
};
