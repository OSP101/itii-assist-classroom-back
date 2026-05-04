const Joi = require('joi');

const login = {
  body: Joi.object({
    username: Joi.string().required().messages({
      'string.empty': 'Username is required',
      'any.required': 'Username is required',
    }),
    password: Joi.string().required().messages({
      'string.empty': 'Password is required',
      'any.required': 'Password is required',
    }),
  }),
};

const refreshToken = {
  body: Joi.object({
    refreshToken: Joi.string().required().messages({
      'string.empty': 'Refresh token is required',
      'any.required': 'Refresh token is required',
    }),
  }),
};

const changePassword = {
  body: Joi.object({
    currentPassword: Joi.string().required().messages({
      'string.empty': 'Current password is required',
      'any.required': 'Current password is required',
    }),
    newPassword: Joi.string().min(6).required().messages({
      'string.empty': 'New password is required',
      'string.min': 'New password must be at least 6 characters',
      'any.required': 'New password is required',
    }),
    confirmPassword: Joi.string().valid(Joi.ref('newPassword')).required().messages({
      'any.only': 'Passwords do not match',
      'any.required': 'Confirm password is required',
    }),
  }),
};

const updateProfile = {
  body: Joi.object({
    full_name: Joi.string().max(255).allow('').optional().messages({
      'string.max': 'Full name must be less than 255 characters',
    }),
    email: Joi.string().email().allow('', null).optional().messages({
      'string.email': 'Invalid email format',
    }),
    current_password: Joi.string().required().messages({
      'any.required': 'Current password is required',
      'string.empty': 'Current password is required',
    }),
  }),
};

module.exports = {
  login,
  refreshToken,
  changePassword,
  updateProfile,
};
