const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const CourseInstructor = sequelize.define('CourseInstructor', {
  id: {
    type: DataTypes.INTEGER,
    primaryKey: true,
    autoIncrement: true,
  },
  course_id: {
    type: DataTypes.STRING(21),
    allowNull: false,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
  is_primary: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: false,
  },
  assigned_at: {
    type: DataTypes.DATE,
    allowNull: false,
    defaultValue: DataTypes.NOW,
  },
}, {
  tableName: 'course_instructors',
  timestamps: false,
  indexes: [
    {
      unique: true,
      fields: ['course_id', 'user_id'],
    },
  ],
});

module.exports = CourseInstructor;
