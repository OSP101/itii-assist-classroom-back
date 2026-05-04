const express = require('express');
const router = express.Router({ mergeParams: true });
const ctrl = require('../controllers/courseActivityLog.controller');
const { authenticate, authorize } = require('../middlewares/auth');

// All routes require authentication + instructor/admin role
router.use(authenticate);
router.use(authorize('admin', 'instructor'));

// Activity logs
router.get('/', ctrl.getActivityLogs);
router.get('/stats', ctrl.getActivityStats);
router.get('/filters', ctrl.getActivityFilters);

// TA statistics
router.get('/ta-stats', ctrl.getTAStats);
router.get('/ta-stats/:userId', ctrl.getTADetail);

module.exports = router;
