const { TOTP, generateSecret: generateOTPSecret, generateURI, verifySync } = require('otplib');
const QRCode = require('qrcode');
const crypto = require('crypto');
const logger = require('./logger');
const bcrypt = require('bcryptjs');
const config = require('../config');

// Configure TOTP
const totp = new TOTP({
  digits: 6,
  period: 30,
});

/**
 * Generate a new TOTP secret
 */
const generateTOTPSecret = () => {
  return generateOTPSecret();
};

/**
 * Generate TOTP URI for QR code
 */
const generateTOTPUri = (secret, username) => {
  return generateURI({
    type: 'totp',
    label: username,
    issuer: config.twoFactor.issuer,
    secret: secret,
    algorithm: 'SHA1',
    digits: 6,
    period: 30,
  });
};

/**
 * Generate QR code as data URL
 */
const generateQRCode = async (otpauth) => {
  try {
    return await QRCode.toDataURL(otpauth, {
      width: 256,
      margin: 2,
      color: {
        dark: '#000000',
        light: '#ffffff',
      },
    });
  } catch (error) {
    logger.error('Failed to generate QR code:', error);
    throw error;
  }
};

/**
 * Verify TOTP token
 */
const verifyTOTP = (token, secret) => {
  try {
    // Use verifySync which returns { valid: boolean } for otplib v12+
    const result = verifySync({
      type: 'totp',
      token: token,
      secret: secret,
      digits: 6,
      period: 30,
      window: 1, // Allow 1 step before/after for time drift
    });
    return result.valid === true;
  } catch (error) {
    logger.error('TOTP verification error:', error);
    return false;
  }
};

/**
 * Generate a random 6-digit email code
 */
const generateEmailCode = () => {
  return crypto.randomInt(100000, 999999).toString();
};

/**
 * Generate backup codes (8 codes, each 8 characters)
 */
const generateBackupCodes = async () => {
  const codes = [];
  const hashedCodes = [];
  
  for (let i = 0; i < 8; i++) {
    // Generate random code: xxxx-xxxx format
    const part1 = crypto.randomBytes(2).toString('hex').toUpperCase();
    const part2 = crypto.randomBytes(2).toString('hex').toUpperCase();
    const code = `${part1}-${part2}`;
    codes.push(code);
    
    // Hash the code for storage
    const salt = await bcrypt.genSalt(10);
    const hashedCode = await bcrypt.hash(code, salt);
    hashedCodes.push(hashedCode);
  }
  
  return { codes, hashedCodes };
};

/**
 * Verify a backup code
 */
const verifyBackupCode = async (code, hashedCodes) => {
  if (!hashedCodes || !Array.isArray(hashedCodes)) {
    return { valid: false, index: -1 };
  }
  
  for (let i = 0; i < hashedCodes.length; i++) {
    if (hashedCodes[i] && await bcrypt.compare(code, hashedCodes[i])) {
      return { valid: true, index: i };
    }
  }
  
  return { valid: false, index: -1 };
};

/**
 * Simple encryption for storing secrets (using JWT secret as key)
 * In production, use proper key management
 */
const encryptSecret = (secret) => {
  const key = crypto.scryptSync(config.jwt.accessSecret, 'salt', 32);
  const iv = crypto.randomBytes(16);
  const cipher = crypto.createCipheriv('aes-256-cbc', key, iv);
  let encrypted = cipher.update(secret, 'utf8', 'hex');
  encrypted += cipher.final('hex');
  return iv.toString('hex') + ':' + encrypted;
};

/**
 * Decrypt stored secret
 */
const decryptSecret = (encryptedSecret) => {
  try {
    const key = crypto.scryptSync(config.jwt.accessSecret, 'salt', 32);
    const [ivHex, encrypted] = encryptedSecret.split(':');
    const iv = Buffer.from(ivHex, 'hex');
    const decipher = crypto.createDecipheriv('aes-256-cbc', key, iv);
    let decrypted = decipher.update(encrypted, 'hex', 'utf8');
    decrypted += decipher.final('utf8');
    return decrypted;
  } catch (error) {
    logger.error('Failed to decrypt secret:', error);
    return null;
  }
};

module.exports = {
  generateTOTPSecret,
  generateTOTPUri,
  generateQRCode,
  verifyTOTP,
  generateEmailCode,
  generateBackupCodes,
  verifyBackupCode,
  encryptSecret,
  decryptSecret,
};
