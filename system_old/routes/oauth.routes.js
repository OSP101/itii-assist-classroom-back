const express = require('express');
const router = express.Router();
const { authenticate, authorize } = require('../middlewares/auth');
const oauthController = require('../controllers/oauth.controller');

// User routes - require authentication
router.get('/linked', authenticate, oauthController.getLinkedAccounts);
router.post('/link', authenticate, oauthController.linkAccount);
router.delete('/unlink/:provider', authenticate, oauthController.unlinkAccount);

// Admin routes
router.get('/admin/user/:userId', authenticate, authorize('admin'), oauthController.getAccountsForUser);
router.delete('/admin/user/:userId/:provider', authenticate, authorize('admin'), oauthController.adminUnlinkAccount);

module.exports = router;
