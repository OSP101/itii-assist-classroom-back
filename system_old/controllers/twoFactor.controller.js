const asyncHandler = require('express-async-handler');
const { User, TwoFactorPending, SystemLog, RefreshToken } = require('../models');
const { ApiError, jwt: jwtUtil, logger } = require('../utils');
const { authLogger } = require('../middlewares/requestLogger');
const twoFactorService = require('../utils/twoFactorService');
const emailService = require('../utils/emailService');

/**
 * Get 2FA status for current user
 * GET /api/auth/2fa/status
 */
const getStatus = asyncHandler(async (req, res) => {
  const user = await User.findByPk(req.user.id, {
    attributes: ['id', 'two_factor_enabled', 'two_factor_method', 'two_factor_confirmed_at'],
  });

  if (!user) {
    throw ApiError.notFound('User not found');
  }

  res.json({
    success: true,
    data: {
      enabled: user.two_factor_enabled || false,
      method: user.two_factor_method || null,
      confirmedAt: user.two_factor_confirmed_at || null,
    },
  });
});

/**
 * Setup TOTP 2FA
 * POST /api/auth/2fa/setup/totp
 */
const setupTOTP = asyncHandler(async (req, res) => {
  const user = await User.findByPk(req.user.id);

  if (!user) {
    throw ApiError.notFound('User not found');
  }

  // Check if 2FA is already enabled
  if (user.two_factor_enabled) {
    throw ApiError.badRequest('การยืนยันตัวตนสองขั้นตอนเปิดใช้งานอยู่แล้ว กรุณาปิดก่อนเพื่อตั้งค่าใหม่');
  }

  // Generate new secret
  const secret = twoFactorService.generateTOTPSecret();
  const otpauth = twoFactorService.generateTOTPUri(secret, user.username);
  const qrCode = await twoFactorService.generateQRCode(otpauth);

  // Store pending setup (encrypted)
  const encryptedSecret = twoFactorService.encryptSecret(secret);
  
  // Upsert pending record
  await TwoFactorPending.upsert({
    user_id: user.id,
    method: 'totp',
    secret: encryptedSecret,
    expires_at: new Date(Date.now() + 15 * 60 * 1000), // 15 minutes
  });

  // Clean up expired records
  await TwoFactorPending.cleanupExpired();

  res.json({
    success: true,
    data: {
      qrCode,
      secret, // Show secret for manual entry
      issuer: require('../config').twoFactor.issuer,
    },
  });
});

/**
 * Setup Email 2FA
 * POST /api/auth/2fa/setup/email
 */
const setupEmail = asyncHandler(async (req, res) => {
  const user = await User.findByPk(req.user.id);

  if (!user) {
    throw ApiError.notFound('User not found');
  }

  // Check if 2FA is already enabled
  if (user.two_factor_enabled) {
    throw ApiError.badRequest('การยืนยันตัวตนสองขั้นตอนเปิดใช้งานอยู่แล้ว กรุณาปิดก่อนเพื่อตั้งค่าใหม่');
  }

  // Check if user has email
  if (!user.email) {
    throw ApiError.badRequest('กรุณาเพิ่มอีเมลในโปรไฟล์ก่อนเปิดใช้งานการยืนยันทางอีเมล');
  }

  // Generate email code
  const emailCode = twoFactorService.generateEmailCode();
  const expiresAt = new Date(Date.now() + 5 * 60 * 1000); // 5 minutes

  // Store pending setup
  await TwoFactorPending.upsert({
    user_id: user.id,
    method: 'email',
    secret: 'email', // Placeholder
    email_code: emailCode,
    email_code_expires_at: expiresAt,
    expires_at: new Date(Date.now() + 15 * 60 * 1000), // 15 minutes
  });

  // Send email
  try {
    await emailService.send2FACode(user.email, emailCode, user.full_name);
  } catch (error) {
    throw ApiError.internal('ไม่สามารถส่งอีเมลได้ กรุณาลองใหม่อีกครั้ง');
  }

  // Clean up expired records
  await TwoFactorPending.cleanupExpired();

  res.json({
    success: true,
    message: `ส่งรหัสยืนยันไปที่ ${user.email} แล้ว`,
    data: {
      email: user.email.replace(/(.{2})(.*)(@.*)/, '$1***$3'), // Mask email
    },
  });
});

/**
 * Verify and enable 2FA
 * POST /api/auth/2fa/verify
 */
const verifyAndEnable = asyncHandler(async (req, res) => {
  const { code, method } = req.body;

  if (!code || !method) {
    throw ApiError.badRequest('กรุณากรอกรหัสยืนยัน');
  }

  const user = await User.findByPk(req.user.id);
  if (!user) {
    throw ApiError.notFound('User not found');
  }

  // Get pending setup
  const pending = await TwoFactorPending.findOne({
    where: { user_id: user.id, method },
  });

  if (!pending) {
    throw ApiError.badRequest('ไม่พบการตั้งค่า กรุณาเริ่มใหม่อีกครั้ง');
  }

  // Check if expired
  if (new Date() > pending.expires_at) {
    await pending.destroy();
    throw ApiError.badRequest('หมดเวลา กรุณาเริ่มใหม่อีกครั้ง');
  }

  let isValid = false;

  if (method === 'totp') {
    // Verify TOTP code
    const decryptedSecret = twoFactorService.decryptSecret(pending.secret);
    if (!decryptedSecret) {
      throw ApiError.internal('เกิดข้อผิดพลาดในการตรวจสอบ');
    }
    isValid = twoFactorService.verifyTOTP(code, decryptedSecret);
  } else if (method === 'email') {
    // Verify email code
    if (new Date() > pending.email_code_expires_at) {
      throw ApiError.badRequest('รหัสหมดอายุแล้ว กรุณาขอรหัสใหม่');
    }
    isValid = pending.email_code === code;
  }

  if (!isValid) {
    throw ApiError.badRequest('รหัสไม่ถูกต้อง');
  }

  // Generate backup codes
  const { codes: backupCodes, hashedCodes } = await twoFactorService.generateBackupCodes();

  // Enable 2FA
  user.two_factor_enabled = true;
  user.two_factor_method = method;
  user.two_factor_secret = method === 'totp' ? pending.secret : null;
  user.two_factor_backup_codes = hashedCodes;
  user.two_factor_confirmed_at = new Date();
  await user.save();

  // Delete pending record
  await pending.destroy();

  // Log event
  await SystemLog.create({
    log_type: 'security',
    severity: 'info',
    actor_user_id: user.id,
    action: '2fa_enabled',
    detail: `User ${user.username} enabled 2FA via ${method}`,
    ip_address: req.ip,
    user_agent: req.get('User-Agent'),
  });

  // Send confirmation email
  if (user.email) {
    await emailService.send2FASetupEmail(user.email, method, user.full_name);
  }

  res.json({
    success: true,
    message: 'เปิดใช้งานการยืนยันตัวตนสองขั้นตอนสำเร็จ',
    data: {
      backupCodes, // Show backup codes once
      enabled: true,
      method,
    },
  });
});

/**
 * Resend email code
 * POST /api/auth/2fa/resend-email
 */
const resendEmailCode = asyncHandler(async (req, res) => {
  const user = await User.findByPk(req.user.id);

  if (!user || !user.email) {
    throw ApiError.badRequest('ไม่พบอีเมลในโปรไฟล์');
  }

  // Get or create pending record
  let pending = await TwoFactorPending.findOne({
    where: { user_id: user.id, method: 'email' },
  });

  // If no pending record and user has email 2FA enabled, create one for verification
  if (!pending && user.two_factor_enabled && user.two_factor_method === 'email') {
    pending = await TwoFactorPending.create({
      user_id: user.id,
      method: 'email',
      secret: 'email-verification', // Placeholder for email method
      email_code: twoFactorService.generateEmailCode(),
      email_code_expires_at: new Date(Date.now() + 5 * 60 * 1000),
    });
  }

  if (!pending) {
    throw ApiError.badRequest('ไม่พบการตั้งค่า กรุณาเริ่มใหม่อีกครั้ง');
  }

  // Generate new code
  const emailCode = twoFactorService.generateEmailCode();
  const expiresAt = new Date(Date.now() + 5 * 60 * 1000);

  pending.email_code = emailCode;
  pending.email_code_expires_at = expiresAt;
  await pending.save();

  // Send email
  try {
    await emailService.send2FACode(user.email, emailCode, user.full_name);
  } catch (error) {
    throw ApiError.internal('ไม่สามารถส่งอีเมลได้ กรุณาลองใหม่อีกครั้ง');
  }

  res.json({
    success: true,
    message: 'ส่งรหัสใหม่แล้ว',
  });
});

/**
 * Disable 2FA
 * POST /api/auth/2fa/disable
 */
const disable = asyncHandler(async (req, res) => {
  const { password, code } = req.body;

  if (!password) {
    throw ApiError.badRequest('กรุณากรอกรหัสผ่าน');
  }

  const user = await User.findByPk(req.user.id);
  if (!user) {
    throw ApiError.notFound('User not found');
  }

  // Verify password
  const isPasswordValid = await user.comparePassword(password);
  if (!isPasswordValid) {
    throw ApiError.badRequest('รหัสผ่านไม่ถูกต้อง');
  }

  // Verify 2FA code if enabled
  if (user.two_factor_enabled) {
    if (!code) {
      throw ApiError.badRequest('กรุณากรอกรหัส 2FA');
    }

    let isValid = false;

    // For TOTP method
    if (user.two_factor_method === 'totp' && user.two_factor_secret) {
      const decryptedSecret = twoFactorService.decryptSecret(user.two_factor_secret);
      isValid = twoFactorService.verifyTOTP(code, decryptedSecret);
    }

    // For Email method - check pending code
    if (user.two_factor_method === 'email') {
      const pending = await TwoFactorPending.findOne({
        where: { user_id: user.id, method: 'email' },
      });
      
      if (pending && pending.email_code === code) {
        if (new Date() <= pending.email_code_expires_at) {
          isValid = true;
          await pending.destroy();
        }
      }
    }

    // Also check backup codes
    if (!isValid) {
      const backupResult = await twoFactorService.verifyBackupCode(code, user.two_factor_backup_codes);
      if (backupResult.valid) {
        isValid = true;
        // Mark backup code as used
        const newCodes = [...user.two_factor_backup_codes];
        newCodes[backupResult.index] = null;
        user.two_factor_backup_codes = newCodes;
      }
    }

    if (!isValid) {
      throw ApiError.badRequest('รหัส 2FA ไม่ถูกต้อง');
    }
  }

  // Disable 2FA
  user.two_factor_enabled = false;
  user.two_factor_method = null;
  user.two_factor_secret = null;
  user.two_factor_backup_codes = null;
  user.two_factor_confirmed_at = null;
  await user.save();

  // Log event
  await SystemLog.create({
    log_type: 'security',
    severity: 'warn',
    actor_user_id: user.id,
    action: '2fa_disabled',
    detail: `User ${user.username} disabled 2FA`,
    ip_address: req.ip,
    user_agent: req.get('User-Agent'),
  });

  res.json({
    success: true,
    message: 'ปิดใช้งานการยืนยันตัวตนสองขั้นตอนสำเร็จ',
  });
});

/**
 * Regenerate backup codes
 * POST /api/auth/2fa/backup-codes
 */
const regenerateBackupCodes = asyncHandler(async (req, res) => {
  const { password } = req.body;

  if (!password) {
    throw ApiError.badRequest('กรุณากรอกรหัสผ่าน');
  }

  const user = await User.findByPk(req.user.id);
  if (!user) {
    throw ApiError.notFound('User not found');
  }

  // Verify password
  const isPasswordValid = await user.comparePassword(password);
  if (!isPasswordValid) {
    throw ApiError.badRequest('รหัสผ่านไม่ถูกต้อง');
  }

  if (!user.two_factor_enabled) {
    throw ApiError.badRequest('กรุณาเปิดใช้งาน 2FA ก่อน');
  }

  // Generate new backup codes
  const { codes: backupCodes, hashedCodes } = await twoFactorService.generateBackupCodes();

  user.two_factor_backup_codes = hashedCodes;
  await user.save();

  // Log event
  await SystemLog.create({
    log_type: 'security',
    severity: 'info',
    actor_user_id: user.id,
    action: '2fa_backup_codes_regenerated',
    detail: `User ${user.username} regenerated 2FA backup codes`,
    ip_address: req.ip,
    user_agent: req.get('User-Agent'),
  });

  res.json({
    success: true,
    message: 'สร้างรหัสสำรองใหม่สำเร็จ',
    data: {
      backupCodes,
    },
  });
});

/**
 * Verify 2FA code (for login flow)
 * POST /api/auth/2fa/verify-login
 */
const verifyLogin = asyncHandler(async (req, res) => {
  const { userId, code } = req.body;

  if (!userId || !code) {
    throw ApiError.badRequest('ข้อมูลไม่ครบถ้วน');
  }

  const user = await User.findByPk(userId);
  if (!user || !user.two_factor_enabled) {
    throw ApiError.badRequest('ไม่พบข้อมูลผู้ใช้หรือไม่ได้เปิดใช้งาน 2FA');
  }

  let isValid = false;
  let usedBackupCode = false;

  // For TOTP method
  if (user.two_factor_method === 'totp' && user.two_factor_secret) {
    const decryptedSecret = twoFactorService.decryptSecret(user.two_factor_secret);
    isValid = twoFactorService.verifyTOTP(code, decryptedSecret);
  }

  // For Email method - check pending code
  if (user.two_factor_method === 'email') {
    const pending = await TwoFactorPending.findOne({
      where: { user_id: user.id, method: 'email' },
    });
    
    if (pending && pending.email_code === code) {
      if (new Date() <= pending.email_code_expires_at) {
        isValid = true;
        await pending.destroy();
      }
    }
  }

  // Check backup codes if main method failed
  if (!isValid && user.two_factor_backup_codes) {
    const backupResult = await twoFactorService.verifyBackupCode(code, user.two_factor_backup_codes);
    if (backupResult.valid) {
      isValid = true;
      usedBackupCode = true;
      // Mark backup code as used
      const newCodes = [...user.two_factor_backup_codes];
      newCodes[backupResult.index] = null;
      user.two_factor_backup_codes = newCodes;
      await user.save();
    }
  }

  if (!isValid) {
    throw ApiError.badRequest('รหัสยืนยันไม่ถูกต้อง');
  }

  res.json({
    success: true,
    verified: true,
    usedBackupCode,
  });
});

/**
 * Send 2FA code for login (email method)
 * POST /api/auth/2fa/send-login-code
 */
const sendLoginCode = asyncHandler(async (req, res) => {
  const { userId } = req.body;

  if (!userId) {
    throw ApiError.badRequest('ข้อมูลไม่ครบถ้วน');
  }

  const user = await User.findByPk(userId);
  if (!user || !user.two_factor_enabled || user.two_factor_method !== 'email') {
    throw ApiError.badRequest('ไม่พบข้อมูลผู้ใช้หรือไม่ได้ใช้งาน Email 2FA');
  }

  if (!user.email) {
    throw ApiError.badRequest('ไม่พบอีเมลในโปรไฟล์');
  }

  // Generate email code
  const emailCode = twoFactorService.generateEmailCode();
  const expiresAt = new Date(Date.now() + 5 * 60 * 1000);

  // Store pending code
  await TwoFactorPending.upsert({
    user_id: user.id,
    method: 'email',
    secret: 'login',
    email_code: emailCode,
    email_code_expires_at: expiresAt,
    expires_at: new Date(Date.now() + 15 * 60 * 1000),
  });

  // Send email
  try {
    await emailService.send2FACode(user.email, emailCode, user.full_name);
  } catch (error) {
    throw ApiError.internal('ไม่สามารถส่งอีเมลได้ กรุณาลองใหม่อีกครั้ง');
  }

  res.json({
    success: true,
    message: `ส่งรหัสยืนยันไปที่อีเมลของคุณแล้ว`,
  });
});

/**
 * Complete login after 2FA verification
 * POST /api/auth/2fa/complete-login
 */
const completeLogin = asyncHandler(async (req, res) => {
  const { userId, code } = req.body;

  if (!userId || !code) {
    return res.status(400).json({
      success: false,
      message: 'ข้อมูลไม่ครบถ้วน',
    });
  }

  const user = await User.findByPk(userId);
  if (!user || !user.two_factor_enabled) {
    return res.status(400).json({
      success: false,
      message: 'ไม่พบข้อมูลผู้ใช้หรือไม่ได้เปิดใช้งาน 2FA',
    });
  }

  let isValid = false;
  let usedBackupCode = false;

  try {
    // For TOTP method
    if (user.two_factor_method === 'totp' && user.two_factor_secret) {
      const decryptedSecret = twoFactorService.decryptSecret(user.two_factor_secret);
      if (decryptedSecret) {
        isValid = twoFactorService.verifyTOTP(code, decryptedSecret);
      }
    }

    // For Email method - check pending code
    if (user.two_factor_method === 'email') {
      const pending = await TwoFactorPending.findOne({
        where: { user_id: user.id, method: 'email' },
      });
      
      if (pending && pending.email_code === code) {
        if (new Date() <= pending.email_code_expires_at) {
          isValid = true;
          await pending.destroy();
        }
      }
    }

    // Check backup codes if main method failed
    if (!isValid && user.two_factor_backup_codes && Array.isArray(user.two_factor_backup_codes)) {
      const backupResult = await twoFactorService.verifyBackupCode(code, user.two_factor_backup_codes);
      if (backupResult.valid) {
        isValid = true;
        usedBackupCode = true;
        // Mark backup code as used
        const newCodes = [...user.two_factor_backup_codes];
        newCodes[backupResult.index] = null;
        user.two_factor_backup_codes = newCodes;
        await user.save();
      }
    }
  } catch (verifyError) {
    console.error('2FA verification error:', verifyError);
    // Don't expose internal error - just treat as invalid code
    isValid = false;
  }

  if (!isValid) {
    return res.status(400).json({
      success: false,
      message: 'รหัสยืนยันไม่ถูกต้อง',
    });
  }

  // Generate tokens
  const { accessToken, refreshToken, jti, expiresAt } = jwtUtil.generateTokens(user);

  // Save refresh token to database
  await RefreshToken.create({
    jti,
    user_id: user.id,
    expires_at: expiresAt,
    meta: {
      ip: req.ip,
      userAgent: req.get('User-Agent'),
    },
  });

  // Log the login action
  await authLogger.logLogin(req, user, 'local-2fa');

  logger.info(`User ${user.username} logged in successfully via 2FA`);

  // Log security event
  await SystemLog.create({
    log_type: 'auth',
    severity: 'info',
    actor_user_id: user.id,
    action: '2fa_login_success',
    detail: `User ${user.username} completed 2FA login${usedBackupCode ? ' using backup code' : ''}`,
    ip_address: req.ip,
    user_agent: req.get('User-Agent'),
  });

  res.json({
    success: true,
    message: 'Login successful',
    data: {
      user: user.toSafeObject(),
      accessToken,
      refreshToken,
      mustChangePassword: user.must_change_password || false,
      usedBackupCode,
    },
  });
});

module.exports = {
  getStatus,
  setupTOTP,
  setupEmail,
  verifyAndEnable,
  resendEmailCode,
  disable,
  regenerateBackupCodes,
  verifyLogin,
  sendLoginCode,
  completeLogin,
};
