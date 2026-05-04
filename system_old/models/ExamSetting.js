const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const ExamSetting = sequelize.define('ExamSetting', {
    id: {
        type: DataTypes.INTEGER,
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
        comment: 'รหัสรายวิชา',
    },
    exam_type: {
        type: DataTypes.ENUM('midterm', 'final'),
        allowNull: false,
        comment: 'ประเภทการสอบ: midterm=กลางภาค, final=ปลายภาค',
    },
    component: {
        type: DataTypes.ENUM('lab', 'lecture'),
        allowNull: false,
        comment: 'องค์ประกอบ: lab=ปฏิบัติการ, lecture=บรรยาย',
    },
    max_score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: false,
        defaultValue: 0,
        comment: 'คะแนนเต็ม',
    },
    is_visible: {
        type: DataTypes.BOOLEAN,
        allowNull: false,
        defaultValue: false,
        comment: 'แสดงผลให้นักศึกษาเห็นหรือไม่',
    },
    is_active: {
        type: DataTypes.BOOLEAN,
        allowNull: false,
        defaultValue: true,
        comment: 'เปิดใช้งานหรือไม่',
    },
}, {
    tableName: 'exam_settings',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = ExamSetting;