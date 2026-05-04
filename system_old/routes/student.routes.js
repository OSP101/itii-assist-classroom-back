/**
 * Student Routes - API endpoints for student management
 */

const express = require('express');
const router = express.Router();
const { authenticate, authorize } = require('../middlewares/auth');
const studentController = require('../controllers/student.controller');

/**
 * @route   GET /api/students/lookup/:student_id
 * @desc    Lookup student scores by student_id (public endpoint for students)
 * @access  Public - no authentication required
 */
router.get('/lookup/:student_id', studentController.lookupStudentScores);

// All routes below require authentication
router.use(authenticate);

/**
 * @route   POST /api/students/search-by-ids
 * @desc    Search students by multiple student IDs (for bulk operations)
 * @access  Admin, Instructor, TA
 */
router.post('/search-by-ids', studentController.searchStudentsByIds);

/**
 * @route   GET /api/students
 * @desc    Get all students with pagination and filters
 * @access  Admin, Instructor, TA
 */
router.get('/', studentController.getStudents);

/**
 * @route   GET /api/students/stats
 * @desc    Get student statistics
 * @access  Admin, Instructor, TA
 */
router.get('/stats', studentController.getStudentStats);

/**
 * @route   GET /api/students/:id
 * @desc    Get single student by ID
 * @access  Admin, Instructor, TA
 */
router.get('/:id', studentController.getStudentById);

// Routes below require admin role
router.use(authorize('admin'));

/**
 * @route   POST /api/students
 * @desc    Create new student
 * @access  Admin only
 */
router.post('/', studentController.createStudent);

/**
 * @route   POST /api/students/import
 * @desc    Import students from CSV/Excel data
 * @access  Admin only
 */
router.post('/import', studentController.importStudents);

/**
 * @route   PUT /api/students/:id
 * @desc    Update student
 * @access  Admin only
 */
router.put('/:id', studentController.updateStudent);

/**
 * @route   DELETE /api/students/:id
 * @desc    Delete student
 * @access  Admin only
 */
router.delete('/:id', studentController.deleteStudent);

/**
 * @route   PATCH /api/students/:id/status
 * @desc    Toggle student active status
 * @access  Admin only
 */
router.patch('/:id/status', studentController.toggleStudentStatus);

module.exports = router;
