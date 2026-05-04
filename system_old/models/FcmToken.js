const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const FcmToken = sequelize.define('FcmToken', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  fcm_token: {
    type: DataTypes.STRING(500),
    allowNull: false,
    unique: true,
  },
  user_type: {
    type: DataTypes.ENUM('worker', 'student'),
    allowNull: false,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: true,
    comment: 'For authenticated users (workers)',
  },
  student_id: {
    type: DataTypes.STRING(20),
    allowNull: true,
    comment: 'For students (รหัสนักศึกษา)',
  },
  booking_id: {
    type: DataTypes.BIGINT,
    allowNull: true,
    comment: 'For students - linked to their booking',
  },
  session_id: {
    type: DataTypes.STRING(21),
    allowNull: true,
    comment: 'For workers - linked to queue session (nanoid)',
  },
  device_info: {
    type: DataTypes.JSON,
    allowNull: true,
    comment: 'Browser/device information',
  },
  is_active: {
    type: DataTypes.BOOLEAN,
    defaultValue: true,
  },
  last_used_at: {
    type: DataTypes.DATE,
    allowNull: true,
  },
}, {
  tableName: 'fcm_tokens',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
});

module.exports = FcmToken;
