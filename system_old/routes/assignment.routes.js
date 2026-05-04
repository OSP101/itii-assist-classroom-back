const express = require('express');
const router = express.Router();
const assignmentController = require('../controllers/assignment.controller');
const { authenticate, checkCourseActive } = require('../middlewares/auth');

// All routes require authentication
router.use(authenticate);

// GET /api/assignments - Get all assignments for a course
router.get('/', assignmentController.getAssignments);

// GET /api/assignments/:id - Get single assignment
router.get('/:id', assignmentController.getAssignment);

// POST /api/assignments - Create new assignment
router.post('/', checkCourseActive, assignmentController.createAssignment);

// PUT /api/assignments/:id - Update assignment
router.put('/:id', checkCourseActive, assignmentController.updateAssignment);

// DELETE /api/assignments/:id - Delete assignment
router.delete('/:id', checkCourseActive, assignmentController.deleteAssignment);

// PUT /api/assignments/reorder - Reorder assignments
router.put('/reorder/batch', checkCourseActive, assignmentController.reorderAssignments);

module.exports = router;
