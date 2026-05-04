const { UserOAuthAccount, User } = require('../models');
const { ApiError, logger } = require('../utils');

/**
 * Check if the user_oauth_accounts table exists
 */
const tableExists = async () => {
  try {
    await UserOAuthAccount.describe();
    return true;
  } catch (error) {
    return false;
  }
};

/**
 * Get all linked OAuth accounts for the current user
 */
const getLinkedAccounts = async (req, res) => {
  try {
    const userId = req.user.id;

    // Check if table exists
    if (!await tableExists()) {
      return res.json({
        success: true,
        data: [],
      });
    }

    const accounts = await UserOAuthAccount.findAll({
      where: { user_id: userId },
      attributes: ['id', 'provider', 'provider_email', 'provider_name', 'provider_avatar', 'linked_at'],
      order: [['linked_at', 'ASC']],
    });

    res.json({
      success: true,
      data: accounts.map(a => a.toSafeObject()),
    });
  } catch (error) {
    logger.error('Get linked accounts error:', error);
    // If table doesn't exist, return empty array
    if (error.name === 'SequelizeDatabaseError' && error.parent?.code === 'ER_NO_SUCH_TABLE') {
      return res.json({
        success: true,
        data: [],
      });
    }
    res.status(500).json({
      success: false,
      error: 'Failed to get linked accounts',
    });
  }
};

/**
 * Link a new OAuth provider account
 * This is called after OAuth flow when user explicitly links an account
 */
const linkAccount = async (req, res) => {
  try {
    // Check if table exists
    if (!await tableExists()) {
      throw new ApiError(503, 'OAuth feature is not yet available. Please run database migration.');
    }

    const userId = req.user.id;
    const { provider, provider_user_id, provider_email, provider_name, provider_avatar } = req.body;

    if (!provider || !provider_user_id) {
      throw new ApiError(400, 'Provider and provider_user_id are required');
    }

    const validProviders = ['google', 'github', 'apple'];
    if (!validProviders.includes(provider)) {
      throw new ApiError(400, 'Invalid provider');
    }

    // Check if this provider account is already linked to another user
    const existingLink = await UserOAuthAccount.findOne({
      where: { provider, provider_user_id },
    });

    if (existingLink) {
      if (existingLink.user_id === userId) {
        throw new ApiError(400, 'This account is already linked to your profile');
      } else {
        throw new ApiError(400, 'This account is already linked to another user');
      }
    }

    // Check if user already has this provider linked
    const userProviderLink = await UserOAuthAccount.findOne({
      where: { user_id: userId, provider },
    });

    if (userProviderLink) {
      throw new ApiError(400, `You already have a ${provider} account linked`);
    }

    // Create the link
    const oauthAccount = await UserOAuthAccount.create({
      user_id: userId,
      provider,
      provider_user_id,
      provider_email,
      provider_name,
      provider_avatar,
      linked_at: new Date(),
    });

    logger.info(`User ${userId} linked ${provider} account: ${provider_user_id}`);

    res.json({
      success: true,
      message: `${provider} account linked successfully`,
      data: oauthAccount.toSafeObject(),
    });
  } catch (error) {
    if (error instanceof ApiError) {
      return res.status(error.statusCode).json({
        success: false,
        error: error.message,
      });
    }
    logger.error('Link account error:', error);
    res.status(500).json({
      success: false,
      error: 'Failed to link account',
    });
  }
};

/**
 * Unlink an OAuth provider account
 */
const unlinkAccount = async (req, res) => {
  try {
    // Check if table exists
    if (!await tableExists()) {
      throw new ApiError(503, 'OAuth feature is not yet available. Please run database migration.');
    }

    const userId = req.user.id;
    const { provider } = req.params;

    const validProviders = ['google', 'github', 'apple'];
    if (!validProviders.includes(provider)) {
      throw new ApiError(400, 'Invalid provider');
    }

    const oauthAccount = await UserOAuthAccount.findOne({
      where: { user_id: userId, provider },
    });

    if (!oauthAccount) {
      throw new ApiError(404, `No ${provider} account is linked`);
    }

    // Get user to check if they have other auth methods
    const user = await User.findByPk(userId);
    
    // Check if user has a password set (local auth)
    const hasPassword = user.password_hash && user.password_hash !== '';
    
    // Count remaining OAuth accounts
    const remainingOAuth = await UserOAuthAccount.count({
      where: { user_id: userId },
    });

    // Don't allow unlinking if it's the only auth method
    if (!hasPassword && remainingOAuth <= 1) {
      throw new ApiError(400, 'Cannot unlink the only authentication method. Please set a password first.');
    }

    await oauthAccount.destroy();

    // Also clear the old provider field on User table (legacy support)
    if (provider === 'google' && user.google_id) {
      user.google_id = null;
      await user.save();
    }
    // Note: github_id field doesn't exist in old schema, only in new OAuth table

    logger.info(`User ${userId} unlinked ${provider} account`);

    res.json({
      success: true,
      message: `${provider} account unlinked successfully`,
    });
  } catch (error) {
    if (error instanceof ApiError) {
      return res.status(error.statusCode).json({
        success: false,
        error: error.message,
      });
    }
    logger.error('Unlink account error:', error);
    res.status(500).json({
      success: false,
      error: 'Failed to unlink account',
    });
  }
};

/**
 * Admin: Link OAuth account to a user by email match
 * This is used when admin creates a user with an email - auto-link to Google
 */
const adminLinkByEmail = async (userId, email, provider = 'google') => {
  try {
    if (!email) return null;

    // Check if table exists
    if (!await tableExists()) {
      logger.warn('user_oauth_accounts table does not exist, skipping OAuth link');
      return null;
    }

    // Create a placeholder link using email as the provider_user_id
    // When the user actually logs in with OAuth, this will be updated with the real provider_user_id
    const existingLink = await UserOAuthAccount.findOne({
      where: { user_id: userId, provider },
    });

    if (existingLink) {
      return existingLink;
    }

    const oauthAccount = await UserOAuthAccount.create({
      user_id: userId,
      provider,
      provider_user_id: `email:${email}`, // Placeholder until real OAuth login
      provider_email: email,
      linked_at: new Date(),
    });

    logger.info(`Admin linked ${provider} account to user ${userId} via email: ${email}`);

    return oauthAccount;
  } catch (error) {
    logger.error('Admin link by email error:', error);
    return null;
  }
};

/**
 * Find user by OAuth provider info
 * Used during OAuth login flow
 */
const findUserByOAuth = async (provider, providerUserId, providerEmail = null) => {
  try {
    // Check if table exists
    if (!await tableExists()) {
      return null;
    }

    // First try exact match by provider_user_id
    let oauthAccount = await UserOAuthAccount.findOne({
      where: { provider, provider_user_id: providerUserId },
      include: [{ model: User, as: 'user' }],
    });

    if (oauthAccount) {
      return { user: oauthAccount.user, oauthAccount };
    }

    // If not found and email is provided, try to find by email placeholder
    if (providerEmail) {
      oauthAccount = await UserOAuthAccount.findOne({
        where: { 
          provider, 
          provider_user_id: `email:${providerEmail}`,
        },
        include: [{ model: User, as: 'user' }],
      });

      if (oauthAccount) {
        // Update with real provider_user_id
        await oauthAccount.update({
          provider_user_id: providerUserId,
          provider_email: providerEmail,
        });
        
        return { user: oauthAccount.user, oauthAccount };
      }
    }

    return null;
  } catch (error) {
    logger.error('Find user by OAuth error:', error);
    return null;
  }
};

/**
 * Get OAuth accounts for a user (Admin view)
 */
const getAccountsForUser = async (req, res) => {
  try {
    const { userId } = req.params;

    // Check if table exists
    if (!await tableExists()) {
      return res.json({
        success: true,
        data: [],
      });
    }

    const accounts = await UserOAuthAccount.findAll({
      where: { user_id: userId },
      attributes: ['id', 'provider', 'provider_email', 'provider_name', 'linked_at'],
      order: [['linked_at', 'ASC']],
    });

    res.json({
      success: true,
      data: accounts,
    });
  } catch (error) {
    logger.error('Get accounts for user error:', error);
    res.status(500).json({
      success: false,
      error: 'Failed to get accounts',
    });
  }
};

/**
 * Admin: Unlink OAuth account from a user
 */
const adminUnlinkAccount = async (req, res) => {
  try {
    const { userId, provider } = req.params;

    const oauthAccount = await UserOAuthAccount.findOne({
      where: { user_id: userId, provider },
    });

    if (!oauthAccount) {
      throw new ApiError(404, `No ${provider} account is linked to this user`);
    }

    await oauthAccount.destroy();

    logger.info(`Admin unlinked ${provider} account from user ${userId}`);

    res.json({
      success: true,
      message: `${provider} account unlinked successfully`,
    });
  } catch (error) {
    if (error instanceof ApiError) {
      return res.status(error.statusCode).json({
        success: false,
        error: error.message,
      });
    }
    logger.error('Admin unlink account error:', error);
    res.status(500).json({
      success: false,
      error: 'Failed to unlink account',
    });
  }
};

module.exports = {
  getLinkedAccounts,
  linkAccount,
  unlinkAccount,
  adminLinkByEmail,
  findUserByOAuth,
  getAccountsForUser,
  adminUnlinkAccount,
};
