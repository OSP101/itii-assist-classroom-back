const express = require('express');
const cors = require('cors');
const helmet = require('helmet');
const morgan = require('morgan');
const cookieParser = require('cookie-parser');
const rateLimit = require('express-rate-limit');

const config = require('./config');
const { testConnection } = require('./config/database');
const passport = require('./config/passport');
const routes = require('./routes');
const { metricsMiddleware } = require('./middlewares/metrics');
const { 
  notFoundHandler, 
  errorConverter, 
  errorHandler 
} = require('./middlewares');
const { logger } = require('./utils');

// Initialize express app
const app = express();

// Trust proxy (for rate limiting behind reverse proxy)
app.set('trust proxy', 1);

// Security middleware - configure helmet to allow cross-origin images
app.use(helmet({
  crossOriginResourcePolicy: { policy: "cross-origin" },
  crossOriginEmbedderPolicy: false,
}));

// CORS configuration
const allowedOrigins = [
  config.frontendUrl,
  'https://itii.osp101.dev',
  'http://localhost:3000',
  'http://10.199.10.10:3000',
];

// In development, allow any local network IP
const corsOptions = {
  origin: function (origin, callback) {
    // Allow requests with no origin (like mobile apps or curl)
    if (!origin) return callback(null, true);
    
    // Check if origin is in allowed list
    if (allowedOrigins.includes(origin)) {
      return callback(null, true);
    }
    
    // In development, allow local network IPs (192.168.x.x, 10.x.x.x, etc.)
    if (config.nodeEnv === 'development') {
      const localNetworkPattern = /^http:\/\/(192\.168\.\d+\.\d+|10\.\d+\.\d+\.\d+|172\.(1[6-9]|2\d|3[01])\.\d+\.\d+|localhost|127\.0\.0\.1)(:\d+)?$/;
      if (localNetworkPattern.test(origin)) {
        return callback(null, true);
      }
    }
    
    callback(new Error('Not allowed by CORS'));
  },
  credentials: true,
  methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'],
  allowedHeaders: ['Content-Type', 'Authorization'],
};

app.use(cors(corsOptions));

// Rate limiting - ปรับให้เหมาะกับการใช้งานจริง
const limiter = rateLimit({
  windowMs: 1 * 60 * 1000, // 1 minute window
  max: 120, // 120 requests per minute per IP (เพียงพอสำหรับ SPA ที่เรียก API หลายตัวพร้อมกัน)
  standardHeaders: true, // Return rate limit info in the `RateLimit-*` headers
  legacyHeaders: false, // Disable the `X-RateLimit-*` headers
  // Skip rate limiting for certain conditions
  skip: (req) => {
    // Skip rate limiting in development
    if (config.nodeEnv === 'development') return true;
    return false;
  },
  message: {
    success: false,
    error: {
      code: 429,
      message: 'Too many requests, please try again later.',
    },
  },
  // Use IP + User ID for authenticated users (better distribution)
  keyGenerator: (req) => {
    // If user is authenticated, use user ID + IP to allow more requests per user
    if (req.user?.id) {
      return `${req.ip}-user-${req.user.id}`;
    }
    return req.ip;
  },
});
app.use('/api', limiter);

// Auth routes rate limiting (stricter to prevent brute force)
const authLimiter = rateLimit({
  windowMs: 15 * 60 * 1000, // 15 minutes
  max: 30, // 30 login attempts per 15 minutes
  standardHeaders: true,
  legacyHeaders: false,
  message: {
    success: false,
    error: {
      code: 429,
      message: 'Too many login attempts, please try again later.',
    },
  },
});
app.use('/api/auth/login', authLimiter);

// Body parsing middleware
app.use(express.json({ limit: '10mb' }));
app.use(express.urlencoded({ extended: true, limit: '10mb' }));

// Cookie parser
app.use(cookieParser(config.cookieSecret));

// Logging
if (config.nodeEnv === 'development') {
  app.use(morgan('dev'));
} else {
  app.use(morgan('combined', {
    stream: { write: (message) => logger.info(message.trim()) },
  }));
}

// Performance middleware
const { 
  requestTimeout, 
  slowQueryLogger, 
  requestId 
} = require('./middlewares/performance.middleware');

// Add request ID for tracing
app.use(requestId());

// Add request timeout (30 seconds)
app.use(requestTimeout(30000));

// Log slow requests (> 2 seconds)
app.use(slowQueryLogger(2000));

// Initialize Passport
app.use(passport.initialize());

// Serve uploaded files (static) with CORS support
const path = require('path');
app.use('/uploads', (req, res, next) => {
  res.setHeader('Cross-Origin-Resource-Policy', 'cross-origin');
  res.setHeader('Access-Control-Allow-Origin', '*');
  next();
}, express.static(path.join(__dirname, '../uploads')));

// Prometheus metrics middleware (must be before routes to capture all requests)
app.use(metricsMiddleware);

// Request Logger Middleware (บันทึกเฉพาะการกระทำต่อระบบ ไม่บันทึกการเข้าหน้า)
const { requestLogger } = require('./middlewares');
app.use('/api', requestLogger({
  logBody: config.nodeEnv === 'development', // Log body only in development
  excludePaths: ['/api/health', '/api/system/metrics', '/api/system/cpu'],
  logAllRequests: false, // บันทึกเฉพาะ POST, PUT, DELETE, PATCH (การกระทำต่อระบบ)
}));

// API routes
app.use('/api', routes);

// Root endpoint with system info
app.get('/', (req, res) => {
  const uptime = process.uptime();
  const days = Math.floor(uptime / 86400);
  const hours = Math.floor((uptime % 86400) / 3600);
  const minutes = Math.floor((uptime % 3600) / 60);
  const seconds = Math.floor(uptime % 60);
  
  res.json({
    success: true,
    system: {
      name: 'Course & Lab Management System',
      description: 'ระบบจัดการรายวิชา, เช็คชื่อ, เก็บคะแนน และจองคิวตรวจงาน',
      version: '1.0.0',
    },
    developer: {
      name: 'OSP101',
      project: 'Senior Project (โปรเจคจบ)',
      university: 'Khon Kaen University',
    },
    technology: {
      runtime: `Node.js ${process.version}`,
      framework: 'Express.js',
      database: 'MySQL with Sequelize ORM',
      authentication: 'Passport.js + JWT',
      realtime: 'Socket.io (coming soon)',
    },
    server: {
      environment: config.nodeEnv,
      port: config.port,
      platform: process.platform,
      arch: process.arch,
      pid: process.pid,
      memoryUsage: `${Math.round(process.memoryUsage().heapUsed / 1024 / 1024)} MB`,
      uptime: `${days}d ${hours}h ${minutes}m ${seconds}s`,
      startedAt: new Date(Date.now() - uptime * 1000).toISOString(),
    },
    endpoints: {
      health: '/api/health',
      auth: '/api/auth',
      docs: 'Coming soon...',
    },
    timestamp: new Date().toISOString(),
  });
});

// Error handling
app.use(notFoundHandler);
app.use(errorConverter);
app.use(errorHandler);

// Import http and socket.io
const http = require('http');
const { initializeSocket } = require('./config/socket');

// Import Redis and Queue Assignment Worker
const { initializeRedis, closeRedis } = require('./config/redis');
const { startAssignmentWorker, stopAssignmentWorker } = require('./utils/queueAssignmentWorker');

// Create HTTP server
const server = http.createServer(app);

// Initialize Socket.io
const io = initializeSocket(server);

// Make io accessible in routes via req.app.get('io')
app.set('io', io);

// Start server
const startServer = async () => {
  try {
    // Test database connection
    await testConnection();
    
    // Initialize Redis for queue management
    try {
      initializeRedis();
      logger.info('🔴 Redis initialized for queue management');
      
      // Start background assignment worker
      startAssignmentWorker();
      logger.info('⚙️ Queue assignment worker started');
    } catch (redisError) {
      logger.warn('⚠️ Redis not available, queue system will use MySQL fallback:', redisError.message);
    }
    
    // Start listening (use server instead of app for socket.io)
    server.listen(config.port, () => {
      logger.info(`🚀 Server running in ${config.nodeEnv} mode on port ${config.port}`);
      logger.info(`📍 API URL: http://localhost:${config.port}/api`);
      logger.info(`🔌 Socket.io enabled`);
    });
    
    // Start session cleanup scheduler (runs every hour)
    const { RefreshToken } = require('./models');
    setInterval(async () => {
      try {
        const result = await RefreshToken.cleanupStaleSessions();
        if (result.expiredCount > 0 || result.revokedCount > 0) {
          logger.info(`🧹 Session cleanup: ${result.expiredCount} expired, ${result.revokedCount} revoked sessions removed`);
        }
      } catch (error) {
        logger.error('Session cleanup error:', error);
      }
    }, 60 * 60 * 1000); // Every hour
    logger.info('🧹 Session cleanup scheduler started');
    
  } catch (error) {
    logger.error('Failed to start server:', error);
    process.exit(1);
  }
};

// Handle unhandled promise rejections
process.on('unhandledRejection', (reason, promise) => {
  logger.error('Unhandled Rejection at:', promise, 'reason:', reason);
});

// Handle uncaught exceptions
process.on('uncaughtException', (error) => {
  logger.error('Uncaught Exception:', error);
  process.exit(1);
});

// Graceful shutdown
process.on('SIGTERM', async () => {
  logger.info('SIGTERM received, shutting down gracefully...');
  stopAssignmentWorker();
  await closeRedis();
  server.close(() => {
    logger.info('Server closed');
    process.exit(0);
  });
});

process.on('SIGINT', async () => {
  logger.info('SIGINT received, shutting down gracefully...');
  stopAssignmentWorker();
  await closeRedis();
  server.close(() => {
    logger.info('Server closed');
    process.exit(0);
  });
});

// Start the server
startServer();

module.exports = { app, server, io };
