const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const QueueBooking = sequelize.define('QueueBooking', {
    id: {
        type: DataTypes.BIGINT,
        primaryKey: true,
        autoIncrement: true,
    },
    queue_session_id: {
        type: DataTypes.STRING(21),
        allowNull: false,
        references: {
            model: 'queue_sessions',
            key: 'id',
        },
    },
    student_id: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'students',
            key: 'id',
        },
        comment: 'นักศึกษาที่จอง',
    },
    desk_id: {
        type: DataTypes.STRING(21),
        allowNull: false,
        references: {
            model: 'desks',
            key: 'id',
        },
    },
    desk_number: {
        type: DataTypes.INTEGER,
        allowNull: false,
        comment: 'เลขโต๊ะ (denormalized)',
    },
    booking_type: {
        type: DataTypes.ENUM('grading', 'help'),
        allowNull: false,
        comment: 'grading=ตรวจงาน, help=ขอความช่วยเหลือ',
    },
    queue_number: {
        type: DataTypes.INTEGER,
        allowNull: false,
        comment: 'หมายเลขคิวในรอบนี้',
    },
    note: {
        type: DataTypes.TEXT,
        allowNull: true,
        comment: 'หมายเหตุเพิ่มเติม เช่น ปัญหาที่พบ',
    },
    status: {
        type: DataTypes.ENUM('waiting', 'in_progress', 'completed', 'cancelled', 'no_show'),
        defaultValue: 'waiting',
        comment: 'waiting=รอคิว, in_progress=กำลังตรวจ, completed=เสร็จ, cancelled=ยกเลิก, no_show=ไม่มา',
    },
    assigned_worker_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'users',
            key: 'id',
        },
        comment: 'ผู้ที่ได้รับมอบหมายตรวจ',
    },
    assigned_at: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'เวลาที่ได้รับมอบหมาย',
    },
    started_at: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'เวลาเริ่มตรวจ',
    },
    completed_at: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'เวลาตรวจเสร็จ',
    },
    score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: true,
        comment: 'คะแนนที่ได้',
    },
    score_comment: {
        type: DataTypes.TEXT,
        allowNull: true,
        comment: 'ความเห็นเรื่องคะแนน',
    },
    worker_note: {
        type: DataTypes.TEXT,
        allowNull: true,
        comment: 'บันทึกจากผู้ตรวจ',
    },
}, {
    tableName: 'queue_bookings',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
    indexes: [
        { fields: ['queue_session_id'] },
        { fields: ['student_id'] },
        { fields: ['desk_id'] },
        { fields: ['status'] },
        { fields: ['booking_type'] },
        { fields: ['queue_session_id', 'queue_number'] },
    ],
});

module.exports = QueueBooking;
