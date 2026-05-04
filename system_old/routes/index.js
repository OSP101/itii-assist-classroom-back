const express = require('express');
const router = express.Router();

// Import route modules
const authRoutes = require('./auth.routes');
const twoFactorRoutes = require('./twoFactor.routes');
const systemRoutes = require('./system.routes');
const systemLogRoutes = require('./systemLog.routes');
const userRoutes = require('./user.routes');
const studentRoutes = require('./student.routes');
const courseRoutes = require('./course.routes');
const classroomRoutes = require('./classroom.routes');
const feedbackRoutes = require('./feedback.routes');
const teamRoutes = require('./team.routes');
const assignmentRoutes = require('./assignment.routes');
const scoreRoutes = require('./score.routes');
const scoreEditRequestRoutes = require('./scoreEditRequest.routes');
const attendanceRoutes = require('./attendance.routes');
const bonusScoreRoutes = require('./bonusScore.routes');
const queueRoutes = require('./queue.routes');
const queuePublicRoutes = require('./queuePublic.routes');
const notificationRoutes = require('./notification.routes');
const courseActivityLogRoutes = require('./courseActivityLog.routes');
const examScoreRoutes = require('./examScore.routes');
const oauthRoutes = require('./oauth.routes');
const monitoringRoutes = require('./monitoring.routes');

// Mount routes
router.use('/auth', authRoutes);
router.use('/auth/2fa', twoFactorRoutes);
router.use('/oauth', oauthRoutes);
router.use('/system', systemRoutes);
router.use('/logs', systemLogRoutes);
router.use('/users', userRoutes);
router.use('/students', studentRoutes);
router.use('/courses', courseRoutes);
router.use('/courses/:id/teams', teamRoutes);
router.use('/courses/:courseId/queue', queueRoutes);
router.use('/courses/:courseId/activity-logs', courseActivityLogRoutes);
router.use('/courses', examScoreRoutes);
router.use('/queue', queuePublicRoutes);  // Public queue routes (no courseId needed)
router.use('/notifications', notificationRoutes);
router.use('/classrooms', classroomRoutes);
router.use('/feedback', feedbackRoutes);
router.use('/assignments', assignmentRoutes);
router.use('/scores', scoreRoutes);
router.use('/score-edit-requests', scoreEditRequestRoutes);
router.use('/attendance', attendanceRoutes);
router.use('/bonus-scores', bonusScoreRoutes);

// Monitoring & metrics routes (Prometheus scrape + admin API)
router.use('/metrics', monitoringRoutes);
router.use('/monitoring', monitoringRoutes);

// Health check endpoint
router.get('/health', (req, res) => {
  res.json({
    success: true,
    message: 'API is running',
    timestamp: new Date().toISOString(),
  });
});

module.exports = router;
