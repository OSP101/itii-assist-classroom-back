const { FcmToken, NotificationLog, User, QueueBooking, QueueSession } = require('../models');
const logger = require('../utils/logger');
const { Op } = require('sequelize');

/**
 * Register FCM token
 * POST /api/notifications/register
 */
const registerToken = async (req, res) => {
  try {
    const { fcm_token, user_type, user_id, target_id, device_info, student_id } = req.body;

    if (!fcm_token || !user_type) {
      return res.status(400).json({
        success: false,
        error: { message: 'fcm_token and user_type are required' },
      });
    }

    // Prepare token data
    const tokenData = {
      fcm_token,
      user_type,
      device_info,
      is_active: true,
      last_used_at: new Date(),
    };

    // For workers
    if (user_type === 'worker') {
      if (!user_id) {
        return res.status(400).json({
          success: false,
          error: { message: 'user_id is required for workers' },
        });
      }
      tokenData.user_id = user_id;
      tokenData.session_id = target_id; // session_id
    }

    // For students
    if (user_type === 'student') {
      tokenData.student_id = student_id;
      tokenData.booking_id = target_id; // booking_id
    }

    // Upsert token (update if exists, create if not)
    const [token, created] = await FcmToken.upsert(tokenData, {
      returning: true,
    });

    // If not using upsert with returning, try findOrCreate
    if (!token) {
      const existingToken = await FcmToken.findOne({
        where: { fcm_token },
      });

      if (existingToken) {
        await existingToken.update(tokenData);
        return res.json({
          success: true,
          data: { id: existingToken.id, created: false },
        });
      } else {
        const newToken = await FcmToken.create(tokenData);
        return res.json({
          success: true,
          data: { id: newToken.id, created: true },
        });
      }
    }

    return res.json({
      success: true,
      data: { id: token.id, created },
    });
  } catch (error) {
    logger.error('Error registering FCM token:', error);
    
    // Handle duplicate key error
    if (error.name === 'SequelizeUniqueConstraintError') {
      // Token already exists, update it
      try {
        const { fcm_token, user_type, user_id, target_id, device_info, student_id } = req.body;
        
        const existingToken = await FcmToken.findOne({
          where: { fcm_token },
        });

        if (existingToken) {
          await existingToken.update({
            user_type,
            user_id: user_type === 'worker' ? user_id : null,
            student_id: user_type === 'student' ? student_id : null,
            session_id: user_type === 'worker' ? target_id : null,
            booking_id: user_type === 'student' ? target_id : null,
            device_info,
            is_active: true,
            last_used_at: new Date(),
          });

          return res.json({
            success: true,
            data: { id: existingToken.id, created: false },
          });
        }
      } catch (updateError) {
        logger.error('Error updating existing token:', updateError);
      }
    }
    
    return res.status(500).json({
      success: false,
      error: { message: 'Failed to register FCM token' },
    });
  }
};

/**
 * Unregister FCM token
 * POST /api/notifications/unregister
 */
const unregisterToken = async (req, res) => {
  try {
    const { fcm_token } = req.body;

    if (!fcm_token) {
      return res.status(400).json({
        success: false,
        error: { message: 'fcm_token is required' },
      });
    }

    const deleted = await FcmToken.destroy({
      where: { fcm_token },
    });

    return res.json({
      success: true,
      data: { deleted: deleted > 0 },
    });
  } catch (error) {
    logger.error('Error unregistering FCM token:', error);
    return res.status(500).json({
      success: false,
      error: { message: 'Failed to unregister FCM token' },
    });
  }
};

/**
 * Update booking ID for student token
 * POST /api/notifications/update-booking
 */
const updateBookingForToken = async (req, res) => {
  try {
    const { fcm_token, booking_id } = req.body;

    if (!fcm_token || !booking_id) {
      return res.status(400).json({
        success: false,
        error: { message: 'fcm_token and booking_id are required' },
      });
    }

    const [updated] = await FcmToken.update(
      { booking_id, last_used_at: new Date() },
      { where: { fcm_token, user_type: 'student' } }
    );

    return res.json({
      success: true,
      data: { updated: updated > 0 },
    });
  } catch (error) {
    logger.error('Error updating booking for token:', error);
    return res.status(500).json({
      success: false,
      error: { message: 'Failed to update booking for token' },
    });
  }
};

/**
 * Get user's FCM tokens
 * GET /api/notifications/tokens
 */
const getUserTokens = async (req, res) => {
  try {
    const userId = req.user.id;

    const tokens = await FcmToken.findAll({
      where: { user_id: userId, is_active: true },
      attributes: ['id', 'user_type', 'device_info', 'last_used_at', 'created_at'],
    });

    return res.json({
      success: true,
      data: tokens,
    });
  } catch (error) {
    logger.error('Error getting user tokens:', error);
    return res.status(500).json({
      success: false,
      error: { message: 'Failed to get user tokens' },
    });
  }
};

/**
 * Get notification logs
 * GET /api/notifications/logs
 */
const getNotificationLogs = async (req, res) => {
  try {
    const userId = req.user.id;
    const { limit = 50, offset = 0 } = req.query;

    const logs = await NotificationLog.findAll({
      include: [
        {
          model: FcmToken,
          as: 'fcmToken',
          where: { user_id: userId },
          attributes: [],
        },
      ],
      order: [['created_at', 'DESC']],
      limit: parseInt(limit),
      offset: parseInt(offset),
    });

    return res.json({
      success: true,
      data: logs,
    });
  } catch (error) {
    logger.error('Error getting notification logs:', error);
    return res.status(500).json({
      success: false,
      error: { message: 'Failed to get notification logs' },
    });
  }
};

module.exports = {
  registerToken,
  unregisterToken,
  updateBookingForToken,
  getUserTokens,
  getNotificationLogs,
};
