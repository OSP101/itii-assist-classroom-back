const { DataTypes, Op } = require('sequelize');
const { sequelize } = require('../config/database');
const crypto = require('crypto');

const PasswordResetToken = sequelize.define('PasswordResetToken', {
  id: {
    type: DataTypes.BIGINT,
    primaryKey: true,
    autoIncrement: true,
  },
  user_id: {
    type: DataTypes.BIGINT,
    allowNull: false,
  },
  token: {
    type: DataTypes.STRING(255),
    allowNull: false,
    unique: true,
  },
  expires_at: {
    type: DataTypes.DATE,
    allowNull: false,
  },
  used_at: {
    type: DataTypes.DATE,
    allowNull: true,
    defaultValue: null,
  },
}, {
  tableName: 'password_reset_tokens',
  timestamps: true,
  createdAt: 'created_at',
  updatedAt: false,
});

/**
 * Generate a secure token for password reset
 * @returns {string} Secure hex token
 */
PasswordResetToken.generateToken = function() {
  return crypto.randomBytes(32).toString('hex');
};

/**
 * Create a password reset token for a user
 * @param {number} userId - User ID
 * @param {number} expiresInMinutes - Token expiry in minutes (default: 60)
 * @returns {Promise<{token: string, expires_at: Date}>}
 */
PasswordResetToken.createForUser = async function(userId, expiresInMinutes = 60) {
  // Invalidate any existing tokens for this user
  await this.destroy({
    where: {
      user_id: userId,
      used_at: null,
    }
  });

  // Generate new token
  const token = this.generateToken();
  const expires_at = new Date(Date.now() + expiresInMinutes * 60 * 1000);

  // Create token record
  await this.create({
    user_id: userId,
    token,
    expires_at,
  });

  return { token, expires_at };
};

/**
 * Verify and return the token record if valid
 * @param {string} token - The token to verify
 * @returns {Promise<PasswordResetToken|null>}
 */
PasswordResetToken.verifyToken = async function(token) {
  const tokenRecord = await this.findOne({
    where: {
      token,
      used_at: null,
      expires_at: {
        [Op.gt]: new Date(),
      },
    },
  });

  return tokenRecord;
};

/**
 * Mark token as used
 * @param {string} token - The token to mark as used
 */
PasswordResetToken.markAsUsed = async function(token) {
  await this.update(
    { used_at: new Date() },
    { where: { token } }
  );
};

/**
 * Clean up expired tokens (for scheduled cleanup)
 */
PasswordResetToken.cleanupExpired = async function() {
  const deleted = await this.destroy({
    where: {
      [Op.or]: [
        { expires_at: { [Op.lt]: new Date() } },
        { used_at: { [Op.not]: null } },
      ],
    },
  });
  return deleted;
};

module.exports = PasswordResetToken;
