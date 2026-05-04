/**
 * User Routes - API endpoints for user management
 */

const express = require('express');
const router = express.Router();
const { authenticate, authorize } = require('../middlewares/auth');
const userController = require('../controllers/user.controller');

// All routes require authentication and admin role
router.use(authenticate);
router.use(authorize('admin'));

/**
 * @route   GET /api/users
 * @desc    Get all users with pagination and filters
 * @access  Admin only
 */
router.get('/', userController.getUsers);

/**
 * @route   GET /api/users/stats
 * @desc    Get user statistics
 * @access  Admin only
 */
router.get('/stats', userController.getUserStats);

/**
 * @route   GET /api/users/:id
 * @desc    Get single user by ID
 * @access  Admin only
 */
router.get('/:id', userController.getUserById);

/**
 * @route   POST /api/users
 * @desc    Create new user
 * @access  Admin only
 */
router.post('/', userController.createUser);

/**
 * @route   PUT /api/users/:id
 * @desc    Update user
 * @access  Admin only
 */
router.put('/:id', userController.updateUser);

/**
 * @route   DELETE /api/users/:id
 * @desc    Delete user
 * @access  Admin only
 */
router.delete('/:id', userController.deleteUser);

/**
 * @route   PATCH /api/users/:id/status
 * @desc    Toggle user active status
 * @access  Admin only
 */
router.patch('/:id/status', userController.toggleUserStatus);

module.exports = router;
