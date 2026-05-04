const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');
const User = require('./User');

const Feedback = sequelize.define('Feedback', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: true,
    references: {
      model: User,
      key: 'id',
    },
  },
  type: {
    type: DataTypes.ENUM('bug', 'feature', 'improvement', 'other'),
    allowNull: false,
    defaultValue: 'other',
    comment: 'bug=รายงานข้อผิดพลาด, feature=ขอฟีเจอร์ใหม่, improvement=ข้อเสนอแนะ, other=อื่นๆ',
  },
  title: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  description: {
    type: DataTypes.TEXT,
    allowNull: false,
  },
  attachments: {
    type: DataTypes.JSON,
    allowNull: true,
    defaultValue: [],
    comment: 'Array of file URLs (images/videos)',
  },
  status: {
    type: DataTypes.ENUM('pending', 'reviewing', 'resolved', 'rejected'),
    allowNull: false,
    defaultValue: 'pending',
  },
  priority: {
    type: DataTypes.ENUM('low', 'medium', 'high', 'critical'),
    allowNull: false,
    defaultValue: 'medium',
  },
  admin_notes: {
    type: DataTypes.TEXT,
    allowNull: true,
    comment: 'Notes from admin/developer',
  },
  resolved_at: {
    type: DataTypes.DATE,
    allowNull: true,
  },
  resolved_by: {
    type: DataTypes.BIGINT,
    allowNull: true,
    references: {
      model: User,
      key: 'id',
    },
  },
  contact_email: {
    type: DataTypes.STRING(255),
    allowNull: true,
    comment: 'Email for anonymous feedback',
  },
}, {
  tableName: 'feedbacks',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
});

// Associations are defined in models/index.js

module.exports = Feedback;
