const express = require('express');
const router = express.Router();
const scoreController = require('../controllers/score.controller');
const { authenticate, checkCourseActive } = require('../middlewares/auth');

// All routes require authentication
router.use(authenticate);

// GET /api/scores - Get scores for an assignment
router.get('/', scoreController.getScores);

// GET /api/scores/summary - Get student scores summary
router.get('/summary', scoreController.getStudentScoresSummary);

// GET /api/scores/matrix - Get score summary matrix (students x assignments)
router.get('/matrix', scoreController.getScoreSummaryMatrix);

// GET /api/scores/students/search - Search students for autocomplete
router.get('/students/search', scoreController.searchStudents);

// GET /api/scores/groups - Get groups for assignment
router.get('/groups', scoreController.getGroupsForAssignment);

// GET /api/scores/ungraded-summary - Get ungraded students summary per assignment
router.get('/ungraded-summary', scoreController.getUngradedSummary);

// POST /api/scores - Submit single score
router.post('/', checkCourseActive, scoreController.submitScore);

// POST /api/scores/bulk - Submit bulk scores
router.post('/bulk', checkCourseActive, scoreController.submitBulkScores);

// POST /api/scores/group - Submit group score
router.post('/group', checkCourseActive, scoreController.submitGroupScore);

// POST /api/scores/edit-request - Request score edit
router.post('/edit-request', checkCourseActive, scoreController.requestScoreEdit);

// GET /api/scores/edit-requests - Get pending edit requests
router.get('/edit-requests', scoreController.getPendingEditRequests);

// PUT /api/scores/edit-requests/:id - Review edit request
router.put('/edit-requests/:id', scoreController.reviewEditRequest);

module.exports = router;
