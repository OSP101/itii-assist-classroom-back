/**
 * Queue Public Routes
 * เส้นทาง API สำหรับระบบคิวที่ไม่ต้องระบุ courseId (สำหรับ projector view และ student booking)
 */

const express = require('express');
const router = express.Router();
const queueController = require('../controllers/queue.controller');

// ============================================
// Public Routes (no authentication required)
// ============================================

// Verify PIN code - for students to verify session
router.post('/verify-pin', queueController.verifyPIN);

// Validate booking info before creating - check student enrollment and desk status
router.post('/validate', queueController.validateBookingInfo);

// Check existing active booking for student - for restoring state after refresh
router.post('/check-existing', queueController.checkExistingBooking);

// Create booking - student creates a booking
router.post('/bookings', queueController.createBooking);

// Get booking status - student checks their booking status
router.get('/bookings/:bookingId/status', queueController.getBookingStatus);

// ============================================
// Projector View (public for display)
// ============================================

// Get desk statuses for projector display
router.get('/sessions/:sessionId/desk-statuses', queueController.getDeskStatuses);

// Toggle session status (active/paused) - for projector toggle switch
router.post('/sessions/:sessionId/status', queueController.updateQueueSessionStatusPublic);

// Cancel booking - for student and projector cancel
router.post('/bookings/:bookingId/cancel', queueController.cancelBooking);

module.exports = router;
