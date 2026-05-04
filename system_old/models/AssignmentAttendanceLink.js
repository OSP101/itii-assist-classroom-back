const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

/**
 * Junction table for many-to-many relationship between Assignment and AttendanceSession
 * งานหนึ่งงานสามารถผูกกับหลายการเช็คชื่อได้
 */
const AssignmentAttendanceLink = sequelize.define('AssignmentAttendanceLink', {
    id: {
        type: DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    assignment_id: {
        type: DataTypes.INTEGER,
        allowNull: false,
        references: {
            model: 'assignments',
            key: 'id',
        },
    },
    attendance_session_id: {
        type: DataTypes.INTEGER,
        allowNull: false,
        references: {
            model: 'attendance_sessions',
            key: 'id',
        },
    },
}, {
    tableName: 'assignment_attendance_links',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: false,
    indexes: [
        {
            unique: true,
            fields: ['assignment_id', 'attendance_session_id'],
        },
    ],
});

module.exports = AssignmentAttendanceLink;
