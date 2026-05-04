/**
 * Redis Client Configuration
 * Used for real-time queue state management (worker states, waiting queues)
 * 
 * Redis replaces MySQL for frequently-changing states to reduce database load
 * and lock contention under high concurrency.
 */

const Redis = require('ioredis');
const config = require('./index');
const logger = require('../utils/logger');

let redisClient = null;
let subscriberClient = null;

/**
 * Initialize Redis client
 */
const initializeRedis = () => {
  const redisConfig = {
    host: config.redis.host,
    port: config.redis.port,
    password: config.redis.password || undefined,
    db: config.redis.db,
    retryStrategy: (times) => {
      const delay = Math.min(times * 50, 2000);
      return delay;
    },
    maxRetriesPerRequest: 3,
    lazyConnect: true,
  };

  redisClient = new Redis(redisConfig);
  
  redisClient.on('connect', () => {
    logger.info('🔴 Redis connected');
  });

  redisClient.on('error', (err) => {
    logger.error('Redis connection error:', err.message);
  });

  redisClient.on('close', () => {
    logger.info('🔴 Redis connection closed');
  });

  // Connect
  redisClient.connect().catch((err) => {
    logger.error('Failed to connect to Redis:', err.message);
  });

  return redisClient;
};

/**
 * Get Redis client instance
 */
const getRedis = () => {
  if (!redisClient) {
    return initializeRedis();
  }
  return redisClient;
};

/**
 * Create a subscriber client for pub/sub (separate connection needed)
 */
const getSubscriber = () => {
  if (!subscriberClient) {
    subscriberClient = getRedis().duplicate();
  }
  return subscriberClient;
};

/**
 * Graceful shutdown
 */
const closeRedis = async () => {
  if (redisClient) {
    await redisClient.quit();
    redisClient = null;
  }
  if (subscriberClient) {
    await subscriberClient.quit();
    subscriberClient = null;
  }
};

module.exports = {
  initializeRedis,
  getRedis,
  getSubscriber,
  closeRedis,
};
