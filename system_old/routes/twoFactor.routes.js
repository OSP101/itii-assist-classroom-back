const express = require('express');
const router = express.Router();
const { authenticate } = require('../middlewares/auth');
const twoFactorController = require('../controllers/twoFactor.controller');

/**
 * @route   GET /api/auth/2fa/status
 * @desc    Get 2FA status for current user
 * @access  Private
 */
router.get('/status', authenticate, twoFactorController.getStatus);

/**
 * @route   POST /api/auth/2fa/setup/totp
 * @desc    Start TOTP 2FA setup
 * @access  Private
 */
router.post('/setup/totp', authenticate, twoFactorController.setupTOTP);

/**
 * @route   POST /api/auth/2fa/setup/email
 * @desc    Start Email 2FA setup
 * @access  Private
 */
router.post('/setup/email', authenticate, twoFactorController.setupEmail);

/**
 * @route   POST /api/auth/2fa/verify
 * @desc    Verify code and enable 2FA
 * @access  Private
 */
router.post('/verify', authenticate, twoFactorController.verifyAndEnable);

/**
 * @route   POST /api/auth/2fa/resend-email
 * @desc    Resend email verification code
 * @access  Private
 */
router.post('/resend-email', authenticate, twoFactorController.resendEmailCode);

/**
 * @route   POST /api/auth/2fa/disable
 * @desc    Disable 2FA
 * @access  Private
 */
router.post('/disable', authenticate, twoFactorController.disable);

/**
 * @route   POST /api/auth/2fa/backup-codes
 * @desc    Regenerate backup codes
 * @access  Private
 */
router.post('/backup-codes', authenticate, twoFactorController.regenerateBackupCodes);

/**
 * @route   POST /api/auth/2fa/verify-login
 * @desc    Verify 2FA code during login
 * @access  Public (requires userId)
 */
router.post('/verify-login', twoFactorController.verifyLogin);

/**
 * @route   POST /api/auth/2fa/send-login-code
 * @desc    Send 2FA code via email for login
 * @access  Public (requires userId)
 */
router.post('/send-login-code', twoFactorController.sendLoginCode);

/**
 * @route   POST /api/auth/2fa/complete-login
 * @desc    Complete login after 2FA verification
 * @access  Public (requires userId and code)
 */
router.post('/complete-login', twoFactorController.completeLogin);

module.exports = router;
