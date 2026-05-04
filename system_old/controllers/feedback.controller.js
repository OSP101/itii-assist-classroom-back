const { Feedback, User, SystemLog } = require('../models');
const { Op } = require('sequelize');
const asyncHandler = require('../utils/asyncHandler');
const ApiError = require('../utils/ApiError');

/**
 * Create feedback
 * POST /api/feedback
 */
const createFeedback = asyncHandler(async (req, res) => {
  const { type, title, description, attachments, contact_email } = req.body;
  const user_id = req.user?.id || null;

  const feedback = await Feedback.create({
    user_id,
    type,
    title,
    description,
    attachments: attachments || [],
    contact_email: contact_email || null,
    status: 'pending',
    priority: 'medium',
  });

  // Log the action
  await SystemLog.create({
    user_id,
    action: 'CREATE_FEEDBACK',
    entity_type: 'feedback',
    entity_id: feedback.id,
    details: { type, title },
    ip_address: req.ip,
  });

  res.status(201).json({
    success: true,
    message: 'ส่ง Feedback สำเร็จ',
    data: feedback,
  });
});

/**
 * Get all feedbacks (Admin only)
 * GET /api/feedback
 */
const getFeedbacks = asyncHandler(async (req, res) => {
  const {
    page = 1,
    limit = 10,
    search = '',
    type,
    status,
    priority,
    sort_by = 'created_at',
    sort_order = 'DESC',
  } = req.query;

  const offset = (parseInt(page) - 1) * parseInt(limit);

  // Build where clause
  const where = {};

  if (search) {
    where[Op.or] = [
      { title: { [Op.like]: `%${search}%` } },
      { description: { [Op.like]: `%${search}%` } },
    ];
  }

  if (type && type !== 'all') {
    where.type = type;
  }

  if (status && status !== 'all') {
    where.status = status;
  }

  if (priority && priority !== 'all') {
    where.priority = priority;
  }

  const { count, rows: feedbacks } = await Feedback.findAndCountAll({
    where,
    include: [
      {
        model: User,
        as: 'user',
        attributes: ['id', 'username', 'full_name', 'email', 'role', 'avatar'],
      },
      {
        model: User,
        as: 'resolver',
        attributes: ['id', 'username', 'full_name', 'avatar'],
      },
    ],
    order: [[sort_by, sort_order.toUpperCase()]],
    limit: parseInt(limit),
    offset,
  });

  res.json({
    success: true,
    data: {
      feedbacks,
      pagination: {
        total: count,
        page: parseInt(page),
        limit: parseInt(limit),
        totalPages: Math.ceil(count / parseInt(limit)),
      },
    },
  });
});

/**
 * Get feedback by ID
 * GET /api/feedback/:id
 */
const getFeedbackById = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const feedback = await Feedback.findByPk(id, {
    include: [
      {
        model: User,
        as: 'user',
        attributes: ['id', 'username', 'full_name', 'email', 'role', 'avatar'],
      },
      {
        model: User,
        as: 'resolver',
        attributes: ['id', 'username', 'full_name', 'avatar'],
      },
    ],
  });

  if (!feedback) {
    throw new ApiError(404, 'ไม่พบ Feedback');
  }

  res.json({
    success: true,
    data: feedback,
  });
});

/**
 * Update feedback status (Admin only)
 * PUT /api/feedback/:id
 */
const updateFeedback = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { status, priority, admin_notes } = req.body;
  const admin_id = req.user.id;

  const feedback = await Feedback.findByPk(id);

  if (!feedback) {
    throw new ApiError(404, 'ไม่พบ Feedback');
  }

  const updateData = {};

  if (status) {
    updateData.status = status;
    if (status === 'resolved' || status === 'rejected') {
      updateData.resolved_at = new Date();
      updateData.resolved_by = admin_id;
    }
  }

  if (priority) {
    updateData.priority = priority;
  }

  if (admin_notes !== undefined) {
    updateData.admin_notes = admin_notes;
  }

  await feedback.update(updateData);

  // Log the action
  await SystemLog.create({
    user_id: admin_id,
    action: 'UPDATE_FEEDBACK',
    entity_type: 'feedback',
    entity_id: feedback.id,
    details: { status, priority },
    ip_address: req.ip,
  });

  // Reload with associations
  await feedback.reload({
    include: [
      {
        model: User,
        as: 'user',
        attributes: ['id', 'username', 'full_name', 'email', 'role'],
      },
      {
        model: User,
        as: 'resolver',
        attributes: ['id', 'username', 'full_name'],
      },
    ],
  });

  res.json({
    success: true,
    message: 'อัปเดต Feedback สำเร็จ',
    data: feedback,
  });
});

/**
 * Delete feedback (Admin only)
 * DELETE /api/feedback/:id
 */
const deleteFeedback = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const admin_id = req.user.id;

  const feedback = await Feedback.findByPk(id);

  if (!feedback) {
    throw new ApiError(404, 'ไม่พบ Feedback');
  }

  await feedback.destroy();

  // Log the action
  await SystemLog.create({
    user_id: admin_id,
    action: 'DELETE_FEEDBACK',
    entity_type: 'feedback',
    entity_id: id,
    details: { title: feedback.title },
    ip_address: req.ip,
  });

  res.json({
    success: true,
    message: 'ลบ Feedback สำเร็จ',
  });
});

/**
 * Get feedback stats (Admin only)
 * GET /api/feedback/stats
 */
const getFeedbackStats = asyncHandler(async (req, res) => {
  const [total, pending, reviewing, resolved, rejected] = await Promise.all([
    Feedback.count(),
    Feedback.count({ where: { status: 'pending' } }),
    Feedback.count({ where: { status: 'reviewing' } }),
    Feedback.count({ where: { status: 'resolved' } }),
    Feedback.count({ where: { status: 'rejected' } }),
  ]);

  const [bugs, features, improvements, others] = await Promise.all([
    Feedback.count({ where: { type: 'bug' } }),
    Feedback.count({ where: { type: 'feature' } }),
    Feedback.count({ where: { type: 'improvement' } }),
    Feedback.count({ where: { type: 'other' } }),
  ]);

  res.json({
    success: true,
    data: {
      total,
      byStatus: { pending, reviewing, resolved, rejected },
      byType: { bugs, features, improvements, others },
    },
  });
});

/**
 * Get my feedbacks (for logged-in user)
 * GET /api/feedback/my
 */
const getMyFeedbacks = asyncHandler(async (req, res) => {
  const user_id = req.user.id;
  const { page = 1, limit = 10 } = req.query;
  const offset = (parseInt(page) - 1) * parseInt(limit);

  const { count, rows: feedbacks } = await Feedback.findAndCountAll({
    where: { user_id },
    order: [['created_at', 'DESC']],
    limit: parseInt(limit),
    offset,
  });

  res.json({
    success: true,
    data: {
      feedbacks,
      pagination: {
        total: count,
        page: parseInt(page),
        limit: parseInt(limit),
        totalPages: Math.ceil(count / parseInt(limit)),
      },
    },
  });
});

module.exports = {
  createFeedback,
  getFeedbacks,
  getFeedbackById,
  updateFeedback,
  deleteFeedback,
  getFeedbackStats,
  getMyFeedbacks,
};
