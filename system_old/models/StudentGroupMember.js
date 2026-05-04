const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const StudentGroupMember = sequelize.define('StudentGroupMember', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  group_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
  student_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
  joined_at: {
    type: DataTypes.DATE,
    defaultValue: DataTypes.NOW,
  },
}, {
  tableName: 'student_group_members',
  timestamps: false,
});

module.exports = StudentGroupMember;
