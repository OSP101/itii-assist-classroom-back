const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');
const { nanoid } = require('nanoid');

const QueueSession = sequelize.define('QueueSession', {
    id: {
        type: DataTypes.STRING(21),
        primaryKey: true,
        defaultValue: () => nanoid(),
    },
    course_id: {
        type: DataTypes.STRING(21),
        allowNull: false,
        references: {
            model: 'courses',
            key: 'id',
        },
    },
    classroom_id: {
        type: DataTypes.STRING(21),
        allowNull: false,
        references: {
            model: 'classrooms',
            key: 'id',
        },
    },
    title: {
        type: DataTypes.STRING(255),
        allowNull: false,
        comment: 'ชื่อการจองคิว เช่น Lab01 - ตรวจงาน',
    },
    description: {
        type: DataTypes.TEXT,
        allowNull: true,
        comment: 'รายละเอียดเพิ่มเติม',
    },
    pin_code: {
        type: DataTypes.STRING(10),
        allowNull: false,
        comment: 'รหัส PIN 6 หลัก',
    },
    linked_assignment_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'assignments',
            key: 'id',
        },
        comment: 'Assignment ที่ลิงก์สำหรับลงคะแนน',
    },
    require_attendance: {
        type: DataTypes.BOOLEAN,
        defaultValue: false,
        comment: 'ต้องเช็คชื่อก่อนจึงจะจองได้',
    },
    linked_attendance_session_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'attendance_sessions',
            key: 'id',
        },
        comment: 'Session เช็คชื่อที่ลิงก์',
    },
    status: {
        type: DataTypes.ENUM('draft', 'active', 'paused', 'closed'),
        defaultValue: 'draft',
        comment: 'draft=ยังไม่เปิด, active=กำลังรับจอง, paused=หยุดชั่วคราว, closed=ปิดแล้ว',
    },
    start_time: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'เวลาเริ่มรับจอง',
    },
    end_time: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'เวลาสิ้นสุดรับจอง',
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
    tableName: 'queue_sessions',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
    indexes: [
        { fields: ['course_id'] },
        { fields: ['classroom_id'] },
        { fields: ['status'] },
        { fields: ['pin_code'] },
    ],
});

// Generate random PIN code (6 digits)
QueueSession.generatePIN = () => {
    return Math.floor(100000 + Math.random() * 900000).toString();
};

module.exports = QueueSession;
