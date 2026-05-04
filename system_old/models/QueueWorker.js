const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const QueueWorker = sequelize.define('QueueWorker', {
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
    user_id: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'users',
            key: 'id',
        },
        comment: 'อาจารย์หรือ TA',
    },
    accept_grading: {
        type: DataTypes.BOOLEAN,
        defaultValue: true,
        comment: 'รับตรวจงาน',
    },
    accept_help: {
        type: DataTypes.BOOLEAN,
        defaultValue: true,
        comment: 'รับแก้ไขปัญหา',
    },
    status: {
        type: DataTypes.ENUM('online', 'busy', 'offline'),
        defaultValue: 'offline',
        comment: 'online=พร้อมรับงาน, busy=กำลังทำงาน, offline=ออฟไลน์',
    },
    current_booking_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        comment: 'งานที่กำลังทำอยู่',
    },
    total_grading_completed: {
        type: DataTypes.INTEGER,
        defaultValue: 0,
        comment: 'จำนวนตรวจงานเสร็จ',
    },
    total_help_completed: {
        type: DataTypes.INTEGER,
        defaultValue: 0,
        comment: 'จำนวนช่วยเหลือเสร็จ',
    },
    last_active_at: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'เวลาที่ active ล่าสุด',
    },
}, {
    tableName: 'queue_workers',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
    indexes: [
        { 
            unique: true,
            fields: ['queue_session_id', 'user_id'],
            name: 'unique_worker',
        },
        { fields: ['queue_session_id'] },
        { fields: ['user_id'] },
        { fields: ['status'] },
    ],
});

module.exports = QueueWorker;
