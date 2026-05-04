const express = require('express');
const router = express.Router();
const feedbackController = require('../controllers/feedback.controller');
const { authenticate, isAdmin } = require('../middlewares/auth');
const validate = require('../middlewares/validate');
const feedbackValidation = require('../validations/feedback.validation');

// Public/User routes (can be anonymous or authenticated)
router.post(
  '/',
  authenticate, // Optional auth - will get user if logged in
  validate(feedbackValidation.createFeedback),
  feedbackController.createFeedback
);

// User routes (must be authenticated)
router.get('/my', authenticate, feedbackController.getMyFeedbacks);

// Admin routes
router.get(
  '/stats',
  authenticate,
  isAdmin,
  feedbackController.getFeedbackStats
);

router.get(
  '/',
  authenticate,
  isAdmin,
  validate(feedbackValidation.getFeedbacks),
  feedbackController.getFeedbacks
);

router.get(
  '/:id',
  authenticate,
  isAdmin,
  feedbackController.getFeedbackById
);

router.put(
  '/:id',
  authenticate,
  isAdmin,
  validate(feedbackValidation.updateFeedback),
  feedbackController.updateFeedback
);

router.delete(
  '/:id',
  authenticate,
  isAdmin,
  feedbackController.deleteFeedback
);

module.exports = router;
