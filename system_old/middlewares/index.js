module.exports = {
  ...require('./auth'),
  validate: require('./validate'),
  ...require('./errorHandler'),
  ...require('./requestLogger'),
};
