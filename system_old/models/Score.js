const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const Score = sequelize.define('Score', {
    id: {
        type: DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    assignment_id: {
        type: DataTypes.INTEGER,
        allowNull: false,
        references: {
            model: 'assignments',
            key: 'id',
        },
    },
    student_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'students',
            key: 'id',
        },
        comment: 'สำหรับงานเดี่ยว',
    },
    group_id: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'student_groups',
            key: 'id',
        },
        comment: 'สำหรับงานกลุ่ม',
    },
    sub_item_id: {
        type: DataTypes.INTEGER,
        allowNull: true,
        references: {
            model: 'assignment_sub_items',
            key: 'id',
        },
        comment: 'สำหรับคะแนนรายข้อย่อย (null = คะแนนรวม)',
    },
    score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: true,
    },
    comment: {
        type: DataTypes.TEXT,
        allowNull: true,
    },
    graded_by: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'users',
            key: 'id',
        },
    },
    graded_at: {
        type: DataTypes.DATE,
        allowNull: true,
    },
    status: {
        type: DataTypes.ENUM('pending', 'graded'),
        allowNull: true,
        defaultValue: 'pending',
    },
}, {
    tableName: 'scores',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = Score;
