const express = require('express');
const router = express.Router();
const classroomController = require('../controllers/classroom.controller');
const { authenticate, authorize } = require('../middlewares/auth');

// All routes require authentication
router.use(authenticate);

// Get classroom statistics
router.get('/stats', classroomController.getStats);

// Get all classrooms
router.get('/', classroomController.getClassrooms);

// Get single classroom
router.get('/:id', classroomController.getClassroomById);

// Create classroom (admin/instructor only)
router.post('/', authorize('admin', 'instructor'), classroomController.createClassroom);

// Update classroom info
router.put('/:id', authorize('admin', 'instructor'), classroomController.updateClassroom);

// Update classroom layout (desks)
router.put('/:id/layout', authorize('admin', 'instructor'), classroomController.updateLayout);

// Toggle classroom active status
router.patch('/:id/toggle-status', authorize('admin', 'instructor'), classroomController.toggleStatus);

// Restore soft-deleted classroom
router.post('/:id/restore', authorize('admin'), classroomController.restoreClassroom);

// Delete classroom
router.delete('/:id', authorize('admin'), classroomController.deleteClassroom);

module.exports = router;
