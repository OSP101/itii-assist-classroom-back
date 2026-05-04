/**
 * Attendance Routes
 * API endpoints for attendance system
 */

const express = require('express');
const router = express.Router();
const attendanceController = require('../controllers/attendance.controller');
const { authenticate, checkCourseActive } = require('../middlewares/auth');

// ============================================
// Public Routes (for student check-in)
// ============================================

/**
 * @route   GET /api/attendance/check-in/:sessionId/info
 * @desc    Get session info for check-in page (public)
 * @access  Public
 */
router.get('/check-in/:sessionId/info', attendanceController.getSessionInfo);

/**
 * @route   POST /api/attendance/check-in/:sessionId
 * @desc    Student check-in (requires Google auth from frontend)
 * @access  Public (with Google verification)
 */
router.post('/check-in/:sessionId', attendanceController.studentCheckIn);

/**
 * @route   POST /api/attendance/verify-student
 * @desc    Verify student by Google email
 * @access  Public
 */
router.post('/verify-student', attendanceController.verifyStudent);

// ============================================
// Protected Routes (Instructor/TA only)
// ============================================

// All routes below require authentication
router.use(authenticate);

/**
 * @route   GET /api/attendance
 * @desc    Get all attendance sessions for a course
 * @access  Instructor, TA
 */
router.get('/', attendanceController.getAttendanceSessions);

/**
 * @route   POST /api/attendance
 * @desc    Create new attendance session
 * @access  Instructor, TA
 */
router.post('/', checkCourseActive, attendanceController.createAttendanceSession);

/**
 * @route   GET /api/attendance/:id
 * @desc    Get single attendance session with records
 * @access  Instructor, TA
 */
router.get('/:id', attendanceController.getAttendanceSession);

/**
 * @route   PUT /api/attendance/:id
 * @desc    Update attendance session
 * @access  Instructor, TA
 */
router.put('/:id', attendanceController.updateAttendanceSession);

/**
 * @route   POST /api/attendance/:id/preview-section-change
 * @desc    Preview impact of removing sections (which checked-in students will be affected)
 * @access  Instructor, TA
 */
router.post('/:id/preview-section-change', attendanceController.previewSectionChange);

/**
 * @route   POST /api/attendance/:id/preview-time-change
 * @desc    Preview impact of changing attendance time rules on existing check-ins
 * @access  Instructor, TA
 */
router.post('/:id/preview-time-change', attendanceController.previewTimeChange);

/**
 * @route   POST /api/attendance/:id/apply-time-change
 * @desc    Apply time change and re-evaluate all existing check-in records
 * @access  Instructor, TA
 */
router.post('/:id/apply-time-change', attendanceController.applyTimeChange);

/**
 * @route   DELETE /api/attendance/:id
 * @desc    Delete attendance session
 * @access  Instructor, TA
 */
router.delete('/:id', attendanceController.deleteAttendanceSession);

/**
 * @route   POST /api/attendance/:id/activate
 * @desc    Activate attendance session (open for check-in)
 * @access  Instructor, TA
 */
router.post('/:id/activate', attendanceController.activateSession);

/**
 * @route   POST /api/attendance/:id/close
 * @desc    Close attendance session
 * @access  Instructor, TA
 */
router.post('/:id/close', attendanceController.closeSession);

/**
 * @route   GET /api/attendance/:id/records
 * @desc    Get all records for a session
 * @access  Instructor, TA
 */
router.get('/:id/records', attendanceController.getAttendanceRecords);

/**
 * @route   PUT /api/attendance/:id/records/:recordId
 * @desc    Update student attendance status (manual)
 * @access  Instructor, TA
 */
router.put('/:id/records/:recordId', attendanceController.updateAttendanceRecord);

module.exports = router;
