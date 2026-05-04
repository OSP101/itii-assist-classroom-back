const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const AssignmentSubItem = sequelize.define('AssignmentSubItem', {
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
    name: {
        type: DataTypes.STRING(255),
        allowNull: false,
    },
    max_score: {
        type: DataTypes.DECIMAL(10, 2),
        allowNull: true,
        defaultValue: 10,
    },
    order_index: {
        type: DataTypes.INTEGER,
        allowNull: true,
        defaultValue: 0,
    },
}, {
    tableName: 'assignment_sub_items',
    timestamps: true,
    createdAt: 'created_at',
    updatedAt: 'updated_at',
});

module.exports = AssignmentSubItem;
