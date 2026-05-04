const Joi = require('joi');

const createFeedback = {
  body: Joi.object({
    type: Joi.string().valid('bug', 'feature', 'improvement', 'other').required(),
    title: Joi.string().max(255).required(),
    description: Joi.string().required(),
    attachments: Joi.array().items(Joi.string().uri()).max(5).optional(),
    contact_email: Joi.string().email().allow('', null).optional(),
  }),
};

const updateFeedback = {
  body: Joi.object({
    status: Joi.string().valid('pending', 'reviewing', 'resolved', 'rejected').optional(),
    priority: Joi.string().valid('low', 'medium', 'high', 'critical').optional(),
    admin_notes: Joi.string().allow('', null).optional(),
  }),
};

const getFeedbacks = {
  query: Joi.object({
    page: Joi.number().integer().min(1).default(1),
    limit: Joi.number().integer().min(1).max(100).default(10),
    search: Joi.string().allow('').optional(),
    type: Joi.string().valid('all', 'bug', 'feature', 'improvement', 'other').optional(),
    status: Joi.string().valid('all', 'pending', 'reviewing', 'resolved', 'rejected').optional(),
    priority: Joi.string().valid('all', 'low', 'medium', 'high', 'critical').optional(),
    sort_by: Joi.string().valid('created_at', 'updated_at', 'priority', 'status').default('created_at'),
    sort_order: Joi.string().valid('ASC', 'DESC', 'asc', 'desc').default('DESC'),
  }),
};

module.exports = {
  createFeedback,
  updateFeedback,
  getFeedbacks,
};
