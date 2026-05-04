const { DataTypes } = require('sequelize');
const { sequelize } = require('../config/database');
const bcrypt = require('bcryptjs');

const User = sequelize.define('User', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  username: {
    type: DataTypes.STRING(100),
    allowNull: false,
    unique: true,
  },
  password_hash: {
    type: DataTypes.STRING(255),
    allowNull: false,
  },
  role: {
    type: DataTypes.ENUM('admin', 'instructor', 'ta'),
    allowNull: false,
  },
  full_name: {
    type: DataTypes.STRING(255),
    allowNull: true,
  },
  email: {
    type: DataTypes.STRING(255),
    allowNull: true,
  },
  google_id: {
    type: DataTypes.STRING(255),
    allowNull: true,
  },
  provider: {
    type: DataTypes.ENUM('local', 'google', 'github'),
    allowNull: false,
    defaultValue: 'local',
  },
  is_active: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: true,
  },
  must_change_password: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: false,
  },
  avatar: {
    type: DataTypes.TEXT('long'),
    allowNull: true,
  },
  // Two-Factor Authentication fields
  two_factor_enabled: {
    type: DataTypes.BOOLEAN,
    allowNull: false,
    defaultValue: false,
  },
  two_factor_method: {
    type: DataTypes.ENUM('totp', 'email'),
    allowNull: true,
    defaultValue: null,
  },
  two_factor_secret: {
    type: DataTypes.TEXT,
    allowNull: true,
    defaultValue: null,
  },
  two_factor_backup_codes: {
    type: DataTypes.JSON,
    allowNull: true,
    defaultValue: null,
  },
  two_factor_confirmed_at: {
    type: DataTypes.DATE,
    allowNull: true,
    defaultValue: null,
  },
}, {
  tableName: 'users',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: 'updated_at',
});

// Instance method to check password
User.prototype.comparePassword = async function(candidatePassword) {
  return bcrypt.compare(candidatePassword, this.password_hash);
};

// Instance method to return safe user data (without password and secrets)
User.prototype.toSafeObject = function() {
  const { password_hash, two_factor_secret, two_factor_backup_codes, ...safeUser } = this.toJSON();
  return safeUser;
};

// Static method to hash password
User.hashPassword = async function(password) {
  const salt = await bcrypt.genSalt(10);
  return bcrypt.hash(password, salt);
};

// Hook to hash password before create
User.beforeCreate(async (user) => {
  if (user.password_hash && !user.password_hash.startsWith('$2a$')) {
    user.password_hash = await User.hashPassword(user.password_hash);
  }
});

// Hook to hash password before update
User.beforeUpdate(async (user) => {
  if (user.changed('password_hash') && !user.password_hash.startsWith('$2a$')) {
    user.password_hash = await User.hashPassword(user.password_hash);
  }
});

module.exports = User;
