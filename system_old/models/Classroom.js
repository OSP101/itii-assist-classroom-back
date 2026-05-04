const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');
const { nanoid } = require('nanoid');

const Classroom = sequelize.define('Classroom', {
  id: {
    type: DataTypes.STRING(21),
    primaryKey: true,
    defaultValue: () => nanoid(),
  },
  name: {
    type: DataTypes.STRING(100),
    allowNull: false,
    comment: 'ชื่อห้อง เช่น ห้อง 306',
  },
  building: {
    type: DataTypes.STRING(100),
    allowNull: false,
    comment: 'อาคาร เช่น อาคาร IT',
  },
  floor: {
    type: DataTypes.STRING(20),
    allowNull: false,
    comment: 'ชั้น',
  },
  description: {
    type: DataTypes.TEXT,
    allowNull: true,
    comment: 'รายละเอียดเพิ่มเติม',
  },
  is_active: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: true,
    comment: 'สถานะเปิด/ปิดใช้งาน',
  },
  is_deleted: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: false,
    comment: 'Soft delete flag',
  },
  created_by: {
    type: DataTypes.BIGINT,
    allowNull: true,
    comment: 'ผู้สร้าง',
  },
}, {
  tableName: 'classrooms',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
  indexes: [
    { fields: ['building'] },
    { fields: ['is_deleted'] },
  ],
});

module.exports = Classroom;
