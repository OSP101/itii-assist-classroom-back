const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const CourseTA = sequelize.define('CourseTA', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  course_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
}, {
  tableName: 'course_tas',
  timestamps: true,
  createdAt: 'assigned_at',
  updatedAt: false,
});

module.exports = CourseTA;
