const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const CourseSection = sequelize.define('CourseSection', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  course_id: {
    type: DataTypes.STRING(21), // nanoid format
    allowNull: false,
  },
  section_no: {
    type: DataTypes.STRING(50),
    allowNull: false,
  },
  note: {
    type: DataTypes.STRING(255),
    allowNull: true,
  },
}, {
  tableName: 'course_sections',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: false,
  indexes: [
    {
      unique: true,
      fields: ['course_id', 'section_no'],
    },
  ],
});

module.exports = CourseSection;
