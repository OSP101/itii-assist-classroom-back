const jwt = require('jsonwebtoken');
const { v4: uuidv4 } = require('uuid');
const config = require('../config');

/**
 * Generate Access Token
 * @param {Object} payload - User data to encode
 * @returns {string} JWT access token
 */
const generateAccessToken = (payload) => {
  return jwt.sign(payload, config.jwt.accessSecret, {
    expiresIn: config.jwt.accessExpiresIn,
  });
};

/**
 * Generate Refresh Token
 * @param {Object} payload - User data to encode
 * @returns {Object} { token, jti, expiresAt }
 */
const generateRefreshToken = (payload) => {
  const jti = uuidv4();
  
  // Parse expires in string to milliseconds
  const expiresIn = config.jwt.refreshExpiresIn;
  let expiresMs = 7 * 24 * 60 * 60 * 1000; // default 7 days
  
  if (expiresIn.endsWith('d')) {
    expiresMs = parseInt(expiresIn) * 24 * 60 * 60 * 1000;
  } else if (expiresIn.endsWith('h')) {
    expiresMs = parseInt(expiresIn) * 60 * 60 * 1000;
  } else if (expiresIn.endsWith('m')) {
    expiresMs = parseInt(expiresIn) * 60 * 1000;
  }
  
  const expiresAt = new Date(Date.now() + expiresMs);
  
  const token = jwt.sign(
    { ...payload, jti },
    config.jwt.refreshSecret,
    { expiresIn: config.jwt.refreshExpiresIn }
  );
  
  return { token, jti, expiresAt };
};

/**
 * Verify Access Token
 * @param {string} token - JWT token
 * @returns {Object|null} Decoded payload or null
 */
const verifyAccessToken = (token) => {
  try {
    return jwt.verify(token, config.jwt.accessSecret);
  } catch (error) {
    return null;
  }
};

/**
 * Verify Refresh Token
 * @param {string} token - JWT token
 * @returns {Object|null} Decoded payload or null
 */
const verifyRefreshToken = (token) => {
  try {
    return jwt.verify(token, config.jwt.refreshSecret);
  } catch (error) {
    return null;
  }
};

/**
 * Generate both tokens for a user
 * @param {Object} user - User object
 * @returns {Object} { accessToken, refreshToken, jti, expiresAt }
 */
const generateTokens = (user) => {
  const payload = {
    userId: user.id,
    username: user.username,
    role: user.role,
  };
  
  // Generate refresh token first to get the jti
  const { token: refreshToken, jti, expiresAt } = generateRefreshToken(payload);
  
  // Include jti in access token so we can identify the session
  const accessToken = generateAccessToken({ ...payload, jti });
  
  return {
    accessToken,
    refreshToken,
    jti,
    expiresAt,
  };
};

module.exports = {
  generateAccessToken,
  generateRefreshToken,
  verifyAccessToken,
  verifyRefreshToken,
  generateTokens,
};
