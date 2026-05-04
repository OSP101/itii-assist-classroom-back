const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const BonusScore = sequelize.define('BonusScore', {
    id: {
        type: DataTypes.BIGINT,
        primaryKey: true,
        autoIncrement: true,
    },
    course_id: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'courses',
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
    score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: false,
        defaultValue: 1.00,
        comment: 'คะแนนพิเศษ (ค่าเริ่มต้น 1 คะแนน)',
    },
    reason: {
        type: DataTypes.STRING(255),
        allowNull: true,
        comment: 'เหตุผลการให้คะแนน เช่น ตอบคำถามในห้องเรียน',
    },
    given_by: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'users',
            key: 'id',
        },
        comment: 'ผู้ให้คะแนน (instructor/ta)',
    },
    given_at: {
        type: DataTypes.DATE,
        allowNull: false,
        defaultValue: DataTypes.NOW,
    },
}, {
    tableName: 'bonus_scores',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = BonusScore;
