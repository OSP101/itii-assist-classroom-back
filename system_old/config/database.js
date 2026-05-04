const { Sequelize } = require('sequelize');
const config = require('./index');
const logger = require('../utils/logger');

const sequelize = new Sequelize(
  config.db.name,
  config.db.user,
  config.db.password,
  {
    host: config.db.host,
    port: config.db.port,
    dialect: 'mysql',
    logging: config.nodeEnv === 'development' ? console.log : false,
    timezone: '+07:00', // Thailand timezone
    define: {
      timestamps: true,
      underscored: true, // use snake_case for auto-generated fields
      freezeTableName: true, // don't pluralize table names
    },
    pool: {
      max: 25,      // เพิ่มจาก 10 เป็น 25
      min: 5,       // เพิ่มจาก 0 เป็น 5
      acquire: 60000, // เพิ่มจาก 30000 เป็น 60000 (60 วินาที)
      idle: 10000,
    },
  }
);

// Test connection
const testConnection = async () => {
  try {
    await sequelize.authenticate();
    logger.info('✅ Database connection established successfully.');
  } catch (error) {
    logger.error('❌ Unable to connect to the database:', error.message);
    process.exit(1);
  }
};

module.exports = { sequelize, testConnection };
