const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const NotificationLog = sequelize.define('NotificationLog', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  fcm_token_id: {
    type: DataTypes.BIGINT,
    allowNull: true,
  },
  notification_type: {
    type: DataTypes.ENUM('new-task', 'queue-ready', 'booking-completed', 'session-closed', 'other'),
    allowNull: false,
  },
  title: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  body: {
    type: DataTypes.TEXT,
    allowNull: true,
  },
  data: {
    type: DataTypes.JSON,
    allowNull: true,
  },
  status: {
    type: DataTypes.ENUM('pending', 'sent', 'failed', 'delivered'),
    defaultValue: 'pending',
  },
  error_message: {
    type: DataTypes.TEXT,
    allowNull: true,
  },
  sent_at: {
    type: DataTypes.DATE,
    allowNull: true,
  },
  delivered_at: {
    type: DataTypes.DATE,
    allowNull: true,
  },
}, {
  tableName: 'notification_logs',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: false,
});

module.exports = NotificationLog;
