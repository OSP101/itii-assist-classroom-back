const express = require('express');
const router = express.Router();
const courseController = require('../controllers/course.controller');
const { authenticate, authorize, checkCourseActive } = require('../middlewares/auth');

// All routes require authentication
router.use(authenticate);

// Get instructors/TAs list for dropdown (admin, instructor, and ta)
router.get('/instructors', authorize('admin', 'instructor', 'ta'), courseController.getInstructors);
router.get('/tas-list', authorize('admin', 'instructor', 'ta'), courseController.getTAsList);

// My courses (for instructor/TA)
router.get('/my-courses', authorize('instructor', 'ta'), courseController.getMyCourses);
router.get('/my-courses/stats', authorize('instructor', 'ta'), courseController.getMyCoursesStats);

// Course stats
router.get('/stats', authorize('admin', 'instructor'), courseController.getCourseStats);

// Course CRUD
router.get('/', authorize('admin', 'instructor', 'ta'), courseController.getCourses);
router.get('/:id', authorize('admin', 'instructor', 'ta'), courseController.getCourseById);
router.get('/:id/overview', authorize('admin', 'instructor', 'ta'), courseController.getCourseOverview);
router.post('/', authorize('admin', 'instructor'), courseController.createCourse);
router.put('/:id', authorize('admin', 'instructor'), courseController.updateCourse);
router.delete('/:id', authorize('admin', 'instructor'), courseController.deleteCourse);
router.patch('/:id/toggle-status', authorize('admin', 'instructor'), courseController.toggleCourseStatus);

// Section management (admin or course owner/TA)
router.post('/:id/sections', authorize('admin', 'instructor', 'ta'), checkCourseActive, courseController.addSection);
router.put('/:id/sections/:sectionId', authorize('admin', 'instructor', 'ta'), checkCourseActive, courseController.updateSection);
router.delete('/:id/sections/:sectionId', authorize('admin', 'instructor', 'ta'), checkCourseActive, courseController.removeSection);

// TA management (admin or course instructor only)
router.post('/:id/tas', authorize('admin', 'instructor'), checkCourseActive, courseController.addTA);
router.post('/:id/tas/bulk', authorize('admin', 'instructor'), checkCourseActive, courseController.bulkAddTAs);
router.delete('/:id/tas/:userId', authorize('admin', 'instructor'), checkCourseActive, courseController.removeTA);

// Instructor management (admin or course instructor only)
router.post('/:id/instructors', authorize('admin', 'instructor'), checkCourseActive, courseController.addInstructor);
router.post('/:id/instructors/bulk', authorize('admin', 'instructor'), checkCourseActive, courseController.bulkAddInstructors);
router.delete('/:id/instructors/:userId', authorize('admin', 'instructor'), checkCourseActive, courseController.removeInstructor);

// Student management in sections (admin, instructor, or TA of course)
router.get('/:id/sections/:sectionId/students', authorize('admin', 'instructor', 'ta'), courseController.getSectionStudents);
router.post('/:id/sections/:sectionId/students', authorize('admin', 'instructor', 'ta'), checkCourseActive, courseController.addStudentToSection);
router.post('/:id/sections/:sectionId/students/bulk', authorize('admin', 'instructor', 'ta'), checkCourseActive, courseController.bulkAddStudentsToSection);
router.delete('/:id/sections/:sectionId/students/:studentId', authorize('admin', 'instructor', 'ta'), checkCourseActive, courseController.removeStudentFromSection);

module.exports = router;
