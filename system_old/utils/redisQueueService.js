/**
 * Redis Queue Service
 * 
 * Handles ALL real-time queue states in Redis instead of MySQL.
 * This reduces database lock contention and request timeouts under high concurrency.
 * 
 * Redis Key Schema:
 * - queue:{sessionId}:workers                    - HASH of worker states { odUserId: JSON state }
 * - queue:{sessionId}:available:{type}           - SET of available worker userIds (grading/help)
 * - queue:{sessionId}:waiting:grading            - LIST of waiting booking IDs (FIFO)
 * - queue:{sessionId}:waiting:help               - LIST of waiting booking IDs (FIFO)
 * - queue:{sessionId}:booking:{bookingId}        - HASH of booking metadata
 * - queue:{sessionId}:desk:{deskId}              - HASH of desk status
 * - queue:{sessionId}:assignment:lock            - Lock for assignment process
 * - queue:assignment:pending                     - LIST of {sessionId, bookingId} to assign
 * 
 * MySQL is used ONLY for:
 * - Initial loading of session data
 * - Persisting completed/cancelled/skipped bookings
 * - Persisting scores
 * - Historical reports
 */

const { getRedis } = require('../config/redis');
const logger = require('./logger');

// Key TTL in seconds (1 hour for session data, longer for booking history)
const SESSION_TTL = 3600;
const BOOKING_TTL = 86400; // 24 hours

// ============================================
// Key Generators
// ============================================

const keys = {
  workers: (sessionId) => `queue:${sessionId}:workers`,
  availableWorkers: (sessionId, type) => `queue:${sessionId}:available:${type}`,
  waitingQueue: (sessionId, type) => `queue:${sessionId}:waiting:${type}`,
  booking: (sessionId, bookingId) => `queue:${sessionId}:booking:${bookingId}`,
  deskStatus: (sessionId, deskId) => `queue:${sessionId}:desk:${deskId}`,
  assignmentLock: (sessionId) => `queue:${sessionId}:assignment:lock`,
  pendingAssignments: () => 'queue:assignment:pending',
  sessionActive: (sessionId) => `queue:${sessionId}:active`,
};

// ============================================
// Worker State Management
// ============================================

/**
 * Worker state structure in Redis:
 * {
 *   userId: number,
 *   status: 'online' | 'busy' | 'paused' | 'offline',
 *   acceptGrading: boolean,
 *   acceptHelp: boolean,
 *   currentBookingId: number | null,
 *   joinedAt: timestamp,
 *   lastActiveAt: timestamp,
 *   completedGrading: number,
 *   completedHelp: number,
 * }
 */

/**
 * Add or update worker in session
 * Called when worker joins or updates preferences
 */
const setWorkerState = async (sessionId, userId, state) => {
  const redis = getRedis();
  const key = keys.workers(sessionId);
  
  const workerData = {
    userId: userId,
    status: state.status || 'online',
    acceptGrading: state.acceptGrading !== false,
    acceptHelp: state.acceptHelp !== false,
    currentBookingId: state.currentBookingId || null,
    joinedAt: state.joinedAt || Date.now(),
    lastActiveAt: Date.now(),
    completedGrading: state.completedGrading || 0,
    completedHelp: state.completedHelp || 0,
  };

  await redis.hset(key, userId.toString(), JSON.stringify(workerData));
  await redis.expire(key, SESSION_TTL);

  // Update availability sets
  await updateWorkerAvailability(sessionId, userId, workerData);

  logger.debug(`[Redis] Worker ${userId} state set in session ${sessionId}:`, workerData.status);
  return workerData;
};

/**
 * Get worker state from Redis
 */
const getWorkerState = async (sessionId, userId) => {
  const redis = getRedis();
  const data = await redis.hget(keys.workers(sessionId), userId.toString());
  return data ? JSON.parse(data) : null;
};

/**
 * Get all workers in session
 */
const getAllWorkers = async (sessionId) => {
  const redis = getRedis();
  const workersHash = await redis.hgetall(keys.workers(sessionId));
  
  const workers = [];
  for (const [userId, data] of Object.entries(workersHash)) {
    workers.push(JSON.parse(data));
  }
  
  return workers;
};

/**
 * Update worker availability in Redis SETs
 * Workers in 'online' status are added to available sets based on acceptGrading/acceptHelp
 */
const updateWorkerAvailability = async (sessionId, userId, workerData) => {
  const redis = getRedis();
  const userIdStr = userId.toString();

  // Remove from all availability sets first
  await redis.srem(keys.availableWorkers(sessionId, 'grading'), userIdStr);
  await redis.srem(keys.availableWorkers(sessionId, 'help'), userIdStr);

  // Only add to available sets if status is 'online' (not busy/paused/offline)
  if (workerData.status === 'online') {
    if (workerData.acceptGrading) {
      await redis.sadd(keys.availableWorkers(sessionId, 'grading'), userIdStr);
      await redis.expire(keys.availableWorkers(sessionId, 'grading'), SESSION_TTL);
    }
    if (workerData.acceptHelp) {
      await redis.sadd(keys.availableWorkers(sessionId, 'help'), userIdStr);
      await redis.expire(keys.availableWorkers(sessionId, 'help'), SESSION_TTL);
    }
  }
};

/**
 * Set worker to busy (assigned a booking)
 */
const setWorkerBusy = async (sessionId, userId, bookingId) => {
  const worker = await getWorkerState(sessionId, userId);
  if (!worker) return null;

  worker.status = 'busy';
  worker.currentBookingId = bookingId;
  worker.lastActiveAt = Date.now();

  return setWorkerState(sessionId, userId, worker);
};

/**
 * Set worker to paused (finishing current task, won't accept new)
 */
const setWorkerPaused = async (sessionId, userId) => {
  const worker = await getWorkerState(sessionId, userId);
  if (!worker) return null;

  worker.status = 'paused';
  worker.lastActiveAt = Date.now();

  return setWorkerState(sessionId, userId, worker);
};

/**
 * Set worker to online (ready for new tasks)
 */
const setWorkerOnline = async (sessionId, userId) => {
  const worker = await getWorkerState(sessionId, userId);
  if (!worker) return null;

  worker.status = 'online';
  worker.currentBookingId = null;
  worker.lastActiveAt = Date.now();

  return setWorkerState(sessionId, userId, worker);
};

/**
 * Set worker to offline
 */
const setWorkerOffline = async (sessionId, userId) => {
  const worker = await getWorkerState(sessionId, userId);
  if (!worker) return null;

  worker.status = 'offline';
  worker.currentBookingId = null;
  worker.lastActiveAt = Date.now();

  return setWorkerState(sessionId, userId, worker);
};

/**
 * Increment worker completion count
 */
const incrementWorkerCompletion = async (sessionId, userId, bookingType) => {
  const worker = await getWorkerState(sessionId, userId);
  if (!worker) return null;

  if (bookingType === 'grading') {
    worker.completedGrading = (worker.completedGrading || 0) + 1;
  } else {
    worker.completedHelp = (worker.completedHelp || 0) + 1;
  }
  worker.lastActiveAt = Date.now();

  return setWorkerState(sessionId, userId, worker);
};

/**
 * Pop an available worker for a booking type (grading/help)
 * Returns the worker with the least completed tasks (load balancing)
 */
const popAvailableWorker = async (sessionId, bookingType) => {
  const redis = getRedis();
  const availableKey = keys.availableWorkers(sessionId, bookingType);
  
  // Get all available workers
  const availableUserIds = await redis.smembers(availableKey);
  
  if (availableUserIds.length === 0) {
    return null;
  }

  // Get worker states to find one with least completions
  let bestWorker = null;
  let minCompletions = Infinity;

  for (const userIdStr of availableUserIds) {
    const worker = await getWorkerState(sessionId, parseInt(userIdStr));
    if (worker && worker.status === 'online') {
      const completions = bookingType === 'grading' 
        ? worker.completedGrading 
        : worker.completedHelp;
      
      if (completions < minCompletions) {
        minCompletions = completions;
        bestWorker = worker;
      }
    }
  }

  return bestWorker;
};

// ============================================
// Booking Queue Management
// ============================================

/**
 * Booking metadata structure in Redis:
 * {
 *   id: number,
 *   sessionId: number,
 *   studentId: number,
 *   deskId: number,
 *   deskNumber: string,
 *   bookingType: 'grading' | 'help',
 *   queueNumber: number,
 *   status: 'waiting' | 'in_progress',
 *   note: string,
 *   createdAt: timestamp,
 *   assignedWorkerId: number | null,
 *   assignedAt: timestamp | null,
 *   studentInfo: { id, studentId, fullName },
 * }
 */

/**
 * Add booking to waiting queue
 * Called after booking is created in MySQL
 */
const addBookingToQueue = async (sessionId, booking) => {
  const redis = getRedis();
  const bookingId = booking.id;
  
  const bookingData = {
    id: bookingId.toString(),
    sessionId: sessionId.toString(),
    studentId: (booking.student_id || booking.studentId).toString(),
    deskId: (booking.desk_id || booking.deskId).toString(),
    deskNumber: booking.desk_number || booking.deskNumber || '',
    bookingType: booking.booking_type || booking.bookingType,
    queueNumber: (booking.queue_number || booking.queueNumber).toString(),
    status: 'waiting',
    note: booking.note || '',
    createdAt: Date.now().toString(),
    assignedWorkerId: '',
    assignedAt: '',
    studentInfo: booking.studentInfo ? JSON.stringify(booking.studentInfo) : '',
  };

  // Store booking metadata - flatten object for HSET
  const bookingKey = keys.booking(sessionId, bookingId);
  const flatArgs = [];
  for (const [key, value] of Object.entries(bookingData)) {
    flatArgs.push(key, value);
  }
  await redis.hset(bookingKey, ...flatArgs);
  await redis.expire(bookingKey, BOOKING_TTL);

  // Add to waiting queue (FIFO)
  const queueKey = keys.waitingQueue(sessionId, bookingData.bookingType);
  await redis.rpush(queueKey, bookingId.toString());
  await redis.expire(queueKey, SESSION_TTL);

  // Add to pending assignments for background worker
  await redis.rpush(keys.pendingAssignments(), JSON.stringify({
    sessionId,
    bookingId,
    bookingType: bookingData.bookingType,
    addedAt: Date.now(),
  }));

  logger.debug(`[Redis] Booking ${bookingId} added to queue ${sessionId}:${bookingData.bookingType}`);
  return bookingData;
};

/**
 * Get booking metadata from Redis
 */
const getBooking = async (sessionId, bookingId) => {
  const redis = getRedis();
  const data = await redis.hgetall(keys.booking(sessionId, bookingId));
  
  if (!data || Object.keys(data).length === 0) {
    return null;
  }
  
  // Parse numeric fields (handle empty strings)
  return {
    ...data,
    id: parseInt(data.id),
    sessionId: parseInt(data.sessionId),
    studentId: parseInt(data.studentId),
    deskId: parseInt(data.deskId),
    queueNumber: parseInt(data.queueNumber),
    createdAt: data.createdAt ? parseInt(data.createdAt) : null,
    assignedWorkerId: data.assignedWorkerId && data.assignedWorkerId !== '' ? parseInt(data.assignedWorkerId) : null,
    assignedAt: data.assignedAt && data.assignedAt !== '' ? parseInt(data.assignedAt) : null,
    studentInfo: data.studentInfo && data.studentInfo !== '' ? JSON.parse(data.studentInfo) : null,
  };
};

/**
 * Pop next waiting booking from queue
 */
const popNextBooking = async (sessionId, bookingType) => {
  const redis = getRedis();
  const queueKey = keys.waitingQueue(sessionId, bookingType);
  
  const bookingIdStr = await redis.lpop(queueKey);
  if (!bookingIdStr) {
    return null;
  }

  const booking = await getBooking(sessionId, parseInt(bookingIdStr));
  return booking;
};

/**
 * Peek at next waiting booking without removing
 */
const peekNextBooking = async (sessionId, bookingType) => {
  const redis = getRedis();
  const queueKey = keys.waitingQueue(sessionId, bookingType);
  
  const bookingIdStr = await redis.lindex(queueKey, 0);
  if (!bookingIdStr) {
    return null;
  }

  return getBooking(sessionId, parseInt(bookingIdStr));
};

/**
 * Get waiting queue length
 */
const getQueueLength = async (sessionId, bookingType) => {
  const redis = getRedis();
  return redis.llen(keys.waitingQueue(sessionId, bookingType));
};

/**
 * Get position of a booking in queue
 */
const getBookingPosition = async (sessionId, bookingId, bookingType) => {
  const redis = getRedis();
  const queueKey = keys.waitingQueue(sessionId, bookingType);
  
  const queue = await redis.lrange(queueKey, 0, -1);
  const position = queue.indexOf(bookingId.toString());
  
  return position === -1 ? null : position + 1;
};

/**
 * Remove booking from waiting queue (for cancellation)
 */
const removeBookingFromQueue = async (sessionId, bookingId, bookingType) => {
  const redis = getRedis();
  const queueKey = keys.waitingQueue(sessionId, bookingType);
  
  await redis.lrem(queueKey, 1, bookingId.toString());
  
  // Remove booking metadata
  await redis.del(keys.booking(sessionId, bookingId));
  
  logger.debug(`[Redis] Booking ${bookingId} removed from queue`);
};

/**
 * Update booking status (for assignment)
 */
const updateBookingStatus = async (sessionId, bookingId, updates) => {
  const redis = getRedis();
  const bookingKey = keys.booking(sessionId, bookingId);
  
  const flatArgs = [];
  for (const [key, value] of Object.entries(updates)) {
    const strValue = value === null || value === undefined ? '' : 
                     typeof value === 'object' ? JSON.stringify(value) : 
                     String(value);
    flatArgs.push(key, strValue);
  }
  
  if (flatArgs.length > 0) {
    await redis.hset(bookingKey, ...flatArgs);
    await redis.expire(bookingKey, BOOKING_TTL);
  }
};

// ============================================
// Desk Status Management
// ============================================

/**
 * Desk status structure in Redis:
 * {
 *   gradingStatus: 'not_started' | 'waiting' | 'in_progress' | 'completed',
 *   helpStatus: 'none' | 'waiting' | 'in_progress',
 *   gradingBookingId: number | null,
 *   helpBookingId: number | null,
 * }
 */

const setDeskStatus = async (sessionId, deskId, status) => {
  const redis = getRedis();
  const key = keys.deskStatus(sessionId, deskId);
  
  const flatArgs = [];
  for (const [k, v] of Object.entries(status)) {
    const strValue = v === null || v === undefined ? '' : String(v);
    flatArgs.push(k, strValue);
  }
  
  if (flatArgs.length > 0) {
    await redis.hset(key, ...flatArgs);
    await redis.expire(key, SESSION_TTL);
  }
};

const getDeskStatus = async (sessionId, deskId) => {
  const redis = getRedis();
  return redis.hgetall(keys.deskStatus(sessionId, deskId));
};

const getAllDeskStatuses = async (sessionId) => {
  const redis = getRedis();
  const pattern = keys.deskStatus(sessionId, '*').replace('*', '');
  
  // Scan for all desk keys in this session
  const deskKeys = [];
  let cursor = '0';
  
  do {
    const [newCursor, keys] = await redis.scan(cursor, 'MATCH', `${pattern}*`, 'COUNT', 100);
    cursor = newCursor;
    deskKeys.push(...keys);
  } while (cursor !== '0');

  const statuses = {};
  for (const key of deskKeys) {
    const deskId = key.split(':').pop();
    statuses[deskId] = await redis.hgetall(key);
  }
  
  return statuses;
};

// ============================================
// Assignment Lock (Prevent Race Conditions)
// ============================================

/**
 * Acquire assignment lock for a session
 * Uses Redis SETNX for atomic locking
 */
const acquireAssignmentLock = async (sessionId, ttlMs = 5000) => {
  const redis = getRedis();
  const lockKey = keys.assignmentLock(sessionId);
  const lockValue = Date.now().toString();
  
  const acquired = await redis.set(lockKey, lockValue, 'NX', 'PX', ttlMs);
  return acquired === 'OK' ? lockValue : null;
};

/**
 * Release assignment lock
 */
const releaseAssignmentLock = async (sessionId, lockValue) => {
  const redis = getRedis();
  const lockKey = keys.assignmentLock(sessionId);
  
  // Only release if we own the lock (check value matches)
  const script = `
    if redis.call("get", KEYS[1]) == ARGV[1] then
      return redis.call("del", KEYS[1])
    else
      return 0
    end
  `;
  
  await redis.eval(script, 1, lockKey, lockValue);
};

// ============================================
// Session Management
// ============================================

/**
 * Mark session as active in Redis
 */
const setSessionActive = async (sessionId, active = true) => {
  const redis = getRedis();
  if (active) {
    await redis.set(keys.sessionActive(sessionId), '1', 'EX', SESSION_TTL);
  } else {
    await redis.del(keys.sessionActive(sessionId));
  }
};

/**
 * Check if session is active
 */
const isSessionActive = async (sessionId) => {
  const redis = getRedis();
  const active = await redis.get(keys.sessionActive(sessionId));
  return active === '1';
};

/**
 * Clear all Redis data for a session (on session end)
 */
const clearSessionData = async (sessionId) => {
  const redis = getRedis();
  
  // Find all keys for this session
  const pattern = `queue:${sessionId}:*`;
  let cursor = '0';
  const keysToDelete = [];
  
  do {
    const [newCursor, foundKeys] = await redis.scan(cursor, 'MATCH', pattern, 'COUNT', 100);
    cursor = newCursor;
    keysToDelete.push(...foundKeys);
  } while (cursor !== '0');

  if (keysToDelete.length > 0) {
    await redis.del(...keysToDelete);
    logger.info(`[Redis] Cleared ${keysToDelete.length} keys for session ${sessionId}`);
  }
};

// ============================================
// Background Assignment Queue
// ============================================

/**
 * Get next pending assignment from global queue
 */
const popPendingAssignment = async () => {
  const redis = getRedis();
  const data = await redis.lpop(keys.pendingAssignments());
  return data ? JSON.parse(data) : null;
};

/**
 * Get pending assignments count
 */
const getPendingAssignmentsCount = async () => {
  const redis = getRedis();
  return redis.llen(keys.pendingAssignments());
};

module.exports = {
  // Keys (for testing/debugging)
  keys,
  
  // Worker management
  setWorkerState,
  getWorkerState,
  getAllWorkers,
  setWorkerBusy,
  setWorkerPaused,
  setWorkerOnline,
  setWorkerOffline,
  incrementWorkerCompletion,
  popAvailableWorker,
  
  // Booking queue
  addBookingToQueue,
  getBooking,
  popNextBooking,
  peekNextBooking,
  getQueueLength,
  getBookingPosition,
  removeBookingFromQueue,
  updateBookingStatus,
  
  // Desk status
  setDeskStatus,
  getDeskStatus,
  getAllDeskStatuses,
  
  // Locking
  acquireAssignmentLock,
  releaseAssignmentLock,
  
  // Session
  setSessionActive,
  isSessionActive,
  clearSessionData,
  
  // Background assignment
  popPendingAssignment,
  getPendingAssignmentsCount,
};
