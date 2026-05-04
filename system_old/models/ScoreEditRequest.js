const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const ScoreEditRequest = sequelize.define('ScoreEditRequest', {
    id: {
        type: DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    score_id: {
        type: DataTypes.INTEGER,
        allowNull: false,
        references: {
            model: 'scores',
            key: 'id',
        },
    },
    old_score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: true,
    },
    new_score: {
        type: DataTypes.DECIMAL(5, 2),
        allowNull: false,
    },
    reason: {
        type: DataTypes.TEXT,
        allowNull: true,
    },
    images: {
        type: DataTypes.JSON,
        allowNull: true,
        defaultValue: null,
        comment: 'JSON array of image file paths',
    },
    status: {
        type: DataTypes.ENUM('pending', 'approved', 'rejected'),
        allowNull: true,
        defaultValue: 'pending',
    },
    requested_by: {
        type: DataTypes.BIGINT,
        allowNull: false,
        references: {
            model: 'users',
            key: 'id',
        },
    },
    reviewed_by: {
        type: DataTypes.BIGINT,
        allowNull: true,
        references: {
            model: 'users',
            key: 'id',
        },
    },
    reviewed_at: {
        type: DataTypes.DATE,
        allowNull: true,
    },
    review_comment: {
        type: DataTypes.TEXT,
        allowNull: true,
    },
}, {
    tableName: 'score_edit_requests',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = ScoreEditRequest;
