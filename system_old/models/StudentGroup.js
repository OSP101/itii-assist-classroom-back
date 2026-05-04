const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const StudentGroup = sequelize.define('StudentGroup', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  course_id: {
    type: DataTypes.STRING(21),
    allowNull: false,
  },
  name: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  group_type: {
    type: DataTypes.ENUM('permanent', 'temporary'),
    defaultValue: 'permanent',
  },
  week_number: {
    type: DataTypes.INTEGER,
    allowNull: true,
    comment: 'For temporary/weekly groups, specify the week number',
  },
  created_at: {
    type: DataTypes.DATE,
    defaultValue: DataTypes.NOW,
  },
}, {
  tableName: 'student_groups',
  timestamps: false,
});

module.exports = StudentGroup;
