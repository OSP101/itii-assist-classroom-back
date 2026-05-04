const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');
const { nanoid } = require('nanoid');

const Course = sequelize.define('Course', {
  id: {
    type: DataTypes.STRING(21),
    primaryKey: true,
    defaultValue: () => nanoid(),
  },
  code: {
    type: DataTypes.STRING(100),
    allowNull: false,
  },
  name: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  year: {
    type: DataTypes.SMALLINT,
    allowNull: false,
  },
  semester: {
    type: DataTypes.TINYINT,
    allowNull: false,
  },
  instructor_id: {
    type: DataTypes.BIGINT,
    allowNull: true,
  },
  description: {
    type: DataTypes.TEXT,
    allowNull: true,
  },
  image: {
    type: DataTypes.TEXT('long'),
    allowNull: true,
    comment: 'Course cover image URL or base64',
  },
  is_active: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: true,
  },
  attention_threshold: {
    type: DataTypes.TINYINT.UNSIGNED,
    allowNull: false,
    defaultValue: 60,
    comment: 'Percentage threshold for low performer alert (default 60%)',
  },
}, {
  tableName: 'courses',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
  // Note: Removed unique constraint on (code, year, semester)
  // Duplicate validation is handled by application logic in course.controller.js
  // Only active courses with the same code+year+semester will be rejected
});

module.exports = Course;
