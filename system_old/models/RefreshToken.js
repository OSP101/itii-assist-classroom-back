const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const RefreshToken = sequelize.define('RefreshToken', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  jti: {
    type: DataTypes.STRING(100),
    allowNull: false,
    unique: true,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
  revoked: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: false,
  },
  expires_at: {
    type: DataTypes.DATE,
    allowNull: false,
  },
  meta: {
    type: DataTypes.JSON,
    allowNull: true,
  },
}, {
  tableName: 'refresh_tokens',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: false,
});

// Static method to clean up expired tokens
RefreshToken.cleanupExpired = async function() {
  return this.destroy({
    where: {
      expires_at: {
        [require('sequelize').Op.lt]: new Date(),
      },
    },
  });
};

// Static method to clean up revoked tokens (older than 24 hours)
RefreshToken.cleanupRevoked = async function() {
  const oneDayAgo = new Date(Date.now() - 24 * 60 * 60 * 1000);
  return this.destroy({
    where: {
      revoked: true,
      created_at: {
        [require('sequelize').Op.lt]: oneDayAgo,
      },
    },
  });
};

// Static method to clean up all stale sessions
RefreshToken.cleanupStaleSessions = async function() {
  const expiredCount = await this.cleanupExpired();
  const revokedCount = await this.cleanupRevoked();
  return { expiredCount, revokedCount };
};

module.exports = RefreshToken;
