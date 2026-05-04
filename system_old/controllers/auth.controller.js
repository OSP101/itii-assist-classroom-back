const passport = require('passport');
const { User, RefreshToken, SystemLog, PasswordResetToken } = require('../models');
const { jwt: jwtUtil, ApiError, asyncHandler, logger } = require('../utils');
const { sendPasswordResetEmail } = require('../utils/emailService');
const { authLogger, securityLogger, getClientIp } = require('../middlewares/requestLogger');
const UAParser = require('ua-parser-js');
const { Op } = require('sequelize');
const config = require('../config');

/**
 * Login with username and password
 * POST /api/auth/login
 */
const login = asyncHandler(async (req, res, next) => {
  passport.authenticate('local', { session: false }, async (err, user, info) => {
    if (err) {
      return next(err);
    }
    
    if (!user) {
      // Log failed login attempt
      await authLogger.logLoginFailed(req, req.body?.username || 'unknown', info?.message || 'Invalid credentials');
      return next(ApiError.unauthorized(info?.message || 'Invalid credentials'));
    }
    
    try {
      // Check if 2FA is enabled
      if (user.two_factor_enabled) {
        return res.json({
          success: true,
          message: 'Two-factor authentication required',
          data: {
            requiresTwoFactor: true,
            twoFactorMethod: user.two_factor_method,
            userId: user.id,
            // Mask email for display
            email: user.email ? user.email.replace(/(.{2})(.*)(@.*)/, '$1***$3') : null,
          },
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
      await authLogger.logLogin(req, user, 'local');
      
      logger.info(`User ${user.username} logged in successfully`);
      
      res.json({
        success: true,
        message: 'Login successful',
        data: {
          user: user.toSafeObject(),
          accessToken,
          refreshToken,
          mustChangePassword: user.must_change_password || false,
        },
      });
    } catch (error) {
      next(error);
    }
  })(req, res, next);
});

/**
 * Refresh access token
 * POST /api/auth/refresh
 */
const refresh = asyncHandler(async (req, res) => {
  const { refreshToken } = req.body;
  
  // Verify refresh token
  const decoded = jwtUtil.verifyRefreshToken(refreshToken);
  
  if (!decoded) {
    throw ApiError.unauthorized('Invalid or expired refresh token');
  }
  
  // Check if token exists and is not revoked
  const tokenRecord = await RefreshToken.findOne({
    where: { jti: decoded.jti, revoked: false },
  });
  
  if (!tokenRecord) {
    throw ApiError.unauthorized('Refresh token has been revoked');
  }
  
  // Get user
  const user = await User.findByPk(decoded.userId);
  
  if (!user || !user.is_active) {
    throw ApiError.unauthorized('User not found or inactive');
  }
  
  // Revoke old refresh token
  tokenRecord.revoked = true;
  await tokenRecord.save();
  
  // Generate new tokens
  const { 
    accessToken: newAccessToken, 
    refreshToken: newRefreshToken, 
    jti, 
    expiresAt 
  } = jwtUtil.generateTokens(user);
  
  // Save new refresh token
  await RefreshToken.create({
    jti,
    user_id: user.id,
    expires_at: expiresAt,
    meta: {
      ip: req.ip,
      userAgent: req.get('User-Agent'),
    },
  });
  
  res.json({
    success: true,
    message: 'Token refreshed successfully',
    data: {
      accessToken: newAccessToken,
      refreshToken: newRefreshToken,
    },
  });
});

/**
 * Logout - revoke refresh token
 * POST /api/auth/logout
 */
const logout = asyncHandler(async (req, res) => {
  const { refreshToken } = req.body;
  
  if (refreshToken) {
    const decoded = jwtUtil.verifyRefreshToken(refreshToken);
    
    if (decoded?.jti) {
      await RefreshToken.update(
        { revoked: true },
        { where: { jti: decoded.jti } }
      );
    }
  }
  
  // Log the logout action
  if (req.user) {
    await authLogger.logLogout(req, req.user);
  }
  
  res.json({
    success: true,
    message: 'Logged out successfully',
  });
});

/**
 * Get current user profile
 * GET /api/auth/me
 */
const getMe = asyncHandler(async (req, res) => {
  const user = await User.findByPk(req.user.id, {
    attributes: { exclude: ['password_hash'] },
  });
  
  res.json({
    success: true,
    data: {
      user: user.toSafeObject(),
    },
  });
});

/**
 * Change password
 * POST /api/auth/change-password
 */
const changePassword = asyncHandler(async (req, res) => {
  const { currentPassword, newPassword } = req.body;
  
  const user = await User.findByPk(req.user.id);
  
  // Verify current password
  const isMatch = await user.comparePassword(currentPassword);
  
  if (!isMatch) {
    throw ApiError.badRequest('รหัสผ่านปัจจุบันของคุณไม่ถูกต้อง');
  }
  
  // Update password and reset must_change_password flag
  user.password_hash = newPassword; // Will be hashed by beforeUpdate hook
  user.must_change_password = false;
  await user.save();
  
  // Revoke all refresh tokens for this user
  await RefreshToken.update(
    { revoked: true },
    { where: { user_id: user.id } }
  );
  
  // Log password change
  await authLogger.logPasswordChange(req, user);
  
  res.json({
    success: true,
    message: 'Password changed successfully. Please login again.',
  });
});

/**
 * Force change password (for first login)
 * POST /api/auth/force-change-password
 */
const forceChangePassword = asyncHandler(async (req, res) => {
  const { newPassword } = req.body;
  
  if (!newPassword || newPassword.length < 6) {
    throw ApiError.badRequest('รหัสผ่านต้องมีอย่างน้อย 6 ตัวอักษร');
  }
  
  const user = await User.findByPk(req.user.id);
  
  if (!user) {
    throw ApiError.notFound('User not found');
  }
  
  // Only allow if must_change_password is true
  if (!user.must_change_password) {
    throw ApiError.badRequest('ไม่จำเป็นต้องเปลี่ยนรหัสผ่าน');
  }
  
  // Update password and reset flag
  user.password_hash = newPassword; // Will be hashed by beforeUpdate hook
  user.must_change_password = false;
  await user.save();
  
  // Revoke all refresh tokens for this user
  await RefreshToken.update(
    { revoked: true },
    { where: { user_id: user.id } }
  );
  
  // Log password change
  await authLogger.logPasswordChange(req, user);
  
  res.json({
    success: true,
    message: 'เปลี่ยนรหัสผ่านสำเร็จ กรุณาเข้าสู่ระบบใหม่',
  });
});

/**
 * Update user profile (requires password confirmation)
 * PUT /api/auth/profile
 */
const updateProfile = asyncHandler(async (req, res) => {
  const { full_name, email, current_password } = req.body;
  
  const user = await User.findByPk(req.user.id);
  
  if (!user) {
    throw ApiError.notFound('User not found');
  }
  
  // Verify current password before allowing profile update
  const isPasswordValid = await user.comparePassword(current_password);
  if (!isPasswordValid) {
    throw ApiError.badRequest('รหัสผ่านไม่ถูกต้อง');
  }
  
  // Update allowed fields
  if (full_name !== undefined) user.full_name = full_name;
  if (email !== undefined) user.email = email;
  
  await user.save();
  
  // Log profile update
  await SystemLog.create({
    log_type: 'auth',
    severity: 'info',
    actor_user_id: user.id,
    action: 'profile_updated',
    detail: `User ${user.username} updated their profile`,
    ip_address: getClientIp(req),
    user_agent: req.get('User-Agent'),
  });
  
  res.json({
    success: true,
    message: 'Profile updated successfully',
    data: {
      user: user.toSafeObject(),
    },
  });
});

/**
 * Google OAuth callback
 * GET /api/auth/google/callback
 */
const googleCallback = asyncHandler(async (req, res, next) => {
  // Check if this is a link action from cookie
  const isLinkAction = req.cookies?.oauth_action === 'link';
  const linkToken = req.cookies?.oauth_link_token;
  
  // Clear the cookies
  res.clearCookie('oauth_action');
  res.clearCookie('oauth_link_token');
  
  passport.authenticate('google', { session: false }, async (err, user, info) => {
    if (err) {
      return next(err);
    }
    
    // Handle link action when user not found through normal flow
    if (!user && isLinkAction && linkToken && info?.profile) {
      try {
        // Verify the link token to get the current user
        const decoded = jwtUtil.verifyAccessToken(linkToken);
        if (!decoded || !decoded.userId) {
          const errorMessage = encodeURIComponent('Invalid or expired session. Please try again.');
          return res.redirect(`${config.frontendUrl}/auth/link-callback?error=${errorMessage}`);
        }
        
        // Find the user
        const existingUser = await User.findByPk(decoded.userId);
        if (!existingUser) {
          const errorMessage = encodeURIComponent('User not found. Please try again.');
          return res.redirect(`${config.frontendUrl}/auth/link-callback?error=${errorMessage}`);
        }
        
        // Link the OAuth account
        const { UserOAuthAccount } = require('../models');
        await UserOAuthAccount.findOrCreate({
          where: { user_id: existingUser.id, provider: 'google' },
          defaults: {
            provider_user_id: info.profile.provider_user_id,
            provider_email: info.profile.provider_email,
            provider_name: info.profile.provider_name,
            provider_avatar: info.profile.provider_avatar,
            linked_at: new Date(),
          },
        });
        
        // Generate new tokens
        const { accessToken, refreshToken, jti, expiresAt } = jwtUtil.generateTokens(existingUser);
        
        // Save refresh token
        await RefreshToken.create({
          jti,
          user_id: existingUser.id,
          expires_at: expiresAt,
          meta: {
            ip: req.ip,
            userAgent: req.get('User-Agent'),
            provider: 'google',
          },
        });
        
        // Redirect with success
        const redirectUrl = `${config.frontendUrl}/auth/link-callback?accessToken=${accessToken}&refreshToken=${refreshToken}&linked=google`;
        return res.redirect(redirectUrl);
        
      } catch (linkError) {
        console.error('Link error:', linkError);
        const errorMessage = encodeURIComponent('Failed to link account. Please try again.');
        return res.redirect(`${config.frontendUrl}/auth/link-callback?error=${errorMessage}`);
      }
    }
    
    if (!user) {
      // Redirect to frontend with error
      const errorMessage = encodeURIComponent(info?.message || 'Google login failed');
      const redirectPath = isLinkAction ? '/auth/link-callback' : '/login';
      return res.redirect(`${config.frontendUrl}${redirectPath}?error=${errorMessage}`);
    }
    
    try {
      // For link action, redirect back to profile page with success message
      if (isLinkAction) {
        // Generate tokens for the user
        const { accessToken, refreshToken, jti, expiresAt } = jwtUtil.generateTokens(user);
        
        // Save refresh token to database
        await RefreshToken.create({
          jti,
          user_id: user.id,
          expires_at: expiresAt,
          meta: {
            ip: req.ip,
            userAgent: req.get('User-Agent'),
            provider: 'google',
          },
        });
        
        // Log the login action
        await authLogger.logLogin(req, user, 'google');
        
        // Redirect to link-callback page with tokens and success flag
        const redirectUrl = `${config.frontendUrl}/auth/link-callback?accessToken=${accessToken}&refreshToken=${refreshToken}&linked=google`;
        return res.redirect(redirectUrl);
      }
      
      // Check if 2FA is enabled
      if (user.two_factor_enabled) {
        // Redirect to frontend with 2FA required data
        const twoFactorData = {
          requiresTwoFactor: true,
          twoFactorMethod: user.two_factor_method,
          userId: user.id,
          email: user.email ? user.email.replace(/(.{2})(.*)(@.*)/, '$1***$3') : null,
        };
        const encodedData = encodeURIComponent(JSON.stringify(twoFactorData));
        return res.redirect(`${config.frontendUrl}/auth/callback?twoFactor=${encodedData}`);
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
          provider: 'google',
        },
      });
      
      // Log the login action
      await authLogger.logLogin(req, user, 'google');
      
      // Redirect to frontend with tokens
      const redirectUrl = `${config.frontendUrl}/auth/callback?accessToken=${accessToken}&refreshToken=${refreshToken}`;
      res.redirect(redirectUrl);
      
    } catch (error) {
      next(error);
    }
  })(req, res, next);
});

/**
 * GitHub OAuth callback
 * GET /api/auth/github/callback
 */
const githubCallback = asyncHandler(async (req, res, next) => {
  // Check if this is a link action from cookie
  const isLinkAction = req.cookies?.oauth_action === 'link';
  const linkToken = req.cookies?.oauth_link_token;
  
  // Clear the cookies
  res.clearCookie('oauth_action');
  res.clearCookie('oauth_link_token');
  
  passport.authenticate('github', { session: false }, async (err, user, info) => {
    if (err) {
      return next(err);
    }
    
    // Handle link action when user not found through normal flow
    if (!user && isLinkAction && linkToken && info?.profile) {
      try {
        // Verify the link token to get the current user
        const decoded = jwtUtil.verifyAccessToken(linkToken);
        if (!decoded || !decoded.userId) {
          const errorMessage = encodeURIComponent('Invalid or expired session. Please try again.');
          return res.redirect(`${config.frontendUrl}/auth/link-callback?error=${errorMessage}`);
        }
        
        // Find the user
        const existingUser = await User.findByPk(decoded.userId);
        if (!existingUser) {
          const errorMessage = encodeURIComponent('User not found. Please try again.');
          return res.redirect(`${config.frontendUrl}/auth/link-callback?error=${errorMessage}`);
        }
        
        // Link the OAuth account
        const { UserOAuthAccount } = require('../models');
        await UserOAuthAccount.findOrCreate({
          where: { user_id: existingUser.id, provider: 'github' },
          defaults: {
            provider_user_id: info.profile.provider_user_id,
            provider_email: info.profile.provider_email,
            provider_name: info.profile.provider_name,
            provider_avatar: info.profile.provider_avatar,
            linked_at: new Date(),
          },
        });
        
        // Generate new tokens
        const { accessToken, refreshToken, jti, expiresAt } = jwtUtil.generateTokens(existingUser);
        
        // Save refresh token
        await RefreshToken.create({
          jti,
          user_id: existingUser.id,
          expires_at: expiresAt,
          meta: {
            ip: req.ip,
            userAgent: req.get('User-Agent'),
            provider: 'github',
          },
        });
        
        // Redirect with success
        const redirectUrl = `${config.frontendUrl}/auth/link-callback?accessToken=${accessToken}&refreshToken=${refreshToken}&linked=github`;
        return res.redirect(redirectUrl);
        
      } catch (linkError) {
        console.error('Link error:', linkError);
        const errorMessage = encodeURIComponent('Failed to link account. Please try again.');
        return res.redirect(`${config.frontendUrl}/auth/link-callback?error=${errorMessage}`);
      }
    }
    
    if (!user) {
      // Redirect to frontend with error
      const errorMessage = encodeURIComponent(info?.message || 'GitHub login failed');
      const redirectPath = isLinkAction ? '/auth/link-callback' : '/login';
      return res.redirect(`${config.frontendUrl}${redirectPath}?error=${errorMessage}`);
    }
    
    try {
      // For link action, redirect back to profile page with success message
      if (isLinkAction) {
        // Generate tokens for the user
        const { accessToken, refreshToken, jti, expiresAt } = jwtUtil.generateTokens(user);
        
        // Save refresh token to database
        await RefreshToken.create({
          jti,
          user_id: user.id,
          expires_at: expiresAt,
          meta: {
            ip: req.ip,
            userAgent: req.get('User-Agent'),
            provider: 'github',
          },
        });
        
        // Log the login action
        await authLogger.logLogin(req, user, 'github');
        
        // Redirect to link-callback page with tokens and success flag
        const redirectUrl = `${config.frontendUrl}/auth/link-callback?accessToken=${accessToken}&refreshToken=${refreshToken}&linked=github`;
        return res.redirect(redirectUrl);
      }
      
      // Check if 2FA is enabled
      if (user.two_factor_enabled) {
        // Redirect to frontend with 2FA required data
        const twoFactorData = {
          requiresTwoFactor: true,
          twoFactorMethod: user.two_factor_method,
          userId: user.id,
          email: user.email ? user.email.replace(/(.{2})(.*)(@.*)/, '$1***$3') : null,
        };
        const encodedData = encodeURIComponent(JSON.stringify(twoFactorData));
        return res.redirect(`${config.frontendUrl}/auth/callback?twoFactor=${encodedData}`);
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
          provider: 'github',
        },
      });
      
      // Log the login action
      await authLogger.logLogin(req, user, 'github');
      
      // Redirect to frontend with tokens
      const redirectUrl = `${config.frontendUrl}/auth/callback?accessToken=${accessToken}&refreshToken=${refreshToken}`;
      res.redirect(redirectUrl);
      
    } catch (error) {
      next(error);
    }
  })(req, res, next);
});

/**
 * Apple OAuth callback
 * POST /api/auth/apple/callback
 */
const appleCallback = asyncHandler(async (req, res, next) => {
  passport.authenticate('apple', { session: false }, async (err, user, info) => {
    if (err) {
      return next(err);
    }
    
    if (!user) {
      // Redirect to frontend with error
      const errorMessage = encodeURIComponent(info?.message || 'Apple login failed');
      return res.redirect(`${config.frontendUrl}/login?error=${errorMessage}`);
    }
    
    try {
      // Check if 2FA is enabled
      if (user.two_factor_enabled) {
        // Redirect to frontend with 2FA required data
        const twoFactorData = {
          requiresTwoFactor: true,
          twoFactorMethod: user.two_factor_method,
          userId: user.id,
          email: user.email ? user.email.replace(/(.{2})(.*)(@.*)/, '$1***$3') : null,
        };
        const encodedData = encodeURIComponent(JSON.stringify(twoFactorData));
        return res.redirect(`${config.frontendUrl}/auth/callback?twoFactor=${encodedData}`);
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
          provider: 'apple',
        },
      });
      
      // Log the login action
      await authLogger.logLogin(req, user, 'apple');
      
      // Redirect to frontend with tokens
      const redirectUrl = `${config.frontendUrl}/auth/callback?accessToken=${accessToken}&refreshToken=${refreshToken}`;
      res.redirect(redirectUrl);
      
    } catch (error) {
      next(error);
    }
  })(req, res, next);
});

/**
 * Get active sessions for current user
 * GET /api/auth/sessions
 */
const getSessions = asyncHandler(async (req, res) => {
  const sessions = await RefreshToken.findAll({
    where: {
      user_id: req.user.id,
      revoked: false,
      expires_at: {
        [Op.gt]: new Date(),
      },
    },
    order: [['created_at', 'DESC']],
  });
  
  // Get current session JTI from access token
  const authHeader = req.get('Authorization');
  let currentJti = null;
  if (authHeader?.startsWith('Bearer ')) {
    const token = authHeader.slice(7);
    try {
      const decoded = jwtUtil.verifyAccessToken(token);
      currentJti = decoded?.jti;
    } catch (e) {
      // Ignore
    }
  }
  
  // Parse user agent and format sessions
  const formattedSessions = sessions.map(session => {
    const meta = session.meta || {};
    const parser = new UAParser(meta.userAgent || '');
    const browser = parser.getBrowser();
    const os = parser.getOS();
    const device = parser.getDevice();
    
    // Determine if this is the current session
    const isCurrent = session.jti === currentJti;
    
    return {
      id: session.id,
      jti: session.jti,
      ip: meta.ip || 'Unknown',
      browser: browser.name ? `${browser.name} ${browser.version || ''}`.trim() : 'Unknown',
      os: os.name ? `${os.name} ${os.version || ''}`.trim() : 'Unknown',
      device: device.type || 'Desktop',
      provider: meta.provider || 'local',
      loginAt: session.created_at,
      expiresAt: session.expires_at,
      isCurrent,
    };
  });
  
  res.json({
    success: true,
    data: {
      sessions: formattedSessions,
    },
  });
});

/**
 * Revoke a specific session
 * DELETE /api/auth/sessions/:sessionId
 */
const revokeSession = asyncHandler(async (req, res) => {
  const { sessionId } = req.params;
  
  const session = await RefreshToken.findOne({
    where: {
      id: sessionId,
      user_id: req.user.id,
      revoked: false,
    },
  });
  
  if (!session) {
    throw ApiError.notFound('Session not found');
  }
  
  session.revoked = true;
  await session.save();
  
  // Log session revocation
  await SystemLog.create({
    log_type: 'security',
    severity: 'info',
    actor_user_id: req.user.id,
    action: 'session_revoked',
    detail: `User ${req.user.username} revoked session ${sessionId}`,
    ip_address: getClientIp(req),
    user_agent: req.get('User-Agent'),
  });
  
  res.json({
    success: true,
    message: 'Session revoked successfully',
  });
});

/**
 * Revoke all sessions except current
 * POST /api/auth/sessions/revoke-all
 */
const revokeAllSessions = asyncHandler(async (req, res) => {
  // Get current session JTI
  const authHeader = req.get('Authorization');
  let currentJti = null;
  if (authHeader?.startsWith('Bearer ')) {
    const token = authHeader.slice(7);
    try {
      const decoded = jwtUtil.verifyAccessToken(token);
      currentJti = decoded?.jti;
    } catch (e) {
      // Ignore
    }
  }
  
  // Revoke all sessions except current
  const whereClause = {
    user_id: req.user.id,
    revoked: false,
  };
  
  if (currentJti) {
    whereClause.jti = { [Op.ne]: currentJti };
  }
  
  const result = await RefreshToken.update(
    { revoked: true },
    { where: whereClause }
  );
  
  // Log
  await SystemLog.create({
    log_type: 'security',
    severity: 'warn',
    actor_user_id: req.user.id,
    action: 'all_sessions_revoked',
    detail: `User ${req.user.username} revoked all other sessions (${result[0]} sessions)`,
    ip_address: getClientIp(req),
    user_agent: req.get('User-Agent'),
  });
  
  res.json({
    success: true,
    message: `Revoked ${result[0]} session(s)`,
    data: {
      revokedCount: result[0],
    },
  });
});

/**
 * Upload user avatar
 * POST /api/auth/avatar
 */
const uploadUserAvatar = asyncHandler(async (req, res) => {
  if (!req.file) {
    throw ApiError.badRequest('No file uploaded');
  }
  
  const user = await User.findByPk(req.user.id);
  if (!user) {
    throw ApiError.notFound('User not found');
  }
  
  // Process image with sharp and convert to base64
  const sharp = require('sharp');
  const processedBuffer = await sharp(req.file.buffer)
    .resize(256, 256, {
      fit: 'cover',
      position: 'center',
    })
    .jpeg({
      quality: 85,
      mozjpeg: true,
    })
    .toBuffer();
  
  // Convert to base64 data URL
  const base64Avatar = `data:image/jpeg;base64,${processedBuffer.toString('base64')}`;
  
  // Update user avatar with base64
  user.avatar = base64Avatar;
  await user.save();
  
  // Log avatar update
  await SystemLog.create({
    log_type: 'auth',
    severity: 'info',
    actor_user_id: user.id,
    action: 'avatar_updated',
    detail: `User ${user.username} updated their avatar`,
    ip_address: getClientIp(req),
    user_agent: req.get('User-Agent'),
  });
  
  res.json({
    success: true,
    message: 'Avatar updated successfully',
    data: {
      avatar: base64Avatar,
    },
  });
});

/**
 * Remove user avatar
 * DELETE /api/auth/avatar
 */
const removeAvatar = asyncHandler(async (req, res) => {
  const user = await User.findByPk(req.user.id);
  if (!user) {
    throw ApiError.notFound('User not found');
  }
  
  user.avatar = null;
  await user.save();
  
  res.json({
    success: true,
    message: 'Avatar removed successfully',
    data: {
      user: user.toSafeObject(),
    },
  });
});

/**
 * Request password reset (Forgot Password)
 * POST /api/auth/forgot-password
 * 
 * Security: Always returns success message regardless of whether email exists
 * This prevents email enumeration attacks
 */
const forgotPassword = asyncHandler(async (req, res) => {
  const { email } = req.body;

  if (!email) {
    throw ApiError.badRequest('กรุณากรอกอีเมล');
  }

  // Always respond with the same message for security
  const successMessage = 'หากอีเมลนี้มีในระบบ เราจะส่งลิงก์สำหรับรีเซ็ตรหัสผ่านไปยังอีเมลของคุณ';

  try {
    // Find user by email (case-insensitive)
    const user = await User.findOne({
      where: {
        email: { [Op.like]: email.toLowerCase() },
        is_active: true,
      },
    });

    if (user) {
      // Generate reset token (expires in 60 minutes)
      const { token, expires_at } = await PasswordResetToken.createForUser(user.id, 60);

      // Build reset URL
      const frontendUrl = process.env.FRONTEND_URL || 'http://localhost:3000';
      const resetUrl = `${frontendUrl}/auth/reset-password?token=${token}`;

      // Send email
      await sendPasswordResetEmail(user.email, resetUrl, user.full_name || user.username);

      // Log the request
      await SystemLog.create({
        log_type: 'auth',
        severity: 'info',
        actor_user_id: user.id,
        action: 'password_reset_requested',
        detail: `Password reset requested for user ${user.username}`,
        ip_address: getClientIp(req),
        user_agent: req.get('User-Agent'),
      });
    } else {
      // Log attempt with non-existent email (for monitoring purposes)
      await SystemLog.create({
        log_type: 'auth',
        severity: 'warn',
        actor_user_id: null,
        action: 'password_reset_nonexistent_email',
        detail: `Password reset attempted for non-existent email: ${email}`,
        ip_address: getClientIp(req),
        user_agent: req.get('User-Agent'),
      });
    }
  } catch (error) {
    // Log error but don't expose it to user
    logger.error('Password reset error:', error);
  }

  // Always return success (prevents email enumeration)
  res.json({
    success: true,
    message: successMessage,
  });
});

/**
 * Validate password reset token
 * POST /api/auth/validate-reset-token
 */
const validateResetToken = asyncHandler(async (req, res) => {
  const { token } = req.body;

  if (!token) {
    throw ApiError.badRequest('Token is required');
  }

  const tokenRecord = await PasswordResetToken.verifyToken(token);

  if (!tokenRecord) {
    throw ApiError.badRequest('ลิงก์รีเซ็ตรหัสผ่านไม่ถูกต้องหรือหมดอายุแล้ว');
  }

  res.json({
    success: true,
    message: 'Token is valid',
    data: {
      valid: true,
    },
  });
});

/**
 * Reset password with token
 * POST /api/auth/reset-password
 */
const resetPassword = asyncHandler(async (req, res) => {
  const { token, newPassword } = req.body;

  if (!token || !newPassword) {
    throw ApiError.badRequest('Token and new password are required');
  }

  if (newPassword.length < 6) {
    throw ApiError.badRequest('รหัสผ่านต้องมีอย่างน้อย 6 ตัวอักษร');
  }

  // Verify token
  const tokenRecord = await PasswordResetToken.verifyToken(token);

  if (!tokenRecord) {
    throw ApiError.badRequest('ลิงก์รีเซ็ตรหัสผ่านไม่ถูกต้องหรือหมดอายุแล้ว');
  }

  // Get user
  const user = await User.findByPk(tokenRecord.user_id);

  if (!user) {
    throw ApiError.notFound('ไม่พบผู้ใช้');
  }

  if (!user.is_active) {
    throw ApiError.forbidden('บัญชีผู้ใช้ถูกปิดการใช้งาน');
  }

  // Update password
  user.password_hash = newPassword; // Will be hashed by beforeUpdate hook
  user.must_change_password = false;
  await user.save();

  // Mark token as used
  await PasswordResetToken.markAsUsed(token);

  // Revoke all refresh tokens for this user (security measure)
  await RefreshToken.update(
    { revoked: true },
    { where: { user_id: user.id, revoked: false } }
  );

  // Log the password reset
  await SystemLog.create({
    log_type: 'auth',
    severity: 'info',
    actor_user_id: user.id,
    action: 'password_reset_completed',
    detail: `Password reset completed for user ${user.username}`,
    ip_address: getClientIp(req),
    user_agent: req.get('User-Agent'),
  });

  res.json({
    success: true,
    message: 'รหัสผ่านถูกเปลี่ยนเรียบร้อยแล้ว กรุณาเข้าสู่ระบบด้วยรหัสผ่านใหม่',
  });
});

module.exports = {
  login,
  refresh,
  logout,
  getMe,
  updateProfile,
  changePassword,
  forceChangePassword,
  googleCallback,
  githubCallback,
  appleCallback,
  getSessions,
  revokeSession,
  revokeAllSessions,
  uploadUserAvatar,
  removeAvatar,
  forgotPassword,
  validateResetToken,
  resetPassword,
};
