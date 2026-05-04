const { Op } = require('sequelize');
const {
  StudentGroup,
  StudentGroupMember,
  Student,
  Course,
  CourseTA,
  sequelize,
} = require('../models');
const { asyncHandler, ApiError } = require('../utils');

/**
 * Helper function to check if user has access to course
 */
const checkCourseAccess = async (courseId, user) => {
  if (user.role === 'admin') return true;
  
  const course = await Course.findByPk(courseId);
  if (!course) return false;
  
  // Check if user is instructor
  if (course.instructor_id === user.id) return true;
  
  // Check if user is TA
  const taAssignment = await CourseTA.findOne({
    where: { course_id: courseId, user_id: user.id }
  });
  return !!taAssignment;
};

/**
 * Get all teams for a course
 * @route GET /api/courses/:id/teams
 */
const getTeams = asyncHandler(async (req, res) => {
  const { id: courseId } = req.params;
  const { type, week } = req.query;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const whereClause = { course_id: courseId };
  
  if (type === 'permanent') {
    whereClause.group_type = 'permanent';
  } else if (type === 'temporary' || type === 'weekly') {
    whereClause.group_type = 'temporary';
    if (week) {
      whereClause.week_number = parseInt(week);
    }
  }

  const teams = await StudentGroup.findAll({
    where: whereClause,
    include: [
      {
        model: Student,
        as: 'members',
        attributes: ['id', 'student_id', 'full_name', 'email'],
        through: { attributes: ['joined_at'] },
      },
    ],
    order: [['id', 'ASC']],  // Sort by ID to maintain creation order (especially for bulk-created teams)
  });

  // Transform data
  const transformedTeams = teams.map(team => ({
    id: team.id,
    name: team.name,
    group_type: team.group_type,
    week_number: team.week_number,
    members: team.members.map(m => ({
      id: m.id,
      student_id: m.student_id,
      full_name: m.full_name,
      email: m.email,
      joined_at: m.StudentGroupMember?.joined_at,
    })),
    created_at: team.created_at,
  }));

  res.json({
    success: true,
    data: transformedTeams,
  });
});

/**
 * Create a new team
 * @route POST /api/courses/:id/teams
 */
const createTeam = asyncHandler(async (req, res) => {
  const { id: courseId } = req.params;
  const { name, group_type, week_number, member_ids } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  if (!name || !name.trim()) {
    throw new ApiError(400, 'กรุณาระบุชื่อกลุ่ม');
  }

  if (!member_ids || !Array.isArray(member_ids) || member_ids.length === 0) {
    throw new ApiError(400, 'กรุณาเลือกสมาชิกอย่างน้อย 1 คน');
  }

  const transaction = await sequelize.transaction();
  
  try {
    // Create the team
    const team = await StudentGroup.create({
      course_id: courseId,
      name: name.trim(),
      group_type: group_type || 'permanent',
      week_number: group_type === 'temporary' ? week_number : null,
    }, { transaction });

    // Add members
    const memberRecords = member_ids.map(studentId => ({
      group_id: team.id,
      student_id: studentId,
    }));

    await StudentGroupMember.bulkCreate(memberRecords, { 
      transaction,
      ignoreDuplicates: true,
    });

    await transaction.commit();

    // Fetch the created team with members
    const createdTeam = await StudentGroup.findByPk(team.id, {
      include: [
        {
          model: Student,
          as: 'members',
          attributes: ['id', 'student_id', 'full_name', 'email'],
        },
      ],
    });

    res.status(201).json({
      success: true,
      message: 'สร้างกลุ่มสำเร็จ',
      data: {
        id: createdTeam.id,
        name: createdTeam.name,
        group_type: createdTeam.group_type,
        week_number: createdTeam.week_number,
        members: createdTeam.members.map(m => ({
          id: m.id,
          student_id: m.student_id,
          full_name: m.full_name,
        })),
        created_at: createdTeam.created_at,
      },
    });
  } catch (error) {
    await transaction.rollback();
    throw error;
  }
});

/**
 * Bulk create teams (for random team formation)
 * @route POST /api/courses/:id/teams/bulk
 */
const bulkCreateTeams = asyncHandler(async (req, res) => {
  const { id: courseId } = req.params;
  const { teams, group_type, week_number } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  if (!teams || !Array.isArray(teams) || teams.length === 0) {
    throw new ApiError(400, 'กรุณาระบุข้อมูลกลุ่ม');
  }

  const transaction = await sequelize.transaction();
  
  try {
    const createdTeams = [];

    for (const teamData of teams) {
      const { name, member_ids } = teamData;
      
      if (!name || !member_ids || member_ids.length === 0) continue;

      // Create the team
      const team = await StudentGroup.create({
        course_id: courseId,
        name: name.trim(),
        group_type: group_type || 'permanent',
        week_number: group_type === 'temporary' ? week_number : null,
      }, { transaction });

      // Add members
      const memberRecords = member_ids.map(studentId => ({
        group_id: team.id,
        student_id: studentId,
      }));

      await StudentGroupMember.bulkCreate(memberRecords, { 
        transaction,
        ignoreDuplicates: true,
      });

      createdTeams.push({
        id: team.id,
        name: team.name,
        memberCount: member_ids.length,
      });
    }

    await transaction.commit();

    res.status(201).json({
      success: true,
      message: `สร้างกลุ่มสำเร็จ ${createdTeams.length} กลุ่ม`,
      data: {
        createdCount: createdTeams.length,
        teams: createdTeams,
      },
    });
  } catch (error) {
    await transaction.rollback();
    throw error;
  }
});

/**
 * Update a team
 * @route PUT /api/courses/:id/teams/:teamId
 */
const updateTeam = asyncHandler(async (req, res) => {
  const { id: courseId, teamId } = req.params;
  const { name, member_ids } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const team = await StudentGroup.findOne({
    where: { id: teamId, course_id: courseId },
  });

  if (!team) {
    throw new ApiError(404, 'ไม่พบกลุ่ม');
  }

  const transaction = await sequelize.transaction();
  
  try {
    // Update team name if provided
    if (name && name.trim()) {
      team.name = name.trim();
      await team.save({ transaction });
    }

    // Update members if provided
    if (member_ids && Array.isArray(member_ids)) {
      // Remove existing members
      await StudentGroupMember.destroy({
        where: { group_id: teamId },
        transaction,
      });

      // Add new members
      if (member_ids.length > 0) {
        const memberRecords = member_ids.map(studentId => ({
          group_id: teamId,
          student_id: studentId,
        }));

        await StudentGroupMember.bulkCreate(memberRecords, { 
          transaction,
          ignoreDuplicates: true,
        });
      }
    }

    await transaction.commit();

    // Fetch updated team
    const updatedTeam = await StudentGroup.findByPk(teamId, {
      include: [
        {
          model: Student,
          as: 'members',
          attributes: ['id', 'student_id', 'full_name', 'email'],
        },
      ],
    });

    res.json({
      success: true,
      message: 'อัพเดทกลุ่มสำเร็จ',
      data: {
        id: updatedTeam.id,
        name: updatedTeam.name,
        group_type: updatedTeam.group_type,
        week_number: updatedTeam.week_number,
        members: updatedTeam.members.map(m => ({
          id: m.id,
          student_id: m.student_id,
          full_name: m.full_name,
        })),
      },
    });
  } catch (error) {
    await transaction.rollback();
    throw error;
  }
});

/**
 * Bulk delete teams
 * @route POST /api/courses/:id/teams/bulk-delete
 */
const bulkDeleteTeams = asyncHandler(async (req, res) => {
  const { id: courseId } = req.params;
  const { team_ids } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  if (!team_ids || !Array.isArray(team_ids) || team_ids.length === 0) {
    throw new ApiError(400, 'กรุณาระบุรายการกลุ่มที่ต้องการลบ');
  }

  const transaction = await sequelize.transaction();
  
  try {
    // Delete members first
    await StudentGroupMember.destroy({
      where: { group_id: team_ids },
      transaction,
    });

    // Delete teams
    const deletedCount = await StudentGroup.destroy({
      where: { 
        id: team_ids,
        course_id: courseId 
      },
      transaction,
    });

    await transaction.commit();

    res.json({
      success: true,
      message: `ลบกลุ่มสำเร็จ ${deletedCount} กลุ่ม`,
      data: {
        deletedCount,
      },
    });
  } catch (error) {
    await transaction.rollback();
    throw error;
  }
});

/**
 * Delete a team
 * @route DELETE /api/courses/:id/teams/:teamId
 */
const deleteTeam = asyncHandler(async (req, res) => {
  const { id: courseId, teamId } = req.params;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const team = await StudentGroup.findOne({
    where: { id: teamId, course_id: courseId },
  });

  if (!team) {
    throw new ApiError(404, 'ไม่พบกลุ่ม');
  }

  const transaction = await sequelize.transaction();
  
  try {
    // Delete members first
    await StudentGroupMember.destroy({
      where: { group_id: teamId },
      transaction,
    });

    // Delete team
    await team.destroy({ transaction });

    await transaction.commit();

    res.json({
      success: true,
      message: 'ลบกลุ่มสำเร็จ',
    });
  } catch (error) {
    await transaction.rollback();
    throw error;
  }
});

/**
 * Add member to team
 * @route POST /api/courses/:id/teams/:teamId/members
 */
const addMemberToTeam = asyncHandler(async (req, res) => {
  const { id: courseId, teamId } = req.params;
  const { student_id } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const team = await StudentGroup.findOne({
    where: { id: teamId, course_id: courseId },
  });

  if (!team) {
    throw new ApiError(404, 'ไม่พบกลุ่ม');
  }

  // Check if already a member
  const existingMember = await StudentGroupMember.findOne({
    where: { group_id: teamId, student_id },
  });

  if (existingMember) {
    throw new ApiError(400, 'นักศึกษาอยู่ในกลุ่มนี้แล้ว');
  }

  await StudentGroupMember.create({
    group_id: teamId,
    student_id,
  });

  res.status(201).json({
    success: true,
    message: 'เพิ่มสมาชิกสำเร็จ',
  });
});

/**
 * Remove member from team
 * @route DELETE /api/courses/:id/teams/:teamId/members/:studentId
 */
const removeMemberFromTeam = asyncHandler(async (req, res) => {
  const { id: courseId, teamId, studentId } = req.params;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(courseId, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const team = await StudentGroup.findOne({
    where: { id: teamId, course_id: courseId },
  });

  if (!team) {
    throw new ApiError(404, 'ไม่พบกลุ่ม');
  }

  const deleted = await StudentGroupMember.destroy({
    where: { group_id: teamId, student_id: studentId },
  });

  if (!deleted) {
    throw new ApiError(404, 'ไม่พบสมาชิกในกลุ่ม');
  }

  res.json({
    success: true,
    message: 'นำสมาชิกออกจากกลุ่มสำเร็จ',
  });
});

module.exports = {
  getTeams,
  createTeam,
  bulkCreateTeams,
  bulkDeleteTeams,
  updateTeam,
  deleteTeam,
  addMemberToTeam,
  removeMemberFromTeam,
};
