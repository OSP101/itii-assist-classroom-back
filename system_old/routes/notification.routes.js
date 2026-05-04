const express = require('express');
const router = express.Router();
const notificationController = require('../controllers/notification.controller');
const { authenticate, optionalAuth } = require('../middlewares/auth');

// Public routes (for students without authentication)
router.post('/register', optionalAuth, notificationController.registerToken);
router.post('/unregister', optionalAuth, notificationController.unregisterToken);
router.post('/update-booking', notificationController.updateBookingForToken);

// Protected routes (for authenticated users)
router.get('/tokens', authenticate, notificationController.getUserTokens);
router.get('/logs', authenticate, notificationController.getNotificationLogs);

module.exports = router;
