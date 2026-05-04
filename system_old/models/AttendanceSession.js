const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const AttendanceSession = sequelize.define('AttendanceSession', {
    id: {
        type: DataTypes.BIGINT,
        primaryKey: true,
        autoIncrement: true,
    },
    course_id: {
        type: DataTypes.STRING(21),
        allowNull: false,
        references: {
            model: 'courses',
            key: 'id',
        },
    },
    course_section_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'course_sections',
            key: 'id',
        },
        comment: 'null = ทุก section',
    },
    title: {
        type: DataTypes.STRING(255),
        allowNull: false,
        comment: 'ชื่อการเช็คชื่อ เช่น Lab01, Lecture Week 1',
    },
    pin_code: {
        type: DataTypes.STRING(10),
        allowNull: true,
        comment: 'รหัส PIN 6 หลัก',
    },
    session_type: {
        type: DataTypes.ENUM('lecture', 'lab', 'online'),
        defaultValue: 'lecture',
        comment: 'รูปแบบการเรียน',
    },
    check_location: {
        type: DataTypes.BOOLEAN,
        defaultValue: false,
        comment: 'เปิดใช้การตรวจสอบตำแหน่ง',
    },
    location_lat: {
        type: DataTypes.DECIMAL(10, 7),
        allowNull: true,
        comment: 'ละติจูดศูนย์กลาง',
    },
    location_lng: {
        type: DataTypes.DECIMAL(10, 7),
        allowNull: true,
        comment: 'ลองจิจูดศูนย์กลาง',
    },
    radius_meters: {
        type: DataTypes.INTEGER,
        defaultValue: 50,
        comment: 'รัศมีที่อนุญาต (เมตร)',
    },
    start_time: {
        type: DataTypes.DATE,
        allowNull: false,
    },
    end_time: {
        type: DataTypes.DATE,
        allowNull: false,
    },
    late_threshold_minutes: {
        type: DataTypes.INTEGER,
        defaultValue: 15,
        comment: 'เวลาสาย (นาที) หลังจากเวลาเริ่ม',
    },
    status: {
        type: DataTypes.ENUM('draft', 'active', 'closed'),
        defaultValue: 'draft',
        comment: 'draft=ยังไม่เปิด, active=กำลังเช็คชื่อ, closed=ปิดแล้ว',
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
    tableName: 'attendance_sessions',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

// Generate random PIN code
AttendanceSession.generatePIN = () => {
    return Math.floor(100000 + Math.random() * 900000).toString();
};

module.exports = AttendanceSession;
