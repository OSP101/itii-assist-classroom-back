const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const Assignment = sequelize.define('Assignment', {
    id: {
        type: DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    course_id: {
        type: DataTypes.STRING(21), // nanoid format
        allowNull: false,
        references: {
            model: 'courses',
            key: 'id',
        },
    },
    name: {
        type: DataTypes.STRING(255),
        allowNull: false,
    },
    description: {
        type: DataTypes.TEXT,
        allowNull: true,
    },
    assignment_type: {
        type: DataTypes.ENUM('individual', 'permanent_group', 'weekly_group', 'assignment'),
        allowNull: false,
        defaultValue: 'individual',
        field: 'assignment_type',
        comment: 'individual=ปฏิบัติการเดี่ยว(Lab), permanent_group=กลุ่มถาวร, weekly_group=กลุ่มรายสัปดาห์, assignment=การบ้าน',
    },
    week_number: {
        type: DataTypes.INTEGER,
        allowNull: true,
        comment: 'สำหรับงานกลุ่มประจำสัปดาห์',
    },
    linked_attendance_session_id: {
        type: DataTypes.INTEGER,
        allowNull: true,
        references: {
            model: 'attendance_sessions',
            key: 'id',
        },
        comment: 'Legacy field - ถ้า set ค่านี้ จะตรวจสอบว่านักศึกษามาเรียนหรือไม่ก่อนลงคะแนน (ใช้ตาราง junction แทนแล้ว)',
    },
    attendance_condition: {
        type: DataTypes.ENUM('and', 'or'),
        allowNull: true,
        defaultValue: 'or',
        comment: 'and = ต้องเช็คชื่อทุกรอบ, or = เช็คชื่ออย่างน้อย 1 รอบ',
    },
    max_score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: true,
        defaultValue: 10,
    },
    due_date: {
        type: DataTypes.DATE,
        allowNull: true,
    },
    order_index: {
        type: DataTypes.INTEGER,
        allowNull: true,
        defaultValue: 0,
    },
    is_active: {
        type: DataTypes.BOOLEAN,
        allowNull: true,
        defaultValue: true,
    },
    is_score_visible: {
        type: DataTypes.BOOLEAN,
        allowNull: true,
        defaultValue: true,
        comment: 'Whether students can see their scores for this assignment',
    },
    created_by: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'users',
            key: 'id',
        },
    },
}, {
    tableName: 'assignments',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = Assignment;
