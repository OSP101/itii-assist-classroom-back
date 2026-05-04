const express = require('express');
const passport = require('passport');
const router = express.Router();

const { authController } = require('../controllers');
const { authenticate, validate } = require('../middlewares');
const { handleAvatarUpload } = require('../middlewares/upload');
const { authValidation } = require('../validations');

/**
 * @route   POST /api/auth/login
 * @desc    Login with username and password
 * @access  Public
 */
router.post('/login', validate(authValidation.login), authController.login);

/**
 * @route   POST /api/auth/refresh
 * @desc    Refresh access token
 * @access  Public
 */
router.post('/refresh', validate(authValidation.refreshToken), authController.refresh);

/**
 * @route   POST /api/auth/logout
 * @desc    Logout and revoke refresh token
 * @access  Public (but better with auth)
 */
router.post('/logout', authController.logout);

/**
 * @route   GET /api/auth/me
 * @desc    Get current user profile
 * @access  Private
 */
router.get('/me', authenticate, authController.getMe);

/**
 * @route   POST /api/auth/change-password
 * @desc    Change password
 * @access  Private
 */
router.post(
  '/change-password',
  authenticate,
  validate(authValidation.changePassword),
  authController.changePassword
);

/**
 * @route   POST /api/auth/force-change-password
 * @desc    Force change password (for first login)
 * @access  Private
 */
router.post(
  '/force-change-password',
  authenticate,
  authController.forceChangePassword
);

/**
 * @route   PUT /api/auth/profile
 * @desc    Update user profile
 * @access  Private
 */
router.put(
  '/profile',
  authenticate,
  validate(authValidation.updateProfile),
  authController.updateProfile
);

/**
 * @route   GET /api/auth/sessions
 * @desc    Get all active sessions for user
 * @access  Private
 */
router.get('/sessions', authenticate, authController.getSessions);

/**
 * @route   DELETE /api/auth/sessions/:sessionId
 * @desc    Revoke a specific session
 * @access  Private
 */
router.delete('/sessions/:sessionId', authenticate, authController.revokeSession);

/**
 * @route   POST /api/auth/sessions/revoke-all
 * @desc    Revoke all sessions except current
 * @access  Private
 */
router.post('/sessions/revoke-all', authenticate, authController.revokeAllSessions);

/**
 * @route   POST /api/auth/avatar
 * @desc    Upload user avatar
 * @access  Private
 */
router.post('/avatar', authenticate, handleAvatarUpload, authController.uploadUserAvatar);

/**
 * @route   DELETE /api/auth/avatar
 * @desc    Remove user avatar
 * @access  Private
 */
router.delete('/avatar', authenticate, authController.removeAvatar);

/**
 * @route   GET /api/auth/google
 * @desc    Initiate Google OAuth login
 * @access  Public
 */
router.get(
  '/google',
  (req, res, next) => {
    // Store action and link token in cookie for callback
    if (req.query.action === 'link') {
      res.cookie('oauth_action', 'link', { 
        httpOnly: true, 
        maxAge: 5 * 60 * 1000, // 5 minutes
        sameSite: 'lax'
      });
      // Store the access token for linking to existing user
      if (req.query.link_token) {
        res.cookie('oauth_link_token', req.query.link_token, { 
          httpOnly: true, 
          maxAge: 5 * 60 * 1000, // 5 minutes
          sameSite: 'lax'
        });
      }
    }
    next();
  },
  passport.authenticate('google', {
    scope: ['profile', 'email'],
    session: false,
  })
);

/**
 * @route   GET /api/auth/google/callback
 * @desc    Google OAuth callback
 * @access  Public
 */
router.get('/google/callback', authController.googleCallback);

/**
 * @route   GET /api/auth/github
 * @desc    Initiate GitHub OAuth login
 * @access  Public
 */
router.get(
  '/github',
  (req, res, next) => {
    // Store action and link token in cookie for callback
    if (req.query.action === 'link') {
      res.cookie('oauth_action', 'link', { 
        httpOnly: true, 
        maxAge: 5 * 60 * 1000, // 5 minutes
        sameSite: 'lax'
      });
      // Store the access token for linking to existing user
      if (req.query.link_token) {
        res.cookie('oauth_link_token', req.query.link_token, { 
          httpOnly: true, 
          maxAge: 5 * 60 * 1000, // 5 minutes
          sameSite: 'lax'
        });
      }
    }
    next();
  },
  passport.authenticate('github', {
    scope: ['user:email'],
    session: false,
  })
);

/**
 * @route   GET /api/auth/github/callback
 * @desc    GitHub OAuth callback
 * @access  Public
 */
router.get('/github/callback', authController.githubCallback);

/**
 * @route   POST /api/auth/apple
 * @desc    Initiate Apple OAuth login
 * @access  Public
 */
router.post(
  '/apple',
  passport.authenticate('apple', {
    session: false,
  })
);

/**
 * @route   POST /api/auth/apple/callback
 * @desc    Apple OAuth callback (Apple uses POST)
 * @access  Public
 */
router.post('/apple/callback', authController.appleCallback);

// ============================================
// Password Reset Routes
// ============================================

/**
 * @route   POST /api/auth/forgot-password
 * @desc    Request password reset email
 * @access  Public
 */
router.post('/forgot-password', authController.forgotPassword);

/**
 * @route   POST /api/auth/validate-reset-token
 * @desc    Validate password reset token
 * @access  Public
 */
router.post('/validate-reset-token', authController.validateResetToken);

/**
 * @route   POST /api/auth/reset-password
 * @desc    Reset password with token
 * @access  Public
 */
router.post('/reset-password', authController.resetPassword);

module.exports = router;
