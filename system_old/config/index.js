const path = require('path');

// Only load .env file if DB_NAME is not already set (from docker env_file)
if (!process.env.DB_NAME) {
  const envFile = process.env.NODE_ENV === 'production' ? '.env.prod' : '.env.dev';
  require('dotenv').config({ path: path.resolve(__dirname, '../../', envFile) });
  
  // Fallback to .env if specific file doesn't exist
  if (!process.env.DB_NAME) {
    require('dotenv').config({ path: path.resolve(__dirname, '../../.env') });
  }
}

// Debug: Log which database is being used
console.log(`📦 Database config: ${process.env.DB_NAME} @ ${process.env.DB_HOST || 'localhost'}`);

module.exports = {
  // Server
  nodeEnv: process.env.NODE_ENV || 'development',
  port: parseInt(process.env.PORT, 10) || 3001,

  // Database
  db: {
    host: process.env.DB_HOST || 'localhost',
    port: parseInt(process.env.DB_PORT, 10) || 3306,
    name: process.env.DB_NAME || 'project_ta_dev',
    user: process.env.DB_USER || 'root',
    password: process.env.DB_PASSWORD || '',
  },

  // Redis - for real-time queue states
  redis: {
    host: process.env.REDIS_HOST || 'localhost',
    port: parseInt(process.env.REDIS_PORT, 10) || 6379,
    password: process.env.REDIS_PASSWORD || undefined,
    db: parseInt(process.env.REDIS_DB, 10) || 0,
  },

  // JWT
  jwt: {
    accessSecret: process.env.JWT_ACCESS_SECRET || 'default-access-secret',
    refreshSecret: process.env.JWT_REFRESH_SECRET || 'default-refresh-secret',
    accessExpiresIn: process.env.JWT_ACCESS_EXPIRES_IN || '15m',
    refreshExpiresIn: process.env.JWT_REFRESH_EXPIRES_IN || '1d',
  },

  // Google OAuth
  google: {
    clientId: process.env.GOOGLE_CLIENT_ID,
    clientSecret: process.env.GOOGLE_CLIENT_SECRET,
    callbackUrl: process.env.GOOGLE_CALLBACK_URL || 'http://localhost:3001/api/auth/google/callback',
  },

  // GitHub OAuth
  github: {
    clientId: process.env.GITHUB_CLIENT_ID,
    clientSecret: process.env.GITHUB_CLIENT_SECRET,
    callbackUrl: process.env.GITHUB_CALLBACK_URL || 'http://localhost:3001/api/auth/github/callback',
  },

  // Apple OAuth
  apple: {
    clientId: process.env.APPLE_CLIENT_ID, // Service ID (e.g., com.example.app.signin)
    teamId: process.env.APPLE_TEAM_ID,
    keyId: process.env.APPLE_KEY_ID,
    privateKeyPath: process.env.APPLE_PRIVATE_KEY_PATH, // Path to .p8 file
    privateKey: process.env.APPLE_PRIVATE_KEY, // Or direct key content
    callbackUrl: process.env.APPLE_CALLBACK_URL || 'http://localhost:3001/api/auth/apple/callback',
  },

  // Frontend
  frontendUrl: process.env.FRONTEND_URL || 'http://localhost:3000',

  // Cookie
  cookieSecret: process.env.COOKIE_SECRET || 'default-cookie-secret',

  // Email (for 2FA)
  email: {
    host: process.env.SMTP_HOST || 'smtp.gmail.com',
    port: parseInt(process.env.SMTP_PORT, 10) || 587,
    secure: process.env.SMTP_SECURE === 'true',
    user: process.env.SMTP_USER || '',
    pass: process.env.SMTP_PASS || '',
    from: process.env.SMTP_FROM || 'noreply@itii-assist.com',
  },

  // Two-Factor Authentication
  twoFactor: {
    appName: process.env.TWO_FACTOR_APP_NAME || 'ITII Assist Classroom',
    issuer: process.env.TWO_FACTOR_ISSUER || 'ITII Assist Classroom',
  },

  // Monitoring / Prometheus
  monitoring: {
    prometheusUrl: process.env.PROMETHEUS_URL || 'http://itii-prometheus:9090',
    alertWebhookSecret: process.env.ALERT_WEBHOOK_SECRET || '',
  },
};
