const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const CourseActivityLog = sequelize.define('CourseActivityLog', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  course_id: {
    type: DataTypes.STRING(100),
    allowNull: false,
    comment: 'รหัสวิชาที่เกิดกิจกรรม',
  },
  actor_user_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
    comment: 'ผู้กระทำ (user id)',
  },
  action: {
    type: DataTypes.STRING(50),
    allowNull: false,
    comment: 'ประเภทการกระทำ',
  },
  category: {
    type: DataTypes.STRING(30),
    allowNull: false,
    defaultValue: 'general',
    comment: 'หมวดหมู่: course, people, assignment, score, attendance, queue',
  },
  target_type: {
    type: DataTypes.STRING(50),
    allowNull: true,
    comment: 'ประเภทเป้าหมาย เช่น student, assignment, score',
  },
  target_id: {
    type: DataTypes.STRING(100),
    allowNull: true,
    comment: 'ID ของเป้าหมาย',
  },
  target_name: {
    type: DataTypes.STRING(255),
    allowNull: true,
    comment: 'ชื่อเป้าหมาย เช่น ชื่องาน, ชื่อนักศึกษา',
  },
  detail: {
    type: DataTypes.JSON,
    allowNull: true,
    comment: 'รายละเอียดเพิ่มเติม',
  },
}, {
  tableName: 'course_activity_logs',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: false, // ไม่ต้องมี updated_at เพราะ log ไม่ถูกแก้ไข
  indexes: [
    { fields: ['course_id'] },
    { fields: ['actor_user_id'] },
    { fields: ['action'] },
    { fields: ['category'] },
    { fields: ['created_at'] },
    { fields: ['course_id', 'action'] },
    { fields: ['course_id', 'created_at'] },
  ],
});

module.exports = CourseActivityLog;
