/**
 * Classroom Controller - Handle classroom and desk management
 */

const { Classroom, Desk, Zone, User } = require('../models');
const { Op } = require('sequelize');
const { sequelize } = require('../config/database');
const ApiError = require('../utils/ApiError');
const asyncHandler = require('../utils/asyncHandler');

/**
 * Get all classrooms with pagination and filters
 * @route GET /api/classrooms
 */
const getClassrooms = asyncHandler(async (req, res) => {
  const {
    page = 1,
    limit = 10,
    search = '',
    building = '',
    showDeleted = 'false',
    sortBy = 'created_at',
    sortOrder = 'DESC',
  } = req.query;

  // Build where clause
  const where = {};

  // Show deleted or not
  if (showDeleted === 'true') {
    where.is_deleted = true;
  } else if (showDeleted === 'all') {
    // Show all
  } else {
    where.is_deleted = false;
  }

  // Search filter
  if (search) {
    where[Op.or] = [
      { name: { [Op.like]: `%${search}%` } },
      { building: { [Op.like]: `%${search}%` } },
      { floor: { [Op.like]: `%${search}%` } },
    ];
  }

  // Building filter
  if (building) {
    where.building = building;
  }

  // Calculate offset
  const offset = (parseInt(page) - 1) * parseInt(limit);

  // Valid sort columns
  const validSortColumns = ['name', 'building', 'floor', 'created_at', 'updated_at'];
  const orderColumn = validSortColumns.includes(sortBy) ? sortBy : 'created_at';
  const orderDirection = sortOrder.toUpperCase() === 'ASC' ? 'ASC' : 'DESC';

  // Query classrooms
  const { count, rows: classrooms } = await Classroom.findAndCountAll({
    where,
    limit: parseInt(limit),
    offset,
    order: [[orderColumn, orderDirection]],
    include: [
      {
        model: Desk,
        as: 'desks',
        attributes: ['id', 'number', 'x', 'y', 'type', 'is_enabled'],
      },
      {
        model: Zone,
        as: 'zones',
        attributes: ['id', 'name', 'x', 'y', 'width', 'height', 'color'],
      },
      {
        model: User,
        as: 'creator',
        attributes: ['id', 'full_name', 'email'],
      },
    ],
  });

  // Calculate pagination info
  const totalPages = Math.ceil(count / parseInt(limit));

  res.json({
    success: true,
    data: {
      classrooms,
      pagination: {
        total: count,
        page: parseInt(page),
        limit: parseInt(limit),
        totalPages,
      },
    },
  });
});

/**
 * Get a single classroom by ID
 * @route GET /api/classrooms/:id
 */
const getClassroomById = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const classroom = await Classroom.findByPk(id, {
    include: [
      {
        model: Desk,
        as: 'desks',
        attributes: ['id', 'number', 'x', 'y', 'type', 'is_enabled'],
        order: [['number', 'ASC']],
      },
      {
        model: Zone,
        as: 'zones',
        attributes: ['id', 'name', 'x', 'y', 'width', 'height', 'color'],
      },
      {
        model: User,
        as: 'creator',
        attributes: ['id', 'full_name', 'email'],
      },
    ],
  });

  if (!classroom) {
    throw new ApiError(404, 'ไม่พบห้องเรียนที่ต้องการ');
  }

  res.json({
    success: true,
    data: classroom,
  });
});

/**
 * Create a new classroom
 * @route POST /api/classrooms
 */
const createClassroom = asyncHandler(async (req, res) => {
  const { name, building, floor, description } = req.body;

  // Validate required fields
  if (!name || !building || !floor) {
    throw new ApiError(400, 'กรุณากรอกข้อมูลให้ครบถ้วน');
  }

  // Create classroom
  const classroom = await Classroom.create({
    name,
    building,
    floor,
    description,
    created_by: req.user?.id || null,
  });

  res.status(201).json({
    success: true,
    message: 'สร้างห้องเรียนเรียบร้อยแล้ว',
    data: classroom,
  });
});

/**
 * Update classroom info
 * @route PUT /api/classrooms/:id
 */
const updateClassroom = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { name, building, floor, description } = req.body;

  const classroom = await Classroom.findByPk(id);

  if (!classroom) {
    throw new ApiError(404, 'ไม่พบห้องเรียนที่ต้องการ');
  }

  // Update fields
  if (name !== undefined) classroom.name = name;
  if (building !== undefined) classroom.building = building;
  if (floor !== undefined) classroom.floor = floor;
  if (description !== undefined) classroom.description = description;

  await classroom.save();

  res.json({
    success: true,
    message: 'อัพเดทห้องเรียนเรียบร้อยแล้ว',
    data: classroom,
  });
});

/**
 * Soft delete a classroom
 * @route DELETE /api/classrooms/:id
 */
const deleteClassroom = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { permanent = false } = req.query;

  const classroom = await Classroom.findByPk(id);

  if (!classroom) {
    throw new ApiError(404, 'ไม่พบห้องเรียนที่ต้องการ');
  }

  if (permanent === 'true') {
    // Permanent delete (desks will be cascade deleted)
    await classroom.destroy();
    res.json({
      success: true,
      message: 'ลบห้องเรียนถาวรเรียบร้อยแล้ว',
    });
  } else {
    // Soft delete
    classroom.is_deleted = true;
    await classroom.save();
    res.json({
      success: true,
      message: 'ลบห้องเรียนเรียบร้อยแล้ว',
    });
  }
});

/**
 * Restore a soft-deleted classroom
 * @route POST /api/classrooms/:id/restore
 */
const restoreClassroom = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const classroom = await Classroom.findByPk(id);

  if (!classroom) {
    throw new ApiError(404, 'ไม่พบห้องเรียนที่ต้องการ');
  }

  if (!classroom.is_deleted) {
    throw new ApiError(400, 'ห้องเรียนนี้ไม่ได้ถูกลบ');
  }

  classroom.is_deleted = false;
  await classroom.save();

  res.json({
    success: true,
    message: 'กู้คืนห้องเรียนเรียบร้อยแล้ว',
    data: classroom,
  });
});

/**
 * Update classroom layout (desks)
 * @route PUT /api/classrooms/:id/layout
 */
const updateLayout = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { desks, zones } = req.body;

  const classroom = await Classroom.findByPk(id);

  if (!classroom) {
    throw new ApiError(404, 'ไม่พบห้องเรียนที่ต้องการ');
  }

  if (!Array.isArray(desks)) {
    throw new ApiError(400, 'ข้อมูลโต๊ะไม่ถูกต้อง');
  }

  // Use transaction
  const transaction = await sequelize.transaction();

  try {
    // ============ DESKS ============
    // Get existing desks
    const existingDesks = await Desk.findAll({
      where: { classroom_id: id },
      transaction,
    });

    const existingDeskIds = existingDesks.map(d => d.id);
    const newDeskIds = desks.filter(d => d.id && !d.id.startsWith('desk_')).map(d => d.id);

    // Delete removed desks
    const desksToDelete = existingDeskIds.filter(id => !newDeskIds.includes(id));
    if (desksToDelete.length > 0) {
      await Desk.destroy({
        where: { id: desksToDelete },
        transaction,
      });
    }

    // Update or create desks
    for (const desk of desks) {
      if (desk.id && existingDeskIds.includes(desk.id)) {
        // Update existing desk
        await Desk.update(
          {
            number: desk.number,
            x: desk.x,
            y: desk.y,
            type: desk.type,
            is_enabled: desk.isEnabled,
          },
          {
            where: { id: desk.id },
            transaction,
          }
        );
      } else {
        // Create new desk
        await Desk.create(
          {
            classroom_id: id,
            number: desk.number,
            x: desk.x,
            y: desk.y,
            type: desk.type,
            is_enabled: desk.isEnabled !== false,
          },
          { transaction }
        );
      }
    }

    // ============ ZONES ============
    if (Array.isArray(zones)) {
      // Get existing zones
      const existingZones = await Zone.findAll({
        where: { classroom_id: id },
        transaction,
      });

      const existingZoneIds = existingZones.map(z => z.id);
      const newZoneIds = zones.filter(z => z.id && !z.id.startsWith('zone_')).map(z => z.id);

      // Delete removed zones
      const zonesToDelete = existingZoneIds.filter(zid => !newZoneIds.includes(zid));
      if (zonesToDelete.length > 0) {
        await Zone.destroy({
          where: { id: zonesToDelete },
          transaction,
        });
      }

      // Update or create zones
      for (const zone of zones) {
        if (zone.id && existingZoneIds.includes(zone.id)) {
          // Update existing zone
          await Zone.update(
            {
              name: zone.name,
              x: zone.x,
              y: zone.y,
              width: zone.width,
              height: zone.height,
              color: zone.color,
            },
            {
              where: { id: zone.id },
              transaction,
            }
          );
        } else {
          // Create new zone
          await Zone.create(
            {
              classroom_id: id,
              name: zone.name,
              x: zone.x || 0,
              y: zone.y || 0,
              width: zone.width || 400,
              height: zone.height || 300,
              color: zone.color || 'rgba(99,102,241,0.15)',
            },
            { transaction }
          );
        }
      }
    }

    await transaction.commit();

    // Fetch updated classroom with desks and zones
    const updatedClassroom = await Classroom.findByPk(id, {
      include: [
        {
          model: Desk,
          as: 'desks',
          attributes: ['id', 'number', 'x', 'y', 'type', 'is_enabled'],
        },
        {
          model: Zone,
          as: 'zones',
          attributes: ['id', 'name', 'x', 'y', 'width', 'height', 'color'],
        },
      ],
    });

    res.json({
      success: true,
      message: 'บันทึกผังห้องเรียบร้อยแล้ว',
      data: updatedClassroom,
    });
  } catch (error) {
    await transaction.rollback();
    throw error;
  }
});

/**
 * Toggle classroom active status
 * @route PATCH /api/classrooms/:id/toggle-status
 */
const toggleStatus = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const classroom = await Classroom.findByPk(id);
  if (!classroom) {
    res.status(404);
    throw new Error('ไม่พบห้องเรียนที่ระบุ');
  }

  // Toggle the is_active status
  classroom.is_active = !classroom.is_active;
  await classroom.save();

  res.json({
    success: true,
    message: classroom.is_active ? 'เปิดใช้งานห้องเรียนแล้ว' : 'ปิดใช้งานห้องเรียนแล้ว',
    data: classroom,
  });
});

/**
 * Get classroom statistics
 * @route GET /api/classrooms/stats
 * Optimized: Uses Promise.all for parallel queries
 */
const getStats = asyncHandler(async (req, res) => {
  // Execute all count queries in parallel for better performance
  const [
    totalClassrooms,
    deletedClassrooms,
    totalDesks,
    computerDesks,
    normalDesks,
    teacherDesks,
    enabledDesks,
    buildings,
  ] = await Promise.all([
    // Classroom counts
    Classroom.count({ where: { is_deleted: false } }),
    Classroom.count({ where: { is_deleted: true } }),
    
    // Desk counts - use subquery approach for better performance
    Desk.count({
      include: [{
        model: Classroom,
        as: 'classroom',
        where: { is_deleted: false },
        attributes: [],
        required: true,
      }],
    }),
    Desk.count({
      where: { type: 'computer' },
      include: [{
        model: Classroom,
        as: 'classroom',
        where: { is_deleted: false },
        attributes: [],
        required: true,
      }],
    }),
    Desk.count({
      where: { type: 'normal' },
      include: [{
        model: Classroom,
        as: 'classroom',
        where: { is_deleted: false },
        attributes: [],
        required: true,
      }],
    }),
    Desk.count({
      where: { type: 'teacher' },
      include: [{
        model: Classroom,
        as: 'classroom',
        where: { is_deleted: false },
        attributes: [],
        required: true,
      }],
    }),
    Desk.count({
      where: { is_enabled: true },
      include: [{
        model: Classroom,
        as: 'classroom',
        where: { is_deleted: false },
        attributes: [],
        required: true,
      }],
    }),
    
    // Unique buildings
    Classroom.findAll({
      where: { is_deleted: false },
      attributes: [[sequelize.fn('DISTINCT', sequelize.col('building')), 'building']],
      raw: true,
    }),
  ]);

  res.json({
    success: true,
    data: {
      totalClassrooms,
      deletedClassrooms,
      totalDesks,
      computerDesks,
      normalDesks,
      teacherDesks,
      enabledDesks,
      disabledDesks: totalDesks - enabledDesks,
      buildings: buildings.map(b => b.building),
    },
  });
});

module.exports = {
  getClassrooms,
  getClassroomById,
  createClassroom,
  updateClassroom,
  deleteClassroom,
  restoreClassroom,
  updateLayout,
  toggleStatus,
  getStats,
};
