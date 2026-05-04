const express = require('express');
const router = express.Router();
const examScoreController = require('../controllers/examScore.controller');
const { authenticate, checkCourseActive } = require('../middlewares/auth');

// All routes require authentication
router.use(authenticate);

// ============================================
// Exam Settings Routes
// ============================================

// GET /api/courses/:courseId/exam-settings - Get exam settings
router.get('/:courseId/exam-settings', examScoreController.getExamSettings);

// PUT /api/courses/:courseId/exam-settings/:settingId - Update exam setting
router.put('/:courseId/exam-settings/:settingId', checkCourseActive, examScoreController.updateExamSetting);

// ============================================
// Exam Scores Routes
// ============================================

// GET /api/courses/:courseId/exam-scores - Get exam scores
router.get('/:courseId/exam-scores', examScoreController.getExamScores);

// GET /api/courses/:courseId/exam-scores/stats - Get exam score statistics
router.get('/:courseId/exam-scores/stats', examScoreController.getExamScoreStats);

// POST /api/courses/:courseId/exam-scores - Save single exam score
router.post('/:courseId/exam-scores', checkCourseActive, examScoreController.saveExamScore);

// POST /api/courses/:courseId/exam-scores/bulk - Bulk save exam scores
router.post('/:courseId/exam-scores/bulk', checkCourseActive, examScoreController.bulkSaveExamScores);

// DELETE /api/courses/:courseId/exam-scores/:scoreId - Delete exam score
router.delete('/:courseId/exam-scores/:scoreId', checkCourseActive, examScoreController.deleteExamScore);

module.exports = router;