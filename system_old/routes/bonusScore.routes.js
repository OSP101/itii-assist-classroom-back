/**
 * Bonus Score Routes
 * คะแนนพิเศษจากการถามตอบในห้องเรียน
 */

const express = require('express');
const router = express.Router();
const bonusScoreController = require('../controllers/bonusScore.controller');
const { authenticate, authorize, checkCourseActive } = require('../middlewares/auth');

// All routes require authentication
router.use(authenticate);

// Give bonus score (instructor/ta only)
router.post('/', authorize('instructor', 'ta'), checkCourseActive, bonusScoreController.giveBonusScore);

// Get enrolled students for bonus score selection
router.get('/course/:courseId/students', authorize('instructor', 'ta'), bonusScoreController.getEnrolledStudentsForBonus);

// Get all bonus scores for a course
router.get('/course/:courseId', authorize('instructor', 'ta'), bonusScoreController.getBonusScoresByCourse);

// Get bonus score summary for a course
router.get('/course/:courseId/summary', authorize('instructor', 'ta'), bonusScoreController.getBonusScoreSummary);

// Get bonus history for specific student in course
router.get('/course/:courseId/student/:studentId', authorize('instructor', 'ta'), bonusScoreController.getStudentBonusHistory);

// Delete bonus score record
router.delete('/:id', authorize('instructor', 'ta'), checkCourseActive, bonusScoreController.deleteBonusScore);

module.exports = router;
