const express = require('express');
const router = express.Router();
const scoreEditRequestController = require('../controllers/scoreEditRequest.controller');
const { authenticate, authorize, checkCourseActive } = require('../middlewares/auth');
const { handleScoreEditImageUpload } = require('../middlewares/upload');

// All routes require authentication
router.use(authenticate);

// Get edit requests for a course (instructor only)
router.get('/', scoreEditRequestController.getEditRequests);

// Get pending count for a course (instructor only)
router.get('/pending-count', scoreEditRequestController.getPendingCount);

// Create edit request (TA) - with optional image upload
router.post('/', handleScoreEditImageUpload, checkCourseActive, scoreEditRequestController.createEditRequest);

// Create batch edit request for group (TA) - with optional image upload
router.post('/batch', handleScoreEditImageUpload, checkCourseActive, scoreEditRequestController.createBatchEditRequest);

// Cancel pending edit request (requester only)
router.delete('/:id/cancel', scoreEditRequestController.cancelEditRequest);

// Batch approve edit requests (instructor only) - for group approval
router.post('/batch-approve', checkCourseActive, scoreEditRequestController.batchApproveEditRequests);

// Batch reject edit requests (instructor only) - for group rejection
router.post('/batch-reject', checkCourseActive, scoreEditRequestController.batchRejectEditRequests);

// Approve edit request (instructor only)
router.post('/:id/approve', checkCourseActive, scoreEditRequestController.approveEditRequest);

// Reject edit request (instructor only)
router.post('/:id/reject', checkCourseActive, scoreEditRequestController.rejectEditRequest);

module.exports = router;
