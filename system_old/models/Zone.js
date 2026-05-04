const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');
const { nanoid } = require('nanoid');

const Zone = sequelize.define('Zone', {
  id: {
    type: DataTypes.STRING(21),
    primaryKey: true,
    defaultValue: () => nanoid(),
  },
  classroom_id: {
    type: DataTypes.STRING(21),
    allowNull: false,
  },
  name: {
    type: DataTypes.STRING(100),
    allowNull: false,
    comment: 'ชื่อโซน เช่น โซน A, แถวหน้า',
  },
  x: {
    type: DataTypes.INTEGER,
    allowNull: false,
    defaultValue: 0,
    comment: 'ตำแหน่ง X บน canvas',
  },
  y: {
    type: DataTypes.INTEGER,
    allowNull: false,
    defaultValue: 0,
    comment: 'ตำแหน่ง Y บน canvas',
  },
  width: {
    type: DataTypes.INTEGER,
    allowNull: false,
    defaultValue: 400,
    comment: 'ความกว้างโซน (px)',
  },
  height: {
    type: DataTypes.INTEGER,
    allowNull: false,
    defaultValue: 300,
    comment: 'ความสูงโซน (px)',
  },
  color: {
    type: DataTypes.STRING(30),
    allowNull: false,
    defaultValue: 'rgba(99,102,241,0.15)',
    comment: 'สีโซน',
  },
}, {
  tableName: 'zones',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
  indexes: [
    { fields: ['classroom_id'] },
  ],
});

module.exports = Zone;
