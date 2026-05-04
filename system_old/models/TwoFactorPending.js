const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

/**
 * TwoFactorPending Model
 * Stores pending 2FA setup data before confirmation
 */
const TwoFactorPending = sequelize.define('TwoFactorPending', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
  method: {
    type: DataTypes.ENUM('totp', 'email'),
    allowNull: false,
  },
  secret: {
    type: DataTypes.TEXT,
    allowNull: false,
  },
  email_code: {
    type: DataTypes.STRING(6),
    allowNull: true,
  },
  email_code_expires_at: {
    type: DataTypes.DATE,
    allowNull: true,
  },
  expires_at: {
    type: DataTypes.DATE,
    allowNull: false,
    defaultValue: () => new Date(Date.now() + 15 * 60 * 1000), // 15 minutes
  },
}, {
  tableName: 'two_factor_pending',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: false,
  indexes: [
    {
      unique: true,
      fields: ['user_id', 'method'],
    },
    {
      fields: ['expires_at'],
    },
  ],
});

// Static method to clean up expired pending records
TwoFactorPending.cleanupExpired = async function() {
  return await this.destroy({
    where: {
      expires_at: {
        [require('sequelize').Op.lt]: new Date(),
      },
    },
  });
};

module.exports = TwoFactorPending;
