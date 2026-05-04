/**
 * Seed script to create/update admin user
 * Run: node src/seeds/admin.seed.js
 */

require('dotenv').config();
const bcrypt = require('bcryptjs');
const { sequelize } = require('../config/database');
const User = require('../models/User');

const createAdmin = async () => {
  try {
    // Test database connection
    await sequelize.authenticate();
    console.log('✅ Database connected');

    const adminData = {
      username: 'admin',
      password: 'osp101@admin',
      role: 'admin',
      full_name: 'Administrator',
      email: 'admin@osp101.com',
      provider: 'local',
      is_active: true,
    };

    // Hash password
    const salt = await bcrypt.genSalt(10);
    const password_hash = await bcrypt.hash(adminData.password, salt);

    // Check if admin exists
    const existingAdmin = await User.findOne({ 
      where: { username: adminData.username } 
    });

    if (existingAdmin) {
      // Update existing admin
      await existingAdmin.update({
        password_hash,
        full_name: adminData.full_name,
        email: adminData.email,
        is_active: adminData.is_active,
      });
      console.log('✅ Admin user updated successfully!');
    } else {
      // Create new admin
      await User.create({
        username: adminData.username,
        password_hash,
        role: adminData.role,
        full_name: adminData.full_name,
        email: adminData.email,
        provider: adminData.provider,
        is_active: adminData.is_active,
      });
      console.log('✅ Admin user created successfully!');
    }

    console.log('');
    console.log('📋 Admin Credentials:');
    console.log('   Username: admin');
    console.log('   Password: osp101@admin');
    console.log('');

    process.exit(0);
  } catch (error) {
    console.error('❌ Error:', error.message);
    process.exit(1);
  }
};

createAdmin();
