const { ApiError } = require('../utils');

/**
 * Joi validation middleware
 * @param {Object} schema - Joi schema object { body, query, params }
 * @returns {Function} Express middleware
 */
const validate = (schema) => {
  return (req, res, next) => {
    const validationErrors = [];
    
    // Validate body
    if (schema.body) {
      const { error, value } = schema.body.validate(req.body, { abortEarly: false });
      if (error) {
        validationErrors.push(...error.details.map(d => d.message));
      } else {
        req.body = value;
      }
    }
    
    // Validate query
    if (schema.query) {
      const { error, value } = schema.query.validate(req.query, { abortEarly: false });
      if (error) {
        validationErrors.push(...error.details.map(d => d.message));
      } else {
        req.query = value;
      }
    }
    
    // Validate params
    if (schema.params) {
      const { error, value } = schema.params.validate(req.params, { abortEarly: false });
      if (error) {
        validationErrors.push(...error.details.map(d => d.message));
      } else {
        req.params = value;
      }
    }
    
    if (validationErrors.length > 0) {
      return next(ApiError.badRequest(validationErrors.join(', ')));
    }
    
    next();
  };
};

module.exports = validate;
