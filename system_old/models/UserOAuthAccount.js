const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');

const UserOAuthAccount = sequelize.define('UserOAuthAccount', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
    references: {
      model: 'users',
      key: 'id',
    },
  },
  provider: {
    type: DataTypes.ENUM('google', 'github', 'apple'),
    allowNull: false,
  },
  provider_user_id: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  provider_email: {
    type: DataTypes.STRING(255),
    allowNull: true,
  },
  provider_avatar: {
    type: DataTypes.STRING(500),
    allowNull: true,
  },
  provider_name: {
    type: DataTypes.STRING(255),
    allowNull: true,
  },
  access_token: {
    type: DataTypes.TEXT,
    allowNull: true,
  },
  refresh_token: {
    type: DataTypes.TEXT,
    allowNull: true,
  },
  token_expires_at: {
    type: DataTypes.DATE,
    allowNull: true,
  },
  linked_at: {
    type: DataTypes.DATE,
    allowNull: false,
    defaultValue: DataTypes.NOW,
  },
}, {
  tableName: 'user_oauth_accounts',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
});

// Instance method to return safe data (without tokens)
UserOAuthAccount.prototype.toSafeObject = function() {
  const { access_token, refresh_token, ...safeData } = this.toJSON();
  return safeData;
};

// Static method to get provider display name
UserOAuthAccount.getProviderDisplayName = function(provider) {
  const names = {
    google: 'Google',
    github: 'GitHub',
    apple: 'Apple',
  };
  return names[provider] || provider;
};

module.exports = UserOAuthAccount;
