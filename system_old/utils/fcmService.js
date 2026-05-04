/**
 * Firebase Cloud Messaging Service
 * Handles sending push notifications to workers and students
 */

const admin = require('firebase-admin');
const logger = require('./logger');
const { FcmToken, NotificationLog } = require('../models');
const { Op } = require('sequelize');

// Initialize Firebase Admin SDK
let firebaseApp = null;

const initializeFirebase = () => {
  if (firebaseApp) return firebaseApp;

  try {
    // Check if service account is configured
    const serviceAccount = process.env.FIREBASE_SERVICE_ACCOUNT;
    
    if (serviceAccount) {
      // Parse the service account JSON from environment variable
      const credentials = JSON.parse(serviceAccount);
      
      firebaseApp = admin.initializeApp({
        credential: admin.credential.cert(credentials),
      });
      
      logger.info('✅ Firebase Admin SDK initialized successfully');
    } else if (process.env.GOOGLE_APPLICATION_CREDENTIALS) {
      // Use default credentials from file
      firebaseApp = admin.initializeApp({
        credential: admin.credential.applicationDefault(),
      });
      
      logger.info('✅ Firebase Admin SDK initialized with default credentials');
    } else {
      logger.warn('⚠️ Firebase Admin SDK not configured. Push notifications will not work.');
      return null;
    }
    
    return firebaseApp;
  } catch (error) {
    logger.error('❌ Error initializing Firebase Admin SDK:', error);
    return null;
  }
};

// Initialize on load
initializeFirebase();

/**
 * Send notification to a single FCM token
 */
const sendToToken = async (token, notification, data = {}) => {
  if (!firebaseApp) {
    logger.warn('Firebase not initialized, skipping push notification');
    return { success: false, error: 'Firebase not initialized' };
  }

  try {
    const message = {
      token,
      notification: {
        title: notification.title,
        body: notification.body,
      },
      data: {
        ...data,
        // Convert all values to strings (FCM requirement)
        ...Object.fromEntries(
          Object.entries(data).map(([k, v]) => [k, String(v)])
        ),
      },
      webpush: {
        notification: {
          icon: notification.icon || '/images/logo.png',
          badge: '/images/badge.png',
          vibrate: [200, 100, 200],
          requireInteraction: notification.requireInteraction || false,
        },
        fcmOptions: {
          link: data.url || '/',
        },
      },
    };

    const response = await admin.messaging().send(message);
    logger.info('✅ Push notification sent:', response);
    
    return { success: true, messageId: response };
  } catch (error) {
    logger.error('❌ Error sending push notification:', error);
    
    // Handle invalid token
    if (
      error.code === 'messaging/invalid-registration-token' ||
      error.code === 'messaging/registration-token-not-registered'
    ) {
      // Deactivate the invalid token
      await FcmToken.update(
        { is_active: false },
        { where: { fcm_token: token } }
      );
    }
    
    return { success: false, error: error.message };
  }
};

/**
 * Send notification to multiple tokens
 */
const sendToMultipleTokens = async (tokens, notification, data = {}) => {
  if (!firebaseApp || tokens.length === 0) {
    return { success: false, sent: 0, failed: tokens.length };
  }

  const results = await Promise.all(
    tokens.map(token => sendToToken(token, notification, data))
  );

  const sent = results.filter(r => r.success).length;
  const failed = results.filter(r => !r.success).length;

  return { success: true, sent, failed, results };
};

/**
 * Send notification to a worker by user_id
 */
const sendToWorker = async (userId, sessionId, notification, data = {}) => {
  const tokens = await FcmToken.findAll({
    where: {
      user_id: userId,
      user_type: 'worker',
      is_active: true,
      [Op.or]: [
        { session_id: sessionId },
        { session_id: null }, // Also send to tokens not specific to a session
      ],
    },
  });

  if (tokens.length === 0) {
    logger.info(`No FCM tokens found for worker ${userId}`);
    return { success: false, error: 'No tokens found' };
  }

  const tokenStrings = tokens.map(t => t.fcm_token);
  const result = await sendToMultipleTokens(tokenStrings, notification, {
    ...data,
    type: 'new-task',
  });

  // Log notification
  for (const token of tokens) {
    await NotificationLog.create({
      fcm_token_id: token.id,
      notification_type: 'new-task',
      title: notification.title,
      body: notification.body,
      data,
      status: result.success ? 'sent' : 'failed',
      sent_at: new Date(),
    });
  }

  return result;
};

/**
 * Send notification to a student by booking_id
 */
const sendToStudent = async (bookingId, notification, data = {}, notificationType = 'queue-ready') => {
  const tokens = await FcmToken.findAll({
    where: {
      booking_id: bookingId,
      user_type: 'student',
      is_active: true,
    },
  });

  if (tokens.length === 0) {
    logger.info(`No FCM tokens found for booking ${bookingId}`);
    return { success: false, error: 'No tokens found' };
  }

  const tokenStrings = tokens.map(t => t.fcm_token);
  const result = await sendToMultipleTokens(tokenStrings, notification, {
    ...data,
    type: notificationType,
  });

  // Log notification
  for (const token of tokens) {
    await NotificationLog.create({
      fcm_token_id: token.id,
      notification_type: notificationType,
      title: notification.title,
      body: notification.body,
      data,
      status: result.success ? 'sent' : 'failed',
      sent_at: new Date(),
    });
  }

  return result;
};

/**
 * Send "New Task" notification to worker
 */
const notifyWorkerNewTask = async (workerId, sessionId, booking) => {
  const notification = {
    title: '📋 มีงานใหม่!',
    body: `โต๊ะ ${booking.desk_number} - ${booking.booking_type === 'grading' ? 'ตรวจงาน' : 'ขอความช่วยเหลือ'}`,
    requireInteraction: true,
  };

  const data = {
    type: 'new-task',
    bookingId: String(booking.id),
    sessionId: String(sessionId),
    deskNumber: booking.desk_number,
    bookingType: booking.booking_type,
    queueNumber: String(booking.queue_number),
    workerUrl: `/classroom/${booking.course_id}/queue/${sessionId}/worker`,
  };

  return sendToWorker(workerId, sessionId, notification, data);
};

/**
 * Send "Your Turn" notification to student (when they're being served)
 */
const notifyStudentYourTurn = async (booking, worker) => {
  const notification = {
    title: '🎉 ถึงคิวของคุณแล้ว!',
    body: `${worker.full_name || 'ผู้ช่วยสอน'} กำลังตรวจงานของคุณ`,
    requireInteraction: true,
  };

  const data = {
    type: 'queue-ready',
    bookingId: String(booking.id),
    queueNumber: String(booking.queue_number),
    workerName: worker.full_name || 'ผู้ช่วยสอน',
    pinCode: booking.pinCode || '',
    bookingUrl: `/queue/book?pin=${booking.pinCode}`,
  };

  return sendToStudent(booking.id, notification, data, 'queue-ready');
};

/**
 * Send "Booking Completed" notification to student
 */
const notifyStudentCompleted = async (booking, score = null) => {
  let body = 'การตรวจงานของคุณเสร็จสิ้นแล้ว';
  if (score !== null && booking.booking_type === 'grading') {
    body = `ตรวจงานเสร็จแล้ว คะแนน: ${score}`;
  }

  const notification = {
    title: '✅ ตรวจเสร็จแล้ว!',
    body,
    requireInteraction: false,
  };

  const data = {
    type: 'booking-completed',
    bookingId: String(booking.id),
    queueNumber: String(booking.queue_number),
    score: score !== null ? String(score) : '',
    pinCode: booking.pinCode || '',
    bookingUrl: `/queue/book?pin=${booking.pinCode}`,
  };

  return sendToStudent(booking.id, notification, data, 'booking-completed');
};

/**
 * Send position update to student (optional - can be frequent)
 */
const notifyStudentPositionUpdate = async (booking, position) => {
  // Only notify when position is low (close to their turn)
  if (position > 3) return { success: false, skipped: true };

  const notification = {
    title: `⏳ เหลืออีก ${position} คิว`,
    body: `อีกไม่นานจะถึงคิวของคุณ กรุณารออยู่ที่โต๊ะ`,
    requireInteraction: false,
  };

  const data = {
    type: 'queue-ready',
    bookingId: String(booking.id),
    position: String(position),
    queueNumber: String(booking.queue_number),
  };

  return sendToStudent(booking.id, notification, data, 'queue-ready');
};

/**
 * Send session closed notification to all active bookings
 */
const notifySessionClosed = async (sessionId, bookings) => {
  const notification = {
    title: '🔔 Session ถูกปิด',
    body: 'การจองคิวถูกยกเลิก กรุณาติดต่อผู้ช่วยสอน',
    requireInteraction: true,
  };

  const results = [];
  for (const booking of bookings) {
    const data = {
      type: 'session-closed',
      sessionId: String(sessionId),
      bookingId: String(booking.id),
    };

    const result = await sendToStudent(booking.id, notification, data, 'session-closed');
    results.push(result);
  }

  return results;
};

module.exports = {
  initializeFirebase,
  sendToToken,
  sendToMultipleTokens,
  sendToWorker,
  sendToStudent,
  notifyWorkerNewTask,
  notifyStudentYourTurn,
  notifyStudentCompleted,
  notifyStudentPositionUpdate,
  notifySessionClosed,
};
