/**
 * AttendanceSessionSection Model (Junction Table)
 * Many-to-many relationship between AttendanceSession and CourseSection
 */

const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const AttendanceSessionSection = sequelize.define('AttendanceSessionSection', {
    id: {
        type: DataTypes.BIGINT,
        primaryKey: true,
        autoIncrement: true,
    },
    attendance_session_id: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'attendance_sessions',
            key: 'id',
        },
    },
    course_section_id: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'course_sections',
            key: 'id',
        },
    },
}, {
    tableName: 'attendance_session_sections',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: false, // No updated_at for junction table
    indexes: [
        {
            unique: true,
            fields: ['attendance_session_id', 'course_section_id'],
        },
    ],
});

module.exports = AttendanceSessionSection;
