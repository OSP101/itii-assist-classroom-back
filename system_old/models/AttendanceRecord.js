const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const AttendanceRecord = sequelize.define('AttendanceRecord', {
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
    student_id: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'students',
            key: 'id',
        },
    },
    check_in_time: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'เวลาที่เช็คชื่อ',
    },
    status: {
        type: DataTypes.ENUM('present', 'late', 'leave', 'absent'),
        defaultValue: 'absent',
        comment: 'มา, สาย, ลา, ขาด',
    },
    google_email: {
        type: DataTypes.STRING(255),
        allowNull: true,
        comment: 'อีเมล Google ที่ใช้ยืนยันตัวตน',
    },
    google_id: {
        type: DataTypes.STRING(255),
        allowNull: true,
        comment: 'Google ID ที่ใช้ยืนยันตัวตน',
    },
    pin_verified: {
        type: DataTypes.BOOLEAN,
        defaultValue: false,
        comment: 'ยืนยัน PIN แล้ว',
    },
    location_verified: {
        type: DataTypes.BOOLEAN,
        defaultValue: false,
        comment: 'ยืนยันตำแหน่งแล้ว',
    },
    location_lat: {
        type: DataTypes.DECIMAL(10, 7),
        allowNull: true,
        comment: 'ละติจูดของนักศึกษา',
    },
    location_lng: {
        type: DataTypes.DECIMAL(10, 7),
        allowNull: true,
        comment: 'ลองจิจูดของนักศึกษา',
    },
    distance_meters: {
        type: DataTypes.INTEGER,
        allowNull: true,
        comment: 'ระยะห่างจากจุดเช็คชื่อ (เมตร)',
    },
    note: {
        type: DataTypes.TEXT,
        allowNull: true,
        comment: 'หมายเหตุ',
    },
    updated_by: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'users',
            key: 'id',
        },
        comment: 'ผู้แก้ไขสถานะ (อาจารย์/TA)',
    },
}, {
    tableName: 'attendance_records',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = AttendanceRecord;
