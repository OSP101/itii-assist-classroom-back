/**
 * Queue Assignment Worker
 * 
 * Background process that handles booking assignments separately from
 * API request handlers and socket events.
 * 
 * This reduces database load and prevents timeout errors under high concurrency
 * by decoupling assignment logic from the request/response cycle.
 * 
 * Architecture:
 * 1. API/Socket handlers add bookings to Redis queue
 * 2. This worker polls the queue and processes assignments
 * 3. Socket events are emitted after successful assignment
 * 4. MySQL is updated asynchronously for persistence
 */

const {
  popAvailableWorker,
  setWorkerBusy,
  popNextBooking,
  peekNextBooking,
  updateBookingStatus,
  getQueueLength,
  isSessionActive,
  acquireAssignmentLock,
  releaseAssignmentLock,
  setDeskStatus,
  getWorkerState,
  getAllWorkers,
} = require('./redisQueueService');
const { getIO } = require('../config/socket');
const logger = require('./logger');

// Models for MySQL persistence (imported lazily to avoid circular deps)
let QueueBooking, QueueWorker, QueueDeskStatus, Student, User;

const loadModels = () => {
  if (!QueueBooking) {
    const models = require('../models');
    QueueBooking = models.QueueBooking;
    QueueWorker = models.QueueWorker;
    QueueDeskStatus = models.QueueDeskStatus;
    Student = models.Student;
    User = models.User;
  }
};

// Worker configuration
const POLL_INTERVAL_MS = 100; // Poll every 100ms
const IDLE_INTERVAL_MS = 500; // Slow down when no work
const MAX_BATCH_SIZE = 10; // Process up to 10 assignments per cycle
const SESSION_CACHE_TTL_MS = 30 * 1000; // Refresh active sessions list every 30s

let isRunning = false;
let pollInterval = null;

// In-memory cache for active session IDs to avoid DB query every poll cycle
let activeSessionsCache = null;
let cacheExpiresAt = 0;

/**
 * Start the assignment worker
 */
const startAssignmentWorker = () => {
  if (isRunning) {
    logger.warn('[AssignmentWorker] Already running');
    return;
  }

  isRunning = true;
  logger.info('[AssignmentWorker] Started');
  
  scheduleNextPoll(POLL_INTERVAL_MS);
};

/**
 * Stop the assignment worker
 */
const stopAssignmentWorker = () => {
  isRunning = false;
  if (pollInterval) {
    clearTimeout(pollInterval);
    pollInterval = null;
  }
  logger.info('[AssignmentWorker] Stopped');
};

/**
 * Schedule the next poll
 */
const scheduleNextPoll = (delayMs) => {
  if (!isRunning) return;
  
  pollInterval = setTimeout(async () => {
    try {
      const processed = await processAssignments();
      
      // Adjust polling interval based on workload
      const nextDelay = processed > 0 ? POLL_INTERVAL_MS : IDLE_INTERVAL_MS;
      scheduleNextPoll(nextDelay);
    } catch (error) {
      logger.error('[AssignmentWorker] Error in poll cycle:', error);
      scheduleNextPoll(IDLE_INTERVAL_MS);
    }
  }, delayMs);
};

/**
 * Process pending assignments
 * Returns number of assignments processed
 */
const processAssignments = async () => {
  loadModels();
  
  let processed = 0;
  
  // Get all active sessions that might have waiting bookings
  // We'll check both grading and help queues for each known session
  const activeSessions = await getActiveSessionsWithWork();
  
  for (const sessionId of activeSessions) {
    if (processed >= MAX_BATCH_SIZE) break;
    
    // Try to assign grading bookings
    const gradingResult = await tryAssignForSession(sessionId, 'grading');
    if (gradingResult) processed++;
    
    // Try to assign help bookings
    const helpResult = await tryAssignForSession(sessionId, 'help');
    if (helpResult) processed++;
  }
  
  return processed;
};

/**
 * Get active sessions that have waiting bookings
 */
const getActiveSessionsWithWork = async () => {
  const now = Date.now();

  // Return cached list if still valid
  if (activeSessionsCache !== null && now < cacheExpiresAt) {
    return activeSessionsCache;
  }

  // Cache expired — refresh from MySQL
  loadModels();
  try {
    const { QueueSession } = require('../models');
    const sessions = await QueueSession.findAll({
      where: { status: 'active' },
      attributes: ['id'],
      raw: true,
    });
    activeSessionsCache = sessions.map(s => s.id);
    cacheExpiresAt = now + SESSION_CACHE_TTL_MS;
    return activeSessionsCache;
  } catch (error) {
    logger.error('[AssignmentWorker] Error getting active sessions:', error);
    return activeSessionsCache || []; // fall back to stale cache on error
  }
};

/**
 * Invalidate the active sessions cache.
 * Call this whenever a session's status changes so the next poll picks up the change immediately.
 */
const invalidateSessionCache = () => {
  cacheExpiresAt = 0;
};

/**
 * Try to assign a booking for a specific session and type
 */
const tryAssignForSession = async (sessionId, bookingType) => {
  // Check if there are waiting bookings
  const queueLength = await getQueueLength(sessionId, bookingType);
  if (queueLength === 0) {
    return false;
  }

  // Check if there are available workers
  const worker = await popAvailableWorker(sessionId, bookingType);
  if (!worker) {
    return false;
  }

  // Acquire lock to prevent race conditions
  const lockValue = await acquireAssignmentLock(sessionId);
  if (!lockValue) {
    logger.debug(`[AssignmentWorker] Could not acquire lock for session ${sessionId}`);
    return false;
  }

  try {
    // Double-check worker is still available
    const currentWorker = await getWorkerState(sessionId, worker.userId);
    if (!currentWorker || currentWorker.status !== 'online') {
      logger.debug(`[AssignmentWorker] Worker ${worker.userId} no longer available`);
      return false;
    }

    // Pop the next booking
    const booking = await popNextBooking(sessionId, bookingType);
    if (!booking) {
      return false;
    }

    // Perform the assignment
    await performAssignment(sessionId, booking, worker);
    
    return true;
  } finally {
    await releaseAssignmentLock(sessionId, lockValue);
  }
};

/**
 * Perform the actual assignment
 * [FIX] Added MySQL status verification to prevent race condition with cancel operation
 */
const performAssignment = async (sessionId, booking, worker) => {
  loadModels();
  
  const now = Date.now();
  const userId = worker.userId;
  
  logger.info(`[AssignmentWorker] Assigning booking ${booking.id} to worker ${userId}`);

  // [FIX] Verify booking still exists and is in 'waiting' status in MySQL
  // This prevents race condition where cancel happened after Redis pop but before assignment
  const mysqlBooking = await QueueBooking.findOne({
    where: {
      id: booking.id,
      status: 'waiting', // Only assign if still waiting
    },
  });

  if (!mysqlBooking) {
    logger.warn(`[AssignmentWorker] Booking ${booking.id} no longer in 'waiting' status, skipping assignment`);
    // Return worker to available pool since assignment was skipped
    const { setWorkerOnline } = require('./redisQueueService');
    await setWorkerOnline(sessionId, userId);
    return;
  }

  // 1. Update Redis state (immediate, real-time)
  await setWorkerBusy(sessionId, userId, booking.id);
  
  await updateBookingStatus(sessionId, booking.id, {
    status: 'in_progress',
    assignedWorkerId: userId,
    assignedAt: now,
  });

  // Update desk status in Redis
  await setDeskStatus(sessionId, booking.deskId, {
    [booking.bookingType === 'grading' ? 'gradingStatus' : 'helpStatus']: 'in_progress',
    [booking.bookingType === 'grading' ? 'gradingBookingId' : 'helpBookingId']: booking.id.toString(),
  });

  // 2. Emit socket events (real-time notification)
  const io = getIO();
  if (io) {
    // Get full booking data for socket event
    const fullBooking = await getFullBookingData(booking.id);
    
    // Notify queue room (projector/dashboard)
    io.to(`queue-${sessionId}`).emit('booking-assigned', {
      booking: fullBooking,
      workerId: userId,
    });

    // Notify worker
    const workerRoom = `worker-${userId}`;
    io.to(workerRoom).emit('new-task', {
      booking: fullBooking,
    });

    // Notify student
    io.to(`booking-${booking.id}`).emit('booking-assigned', {
      booking: fullBooking,
    });

    logger.debug(`[AssignmentWorker] Socket events emitted for booking ${booking.id}`);
  }

  // 3. Persist to MySQL asynchronously (non-blocking)
  persistAssignmentToMySQL(booking.id, userId, now).catch(err => {
    logger.error(`[AssignmentWorker] MySQL persistence error for booking ${booking.id}:`, err);
  });
};

/**
 * Get full booking data with student info for socket events
 */
const getFullBookingData = async (bookingId) => {
  loadModels();
  
  try {
    const booking = await QueueBooking.findByPk(bookingId, {
      include: [
        { model: Student, as: 'student', attributes: ['id', 'student_id', 'full_name'] },
      ],
    });
    return booking ? booking.toJSON() : null;
  } catch (error) {
    logger.error(`[AssignmentWorker] Error fetching booking ${bookingId}:`, error);
    return null;
  }
};

/**
 * Persist assignment to MySQL (async, non-blocking)
 */
const persistAssignmentToMySQL = async (bookingId, workerId, assignedAt) => {
  loadModels();
  
  // Update booking in MySQL
  await QueueBooking.update(
    {
      assigned_worker_id: workerId,
      assigned_at: new Date(assignedAt),
      status: 'in_progress',
      started_at: new Date(assignedAt),
    },
    { where: { id: bookingId } }
  );

  // Update worker in MySQL
  const booking = await QueueBooking.findByPk(bookingId);
  if (booking) {
    await QueueWorker.update(
      {
        status: 'busy',
        current_booking_id: bookingId,
        last_active_at: new Date(),
      },
      { 
        where: { 
          queue_session_id: booking.queue_session_id,
          user_id: workerId,
        } 
      }
    );

    // Update desk status in MySQL
    await QueueDeskStatus.update(
      {
        [booking.booking_type === 'grading' ? 'grading_status' : 'help_status']: 'in_progress',
      },
      {
        where: {
          queue_session_id: booking.queue_session_id,
          desk_id: booking.desk_id,
        },
      }
    );
  }

  logger.debug(`[AssignmentWorker] MySQL persisted for booking ${bookingId}`);
};

/**
 * Manually trigger assignment for a session (called after worker joins)
 */
const triggerAssignmentForSession = async (sessionId) => {
  // Process both types
  await tryAssignForSession(sessionId, 'grading');
  await tryAssignForSession(sessionId, 'help');
};

module.exports = {
  startAssignmentWorker,
  stopAssignmentWorker,
  triggerAssignmentForSession,
  invalidateSessionCache,
};
