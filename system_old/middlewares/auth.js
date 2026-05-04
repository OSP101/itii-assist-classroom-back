const passport = require('passport');
const { ApiError } = require('../utils');
const { User, RefreshToken } = require('../models');

/**
 * Authentication middleware using JWT
 * @param {Object} req - Express request
 * @param {Object} res - Express response
 * @param {Function} next - Next middleware
 */
const authenticate = (req, res, next) => {
  passport.authenticate('jwt', { session: false }, (err, user, info) => {
    if (err) {
      return next(err);
    }
    
    if (!user) {
      const message = info?.message || 'Unauthorized - Invalid or expired token';
      return next(ApiError.unauthorized(message));
    }
    
    if (!user.is_active) {
      return next(ApiError.forbidden('Account has been deactivated'));
    }
    
    req.user = user;
    next();
  })(req, res, next);
};

/**
 * Role-based authorization middleware
 * @param  {...string} roles - Allowed roles
 * @returns {Function} Express middleware
 */
const authorize = (...roles) => {
  return (req, res, next) => {
    if (!req.user) {
      return next(ApiError.unauthorized('Authentication required'));
    }
    
    if (!roles.includes(req.user.role)) {
      return next(ApiError.forbidden(`Access denied. Required roles: ${roles.join(', ')}`));
    }
    
    next();
  };
};

/**
 * Optional authentication - doesn't fail if no token
 */
const optionalAuth = (req, res, next) => {
  passport.authenticate('jwt', { session: false }, (err, user) => {
    if (user) {
      req.user = user;
    }
    next();
  })(req, res, next);
};

/**
 * Admin only middleware
 */
const isAdmin = (req, res, next) => {
  if (!req.user) {
    return next(ApiError.unauthorized('Authentication required'));
  }
  
  if (req.user.role !== 'admin') {
    return next(ApiError.forbidden('Access denied. Admin only.'));
  }
  
  next();
};

/**
 * Check if the course is active before allowing write operations
 * Looks for courseId from multiple sources: params, query, body
 * If course is inactive (is_active === false), returns 403 error
 */
const checkCourseActive = async (req, res, next) => {
  try {
    const courseId = req.params.id || req.params.courseId ||
                     req.query.course_id || req.body.course_id ||
                     req.query.courseId || req.body.courseId;

    if (!courseId) {
      return next(); // No courseId found, let controller handle validation
    }

    const { Course } = require('../models');
    const course = await Course.findByPk(courseId, { attributes: ['id', 'is_active'] });

    if (course && !course.is_active) {
      return next(ApiError.forbidden('รายวิชานี้ปิดใช้งานอยู่ กรุณาเปิดใช้งานก่อนทำการแก้ไข'));
    }

    next();
  } catch (error) {
    next(error);
  }
};

module.exports = {
  authenticate,
  authorize,
  optionalAuth,
  isAdmin,
  checkCourseActive,
};
