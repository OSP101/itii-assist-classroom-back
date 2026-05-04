const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const QueueDeskStatus = sequelize.define('QueueDeskStatus', {
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
    desk_id: {
        type: DataTypes.STRING(21),
        allowNull: false,
        references: {
            model: 'desks',
            key: 'id',
        },
    },
    grading_status: {
        type: DataTypes.ENUM('not_started', 'waiting', 'in_progress', 'completed'),
        defaultValue: 'not_started',
        comment: 'not_started=ยังไม่จอง, waiting=รอตรวจ, in_progress=กำลังตรวจ, completed=ตรวจแล้ว',
    },
    grading_booking_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        comment: 'Booking ID ปัจจุบันของ grading',
    },
    help_status: {
        type: DataTypes.ENUM('none', 'waiting', 'in_progress'),
        defaultValue: 'none',
        comment: 'none=ไม่มี, waiting=รอช่วยเหลือ, in_progress=กำลังช่วย',
    },
    help_booking_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        comment: 'Booking ID ปัจจุบันของ help',
    },
}, {
    tableName: 'queue_desk_status',
    timestamps: true,
    createdAt: false,
    updatedAt: 'updated_at',
    indexes: [
        {
            unique: true,
            fields: ['queue_session_id', 'desk_id'],
            name: 'unique_desk_session',
        },
        { fields: ['queue_session_id'] },
        { fields: ['grading_status'] },
        { fields: ['help_status'] },
    ],
});

module.exports = QueueDeskStatus;
