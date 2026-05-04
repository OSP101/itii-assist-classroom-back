const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');
const { nanoid } = require('nanoid');

const Desk = sequelize.define('Desk', {
  id: {
    type: DataTypes.STRING(21),
    primaryKey: true,
    defaultValue: () => nanoid(),
  },
  classroom_id: {
    type: DataTypes.STRING(21),
    allowNull: false,
  },
  number: {
    type: DataTypes.INTEGER,
    allowNull: false,
    comment: 'หมายเลขโต๊ะ',
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
  type: {
    type: DataTypes.ENUM('computer', 'normal', 'teacher'),
    allowNull: false,
    defaultValue: 'normal',
    comment: 'ประเภทโต๊ะ',
  },
  is_enabled: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: true,
    comment: 'เปิด/ปิดใช้งาน',
  },
}, {
  tableName: 'desks',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
  indexes: [
    { fields: ['classroom_id'] },
    { fields: ['classroom_id', 'number'] },
  ],
});

module.exports = Desk;
