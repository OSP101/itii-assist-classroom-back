const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const ExamScore = sequelize.define('ExamScore', {
    id: {
        type: DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    exam_setting_id: {
        type: DataTypes.INTEGER,
        allowNull: false,
        references: {
            model: 'exam_settings',
            key: 'id',
        },
        comment: 'รหัสการตั้งค่าการสอบ',
    },
    student_id: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'students',
            key: 'id',
        },
        comment: 'รหัสนักศึกษา (internal)',
    },
    score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: true,
        comment: 'คะแนนที่ได้',
    },
    comment: {
        type: DataTypes.TEXT,
        allowNull: true,
        comment: 'หมายเหตุ',
    },
    graded_by: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'users',
            key: 'id',
        },
        comment: 'ผู้ให้คะแนน',
    },
    graded_at: {
        type: DataTypes.DATE,
        allowNull: true,
        comment: 'วันเวลาที่ให้คะแนน',
    },
}, {
    tableName: 'exam_scores',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = ExamScore;