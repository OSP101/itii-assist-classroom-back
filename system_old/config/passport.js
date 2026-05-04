const passport = require('passport');
const { Strategy: JwtStrategy, ExtractJwt } = require('passport-jwt');
const LocalStrategy = require('passport-local').Strategy;
const GoogleStrategy = require('passport-google-oauth20').Strategy;
const GitHubStrategy = require('passport-github2').Strategy;
const AppleStrategy = require('passport-apple').Strategy;
const config = require('./index');
const { User, RefreshToken } = require('../models');
const { Op } = require('sequelize');

/**
 * JWT Strategy - for protected routes
 */
const jwtOptions = {
  jwtFromRequest: ExtractJwt.fromAuthHeaderAsBearerToken(),
  secretOrKey: config.jwt.accessSecret,
};

passport.use(
  new JwtStrategy(jwtOptions, async (payload, done) => {
    try {
      if (!payload?.jti) {
        return done(null, false, { message: 'Invalid session token' });
      }

      const [user, session] = await Promise.all([
        User.findByPk(payload.userId),
        RefreshToken.findOne({
          where: {
            jti: payload.jti,
            user_id: payload.userId,
            revoked: false,
            expires_at: {
              [Op.gt]: new Date(),
            },
          },
        }),
      ]);
      
      if (!user) {
        return done(null, false, { message: 'User not found' });
      }

      if (!session) {
        return done(null, false, { message: 'Session revoked or expired' });
      }
      
      if (!user.is_active) {
        return done(null, false, { message: 'Account deactivated' });
      }
      
      return done(null, user);
    } catch (error) {
      return done(error, false);
    }
  })
);

/**
 * Local Strategy - for username/password login
 */
passport.use(
  new LocalStrategy(
    {
      usernameField: 'username',
      passwordField: 'password',
    },
    async (username, password, done) => {
      try {
        const user = await User.findOne({ where: { username } });
        
        if (!user) {
          return done(null, false, { message: 'Invalid Username' });
        }
        
        if (!user.is_active) {
          return done(null, false, { message: 'Account has been deactivated' });
        }
        
        const isMatch = await user.comparePassword(password);
        
        if (!isMatch) {
          return done(null, false, { message: 'Invalid Password' });
        }
        
        return done(null, user);
      } catch (error) {
        return done(error, false);
      }
    }
  )
);

/**
 * Google OAuth Strategy
 */
if (config.google.clientId && config.google.clientSecret) {
  passport.use(
    new GoogleStrategy(
      {
        clientID: config.google.clientId,
        clientSecret: config.google.clientSecret,
        callbackURL: config.google.callbackUrl,
      },
      async (accessToken, refreshToken, profile, done) => {
        try {
          // Lazy load to avoid circular dependency
          const { UserOAuthAccount } = require('../models');
          const { findUserByOAuth } = require('../controllers/oauth.controller');
          
          const email = profile.emails?.[0]?.value;
          const googleId = profile.id;

          // Try to find user by OAuth account (new system)
          const oauthResult = await findUserByOAuth('google', googleId, email);
          
          if (oauthResult && oauthResult.user) {
            if (!oauthResult.user.is_active) {
              return done(null, false, { message: 'Account has been deactivated' });
            }

            // Update provider info if needed
            if (oauthResult.oauthAccount) {
              await oauthResult.oauthAccount.update({
                provider_name: profile.displayName,
                provider_avatar: profile.photos?.[0]?.value,
                provider_email: email,
              });
            }

            return done(null, oauthResult.user);
          }

          // Fallback: Try to find user by old google_id field
          let user = await User.findOne({ where: { google_id: googleId } });
          
          if (user) {
            if (!user.is_active) {
              return done(null, false, { message: 'Account has been deactivated' });
            }

            // Migrate to new OAuth system
            await UserOAuthAccount.findOrCreate({
              where: { user_id: user.id, provider: 'google' },
              defaults: {
                provider_user_id: googleId,
                provider_email: email,
                provider_name: profile.displayName,
                provider_avatar: profile.photos?.[0]?.value,
                linked_at: new Date(),
              },
            });

            return done(null, user);
          }
          
          // Note: We do NOT auto-link by email anymore
          // User must explicitly link via OAuth link flow in profile settings
          // This prevents accidental linking and respects user's unlink action
          
          // No existing user found - pass profile for manual linking
          // Only Admin can create accounts
          return done(null, false, { 
            message: 'No account found with this Google account. Please contact administrator.',
            profile: {
              provider: 'google',
              provider_user_id: googleId,
              provider_email: email,
              provider_name: profile.displayName,
              provider_avatar: profile.photos?.[0]?.value,
            }
          });
          
        } catch (error) {
          return done(error, false);
        }
      }
    )
  );
}

/**
 * GitHub OAuth Strategy
 */
if (config.github.clientId && config.github.clientSecret) {
  passport.use(
    new GitHubStrategy(
      {
        clientID: config.github.clientId,
        clientSecret: config.github.clientSecret,
        callbackURL: config.github.callbackUrl,
        scope: ['user:email'],
      },
      async (accessToken, refreshToken, profile, done) => {
        try {
          // Lazy load to avoid circular dependency
          const { UserOAuthAccount } = require('../models');
          const { findUserByOAuth } = require('../controllers/oauth.controller');
          
          const email = profile.emails?.[0]?.value;
          const githubId = profile.id.toString();

          // Try to find user by OAuth account
          const oauthResult = await findUserByOAuth('github', githubId, email);
          
          if (oauthResult && oauthResult.user) {
            if (!oauthResult.user.is_active) {
              return done(null, false, { message: 'Account has been deactivated' });
            }

            // Update provider info if needed
            if (oauthResult.oauthAccount) {
              await oauthResult.oauthAccount.update({
                provider_name: profile.displayName || profile.username,
                provider_avatar: profile.photos?.[0]?.value,
                provider_email: email,
              });
            }

            return done(null, oauthResult.user);
          }

          // Note: We do NOT auto-link by email anymore
          // User must explicitly link via OAuth link flow in profile settings
          // This prevents accidental linking and respects user's unlink action
          
          // No existing user found - pass profile for manual linking
          return done(null, false, { 
            message: 'No account found with this GitHub account. Please contact administrator.',
            profile: {
              provider: 'github',
              provider_user_id: githubId,
              provider_email: email,
              provider_name: profile.displayName || profile.username,
              provider_avatar: profile.photos?.[0]?.value,
            }
          });
          
        } catch (error) {
          return done(error, false);
        }
      }
    )
  );
}

/**
 * Apple OAuth Strategy
 */
if (config.apple.clientId && config.apple.teamId && config.apple.keyId && 
    (config.apple.privateKeyPath || config.apple.privateKey)) {
  passport.use(
    new AppleStrategy(
      {
        clientID: config.apple.clientId,
        teamID: config.apple.teamId,
        keyID: config.apple.keyId,
        privateKeyLocation: config.apple.privateKeyPath,
        privateKeyString: config.apple.privateKey,
        callbackURL: config.apple.callbackUrl,
        passReqToCallback: true,
      },
      async (req, accessToken, refreshToken, idToken, profile, done) => {
        try {
          // Lazy load to avoid circular dependency
          const { UserOAuthAccount } = require('../models');
          const { findUserByOAuth } = require('../controllers/oauth.controller');
          
          // Apple provides email only on first authorization
          const email = idToken.email || profile?.email;
          const appleId = idToken.sub;

          // Try to find user by OAuth account
          const oauthResult = await findUserByOAuth('apple', appleId, email);
          
          if (oauthResult && oauthResult.user) {
            if (!oauthResult.user.is_active) {
              return done(null, false, { message: 'Account has been deactivated' });
            }

            // Update provider info if needed (Apple names are only sent once)
            if (oauthResult.oauthAccount && email) {
              await oauthResult.oauthAccount.update({
                provider_email: email,
              });
            }

            return done(null, oauthResult.user);
          }

          // Try to find user by email and link Apple account
          if (email) {
            const user = await User.findOne({ where: { email } });
            
            if (user) {
              // Link Apple account to existing user
              const fullName = idToken.name 
                ? `${idToken.name.firstName || ''} ${idToken.name.lastName || ''}`.trim()
                : null;
                
              await UserOAuthAccount.findOrCreate({
                where: { user_id: user.id, provider: 'apple' },
                defaults: {
                  provider_user_id: appleId,
                  provider_email: email,
                  provider_name: fullName,
                  linked_at: new Date(),
                },
              });
              
              if (!user.is_active) {
                return done(null, false, { message: 'Account has been deactivated' });
              }
              
              return done(null, user);
            }
          }
          
          // No existing user found - don't auto-create
          return done(null, false, { 
            message: 'No account found with this Apple ID. Please contact administrator.' 
          });
          
        } catch (error) {
          return done(error, false);
        }
      }
    )
  );
}

module.exports = passport;
