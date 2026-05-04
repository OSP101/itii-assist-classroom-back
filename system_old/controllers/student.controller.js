/**
 * Student Controller - Handle student-related requests
 */

const { Student, Score, Assignment, AssignmentSubItem, User, Course, CourseSection, CourseSectionStudent, AttendanceSession, AttendanceRecord, StudentGroup, StudentGroupMember, BonusScore, ExamScore, ExamSetting } = require('../models');
const { Op } = require('sequelize');
const ApiError = require('../utils/ApiError');
const asyncHandler = require('../utils/asyncHandler');

/**
 * Get all students with pagination and filters
 * @route GET /api/students
 */
const getStudents = asyncHandler(async (req, res) => {
  const {
    page = 1,
    limit = 10,
    search = '',
    status = '',
    sortBy = 'created_at',
    sortOrder = 'DESC',
  } = req.query;

  // Build where clause
  const where = {};

  // Search filter
  if (search) {
    where[Op.or] = [
      { student_id: { [Op.like]: `%${search}%` } },
      { full_name: { [Op.like]: `%${search}%` } },
      { email: { [Op.like]: `%${search}%` } },
    ];
  }

  // Status filter
  if (status === 'active') {
    where.is_active = true;
  } else if (status === 'inactive') {
    where.is_active = false;
  }

  // Calculate offset
  const offset = (parseInt(page) - 1) * parseInt(limit);

  // Valid sort columns
  const validSortColumns = ['student_id', 'full_name', 'email', 'is_active', 'created_at', 'updated_at'];
  const orderColumn = validSortColumns.includes(sortBy) ? sortBy : 'created_at';
  const orderDirection = sortOrder.toUpperCase() === 'ASC' ? 'ASC' : 'DESC';

  // Query students
  const { count, rows: students } = await Student.findAndCountAll({
    where,
    limit: parseInt(limit),
    offset,
    order: [[orderColumn, orderDirection]],
    attributes: ['id', 'student_id', 'full_name', 'email', 'extra', 'is_active', 'created_at', 'updated_at'],
  });

  // Calculate pagination info
  const totalPages = Math.ceil(count / parseInt(limit));

  res.json({
    success: true,
    data: {
      students,
      pagination: {
        currentPage: parseInt(page),
        totalPages,
        totalItems: count,
        itemsPerPage: parseInt(limit),
        hasMore: parseInt(page) < totalPages,
      },
    },
  });
});

/**
 * Get student statistics
 * @route GET /api/students/stats
 */
const getStudentStats = asyncHandler(async (req, res) => {
  const total = await Student.count();
  const active = await Student.count({ where: { is_active: true } });
  const inactive = await Student.count({ where: { is_active: false } });

  res.json({
    success: true,
    data: {
      total,
      byStatus: {
        active,
        inactive,
      },
    },
  });
});

/**
 * Get single student by ID
 * @route GET /api/students/:id
 */
const getStudentById = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const student = await Student.findByPk(id);

  if (!student) {
    throw new ApiError(404, 'ไม่พบข้อมูลนักศึกษา');
  }

  res.json({
    success: true,
    data: student,
  });
});

/**
 * Create new student
 * @route POST /api/students
 */
const createStudent = asyncHandler(async (req, res) => {
  const { student_id, full_name, email, extra } = req.body;

  // Validate required fields
  if (!student_id || !full_name || !email) {
    throw new ApiError(400, 'กรุณากรอกรหัสนักศึกษา ชื่อ-นามสกุล และอีเมล');
  }

  // Check if student_id already exists
  const existingStudent = await Student.findOne({ where: { student_id } });
  if (existingStudent) {
    throw new ApiError(400, 'รหัสนักศึกษานี้มีอยู่ในระบบแล้ว');
  }

  // Check if email already exists (if provided)
  if (email) {
    const existingEmail = await Student.findOne({ where: { email } });
    if (existingEmail) {
      throw new ApiError(400, 'อีเมลนี้มีอยู่ในระบบแล้ว');
    }
  }

  // Create student
  const student = await Student.create({
    student_id,
    full_name,
    email,
    extra: extra || null,
    is_active: true,
  });

  res.status(201).json({
    success: true,
    message: 'สร้างข้อมูลนักศึกษาสำเร็จ',
    data: student,
  });
});

/**
 * Update student
 * @route PUT /api/students/:id
 */
const updateStudent = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { student_id, full_name, email, extra, is_active } = req.body;

  // Validate required fields
  if (!student_id || !full_name || !email) {
    throw new ApiError(400, 'กรุณากรอกรหัสนักศึกษา ชื่อ-นามสกุล และอีเมล');
  }

  const student = await Student.findByPk(id);

  if (!student) {
    throw new ApiError(404, 'ไม่พบข้อมูลนักศึกษา');
  }

  // Check if student_id is being changed and already exists
  if (student_id && student_id !== student.student_id) {
    const existingStudent = await Student.findOne({ where: { student_id } });
    if (existingStudent) {
      throw new ApiError(400, 'รหัสนักศึกษานี้มีอยู่ในระบบแล้ว');
    }
  }

  // Check if email is being changed and already exists
  if (email && email !== student.email) {
    const existingEmail = await Student.findOne({ where: { email } });
    if (existingEmail) {
      throw new ApiError(400, 'อีเมลนี้มีอยู่ในระบบแล้ว');
    }
  }

  // Update student
  await student.update({
    student_id: student_id || student.student_id,
    full_name: full_name || student.full_name,
    email: email || student.email,
    extra: extra !== undefined ? extra : student.extra,
    is_active: is_active !== undefined ? is_active : student.is_active,
  });

  res.json({
    success: true,
    message: 'อัปเดตข้อมูลนักศึกษาสำเร็จ',
    data: student,
  });
});

/**
 * Delete student
 * @route DELETE /api/students/:id
 */
const deleteStudent = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const student = await Student.findByPk(id);

  if (!student) {
    throw new ApiError(404, 'ไม่พบข้อมูลนักศึกษา');
  }

  await student.destroy();

  res.json({
    success: true,
    message: 'ลบข้อมูลนักศึกษาสำเร็จ',
  });
});

/**
 * Toggle student active status
 * @route PATCH /api/students/:id/status
 */
const toggleStudentStatus = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const student = await Student.findByPk(id);

  if (!student) {
    throw new ApiError(404, 'ไม่พบข้อมูลนักศึกษา');
  }

  await student.update({
    is_active: !student.is_active,
  });

  res.json({
    success: true,
    message: student.is_active ? 'เปิดใช้งานนักศึกษาแล้ว' : 'ปิดใช้งานนักศึกษาแล้ว',
    data: student,
  });
});

/**
 * Import students from CSV/Excel data
 * @route POST /api/students/import
 */
const importStudents = asyncHandler(async (req, res) => {
  const { students } = req.body;

  if (!students || !Array.isArray(students) || students.length === 0) {
    throw new ApiError(400, 'กรุณาส่งข้อมูลนักศึกษาที่ต้องการนำเข้า');
  }

  const results = {
    created: 0,      // สร้างใหม่สำเร็จ
    skipped: 0,      // ข้ามเพราะซ้ำ
    failed: 0,       // ล้มเหลว
    duplicates: [],  // รายการที่ซ้ำ
    errors: [],      // รายการที่ผิดพลาด
  };

  for (const studentData of students) {
    try {
      const { student_id, full_name, email, extra } = studentData;

      if (!student_id || !full_name) {
        results.failed++;
        results.errors.push({ student_id, error: 'ข้อมูลไม่ครบถ้วน (ต้องมีรหัสนักศึกษาและชื่อ)' });
        continue;
      }

      // Check if already exists
      const existing = await Student.findOne({ where: { student_id } });
      if (existing) {
        // Skip - already exists
        results.skipped++;
        results.duplicates.push({ student_id, full_name });
      } else {
        // Create new student
        await Student.create({
          student_id,
          full_name,
          email: email || null,
          extra: extra || null,
          is_active: true,
        });
        results.created++;
      }
    } catch (error) {
      results.failed++;
      results.errors.push({ 
        student_id: studentData.student_id, 
        error: error.message 
      });
    }
  }

  // For backward compatibility
  results.success = results.created;

  res.json({
    success: true,
    message: `เพิ่มใหม่ ${results.created} คน, ซ้ำ ${results.skipped} คน, ล้มเหลว ${results.failed} รายการ`,
    data: results,
  });
});

/**
 * Get student scores by student_id (public endpoint for students to check their scores)
 * @route GET /api/students/lookup/:student_id
 */
const lookupStudentScores = asyncHandler(async (req, res) => {
  const { student_id } = req.params;

  if (!student_id) {
    throw new ApiError(400, 'กรุณาระบุรหัสนักศึกษา');
  }

  // Find student by student_id
  const student = await Student.findOne({
    where: { student_id },
    attributes: ['id', 'student_id', 'full_name', 'email', 'is_active'],
  });

  if (!student) {
    throw new ApiError(404, 'ไม่พบข้อมูลนักศึกษา');
  }

  // Get all courses the student is enrolled in
  const enrollments = await CourseSectionStudent.findAll({
    where: { student_id: student.id },
    include: [
      {
        model: CourseSection,
        as: 'section',
        include: [
          {
            model: Course,
            as: 'course',
            attributes: ['id', 'code', 'name', 'year', 'semester', 'is_active'],
          },
        ],
      },
    ],
  });

  // Get unique courses
  const coursesMap = new Map();
  enrollments.forEach(enrollment => {
    if (enrollment.section && enrollment.section.course) {
      const course = enrollment.section.course;
      if (!coursesMap.has(course.id)) {
        coursesMap.set(course.id, {
          ...course.toJSON(),
          sections: [],
        });
      }
      coursesMap.get(course.id).sections.push({
        id: enrollment.section.id,
        name: enrollment.section.name,
        week_number: enrollment.week_number,
      });
    }
  });

  const courses = Array.from(coursesMap.values());
  const courseIds = courses.map(c => c.id);

  // Get student's group memberships
  const groupMemberships = await StudentGroupMember.findAll({
    where: { student_id: student.id },
    include: [
      {
        model: StudentGroup,
        as: 'group',
        attributes: ['id', 'name', 'course_id', 'group_type', 'week_number'],
      },
    ],
  });

  // Build group IDs list for this student
  const studentGroupIds = groupMemberships.map(m => m.group_id);
  const groupInfoMap = {};
  groupMemberships.forEach(m => {
    if (m.group) {
      groupInfoMap[m.group.id] = {
        id: m.group.id,
        name: m.group.name,
        course_id: m.group.course_id,
        group_type: m.group.group_type,
        week_number: m.group.week_number,
      };
    }
  });

  // Get ALL assignments for enrolled courses (only visible ones for student lookup)
  const allAssignments = await Assignment.findAll({
    where: { 
      course_id: { [Op.in]: courseIds },
      is_active: true,
      is_score_visible: true, // Only show assignments where scores are visible to students
    },
    include: [
      {
        model: AssignmentSubItem,
        as: 'subItems',
        attributes: ['id', 'name', 'max_score', 'order_index'],
      },
    ],
    order: [['order_index', 'ASC'], ['created_at', 'ASC']],
  });

  // Get ALL scores for this student (individual - main scores)
  const individualMainScores = await Score.findAll({
    where: { 
      student_id: student.id,
      sub_item_id: null,
    },
    include: [
      {
        model: User,
        as: 'grader',
        attributes: ['id', 'full_name'],
      },
    ],
  });

  // Get ALL scores for this student (individual - sub-item scores)
  const individualSubItemScores = await Score.findAll({
    where: {
      student_id: student.id,
      sub_item_id: { [Op.ne]: null },
    },
    include: [
      {
        model: User,
        as: 'grader',
        attributes: ['id', 'full_name'],
      },
    ],
  });

  // Get ALL group scores (main scores) where student is a member
  const groupMainScores = studentGroupIds.length > 0 ? await Score.findAll({
    where: { 
      group_id: { [Op.in]: studentGroupIds },
      sub_item_id: null,
    },
    include: [
      {
        model: User,
        as: 'grader',
        attributes: ['id', 'full_name'],
      },
      {
        model: StudentGroup,
        as: 'group',
        attributes: ['id', 'name'],
      },
    ],
  }) : [];

  // Get ALL group scores (sub-item scores) where student is a member
  const groupSubItemScores = studentGroupIds.length > 0 ? await Score.findAll({
    where: {
      group_id: { [Op.in]: studentGroupIds },
      sub_item_id: { [Op.ne]: null },
    },
    include: [
      {
        model: User,
        as: 'grader',
        attributes: ['id', 'full_name'],
      },
    ],
  }) : [];

  // Build score maps
  // Main score map: assignment_id -> score data
  const mainScoreMap = {};
  
  // Individual main scores
  individualMainScores.forEach(score => {
    mainScoreMap[score.assignment_id] = {
      score: score.score !== null ? parseFloat(score.score) : null,
      status: score.status,
      grader: score.grader ? score.grader.full_name : null,
      graded_at: score.graded_at,
      comment: score.comment,
      is_group: false,
      group_info: null,
    };
  });

  // Group main scores (only if not already have individual score)
  groupMainScores.forEach(score => {
    if (!mainScoreMap[score.assignment_id]) {
      mainScoreMap[score.assignment_id] = {
        score: score.score !== null ? parseFloat(score.score) : null,
        status: score.status,
        grader: score.grader ? score.grader.full_name : null,
        graded_at: score.graded_at,
        comment: score.comment,
        is_group: true,
        group_info: score.group ? { id: score.group.id, name: score.group.name } : (groupInfoMap[score.group_id] || null),
      };
    }
  });

  // Sub-item score map: assignment_id -> sub_item_id -> score data
  const subItemScoreMap = {};
  
  // Individual sub-item scores
  individualSubItemScores.forEach(score => {
    if (!subItemScoreMap[score.assignment_id]) {
      subItemScoreMap[score.assignment_id] = {};
    }
    subItemScoreMap[score.assignment_id][score.sub_item_id] = {
      score: score.score !== null ? parseFloat(score.score) : null,
      grader: score.grader ? score.grader.full_name : null,
      graded_at: score.graded_at,
    };
  });

  // Group sub-item scores (only if not already present)
  groupSubItemScores.forEach(score => {
    if (!subItemScoreMap[score.assignment_id]) {
      subItemScoreMap[score.assignment_id] = {};
    }
    if (!subItemScoreMap[score.assignment_id][score.sub_item_id]) {
      subItemScoreMap[score.assignment_id][score.sub_item_id] = {
        score: score.score !== null ? parseFloat(score.score) : null,
        grader: score.grader ? score.grader.full_name : null,
        graded_at: score.graded_at,
      };
    }
  });

  // Group scores by course
  const scoresByCourse = {};
  courses.forEach(course => {
    scoresByCourse[course.id] = {
      course,
      assignments: [],
      totalScore: 0,
      totalMaxScore: 0,
    };
  });

  // Process ALL assignments from enrolled courses
  allAssignments.forEach(assignment => {
    // Check if this course is in scoresByCourse
    if (!scoresByCourse[assignment.course_id]) return;

    // Get main score for this assignment (if exists)
    const mainScore = mainScoreMap[assignment.id];
    
    // Get sub-item scores for this assignment
    const assignmentSubItemScores = subItemScoreMap[assignment.id] || {};
    
    // Check if this is a group assignment type
    const isGroupType = assignment.assignment_type === 'permanent_group' || assignment.assignment_type === 'weekly_group';
    
    // Build assignment data
    const assignmentData = {
      id: assignment.id,
      title: assignment.name,
      type: assignment.assignment_type,
      max_score: parseFloat(assignment.max_score),
      score: mainScore ? mainScore.score : null,
      status: mainScore ? mainScore.status : 'pending',
      grader: mainScore ? mainScore.grader : null,
      graded_at: mainScore ? mainScore.graded_at : null,
      comment: mainScore ? mainScore.comment : null,
      is_group_assignment: isGroupType || (mainScore ? mainScore.is_group : false),
      group_info: mainScore ? mainScore.group_info : null,
      sub_items: [],
    };

    // Add sub-item scores if assignment has sub-items
    if (assignment.subItems && assignment.subItems.length > 0) {
      assignmentData.sub_items = assignment.subItems
        .sort((a, b) => (a.order_index || 0) - (b.order_index || 0))
        .map(subItem => ({
          id: subItem.id,
          name: subItem.name,
          max_score: parseFloat(subItem.max_score),
          score: assignmentSubItemScores[subItem.id]?.score ?? null,
          grader: assignmentSubItemScores[subItem.id]?.grader || null,
          graded_at: assignmentSubItemScores[subItem.id]?.graded_at || null,
        }));
      
      // Calculate total score from sub-items if no main score
      if (assignmentData.score === null) {
        const subItemsWithScores = assignmentData.sub_items.filter(si => si.score !== null);
        if (subItemsWithScores.length > 0) {
          assignmentData.score = subItemsWithScores.reduce((sum, si) => sum + si.score, 0);
          // If any sub-item is graded, mark as graded
          if (subItemsWithScores.length === assignmentData.sub_items.length) {
            assignmentData.status = 'graded';
          }
          // Get the latest grader info from sub-items
          const gradedSubItems = subItemsWithScores.filter(si => si.grader);
          if (gradedSubItems.length > 0) {
            const latestSubItem = gradedSubItems.sort((a, b) => 
              new Date(b.graded_at || 0).getTime() - new Date(a.graded_at || 0).getTime()
            )[0];
            assignmentData.grader = latestSubItem.grader;
            assignmentData.graded_at = latestSubItem.graded_at;
          }
        }
      }
    }

    // Add ALL assignments (regardless of whether they have scores or not)
    scoresByCourse[assignment.course_id].assignments.push(assignmentData);
    
    // Update totals - only count max_score for all assignments
    scoresByCourse[assignment.course_id].totalMaxScore += parseFloat(assignment.max_score);
    
    // Only add to totalScore if assignment has been graded
    if (assignmentData.score !== null) {
      scoresByCourse[assignment.course_id].totalScore += assignmentData.score;
    }
  });

  // Get bonus scores for this student grouped by course
  const bonusScoreRecords = await BonusScore.findAll({
    where: {
      student_id: student.id,
      course_id: { [Op.in]: courseIds },
    },
    attributes: ['course_id', 'score', 'reason', 'given_at'],
    include: [
      {
        model: User,
        as: 'giver',
        attributes: ['id', 'full_name'],
      },
    ],
    order: [['given_at', 'DESC']],
  });

  // Group bonus scores by course
  const bonusByCourse = {};
  bonusScoreRecords.forEach(record => {
    if (!bonusByCourse[record.course_id]) {
      bonusByCourse[record.course_id] = {
        totalBonus: 0,
        records: [],
      };
    }
    bonusByCourse[record.course_id].totalBonus += parseFloat(record.score) || 0;
    bonusByCourse[record.course_id].records.push({
      score: parseFloat(record.score) || 0,
      reason: record.reason,
      given_by: record.giver ? record.giver.full_name : null,
      given_at: record.given_at,
    });
  });

  // Get attendance records for this student (only from sessions that have started or ended)
  const now = new Date();
  const attendanceRecords = await AttendanceRecord.findAll({
    where: { student_id: student.id },
    include: [
      {
        model: AttendanceSession,
        as: 'session',
        attributes: ['id', 'title', 'start_time', 'end_time', 'course_id', 'status'],
        where: {
          // Only show sessions that are active or closed (started)
          // Don't show draft sessions (not yet opened)
          start_time: { [Op.lte]: now }, // Session has started
        },
      },
    ],
    order: [['created_at', 'DESC']],
  });

  // Group attendance by course
  const attendanceByCourse = {};
  attendanceRecords.forEach(record => {
    if (record.session) {
      const courseId = record.session.course_id;
      if (!attendanceByCourse[courseId]) {
        attendanceByCourse[courseId] = {
          records: [],
          summary: { present: 0, late: 0, leave: 0, absent: 0 },
        };
      }
      attendanceByCourse[courseId].records.push({
        id: record.id,
        session_title: record.session.title,
        date: record.session.start_time,
        status: record.status,
        check_in_time: record.check_in_time,
        note: record.note,
      });
      attendanceByCourse[courseId].summary[record.status]++;
    }
  });

  // Get exam scores for this student (only visible ones)
  const examScoreRecords = await ExamScore.findAll({
    where: { student_id: student.id },
    include: [
      {
        model: ExamSetting,
        as: 'examSetting',
        where: {
          course_id: { [Op.in]: courseIds },
          is_active: true,
          is_visible: true, // Only show visible exam scores
        },
        attributes: ['id', 'course_id', 'exam_type', 'component', 'max_score', 'is_visible'],
      },
      {
        model: User,
        as: 'grader',
        attributes: ['id', 'full_name'],
      },
    ],
  });

  // Group exam scores by course
  const examScoresByCourse = {};
  examScoreRecords.forEach(record => {
    if (record.examSetting) {
      const courseId = record.examSetting.course_id;
      if (!examScoresByCourse[courseId]) {
        examScoresByCourse[courseId] = [];
      }
      examScoresByCourse[courseId].push({
        id: record.id,
        exam_type: record.examSetting.exam_type,
        component: record.examSetting.component,
        score: record.score !== null ? parseFloat(record.score) : null,
        max_score: parseFloat(record.examSetting.max_score),
        grader: record.grader ? record.grader.full_name : null,
        graded_at: record.graded_at,
        comment: record.comment,
      });
    }
  });

  // Build final response
  const courseScores = Object.values(scoresByCourse).map(courseData => {
    const bonusData = bonusByCourse[courseData.course.id] || { totalBonus: 0, records: [] };
    return {
      ...courseData,
      bonusScore: {
        total: bonusData.totalBonus,
        records: bonusData.records,
      },
      attendance: attendanceByCourse[courseData.course.id] || {
        records: [],
        summary: { present: 0, late: 0, leave: 0, absent: 0 },
      },
      examScores: examScoresByCourse[courseData.course.id] || [],
      progress: courseData.totalMaxScore > 0 
        ? Math.round((courseData.totalScore / courseData.totalMaxScore) * 100) 
        : 0,
    };
  });

  res.json({
    success: true,
    data: {
      student: {
        id: student.id,
        student_id: student.student_id,
        full_name: student.full_name,
        email: student.email,
      },
      courses: courseScores,
    },
  });
});

/**
 * Search students by multiple student IDs within a specific course/section
 * @route POST /api/students/search-by-ids
 */
const searchStudentsByIds = asyncHandler(async (req, res) => {
  const { student_ids, course_id, section } = req.body;

  if (!student_ids || !Array.isArray(student_ids) || student_ids.length === 0) {
    throw new ApiError(400, 'student_ids array is required');
  }

  // Limit to prevent abuse
  if (student_ids.length > 100) {
    throw new ApiError(400, 'Maximum 100 student IDs allowed per request');
  }

  // Clean up input - trim and remove empty
  const cleanedIds = student_ids
    .map(id => String(id).trim())
    .filter(id => id.length > 0);

  if (cleanedIds.length === 0) {
    return res.json({
      success: true,
      data: {
        found: [],
        not_found: student_ids,
      },
    });
  }

  let students;

  // If course_id is provided, filter students by course enrollment
  if (course_id) {
    // Build section filter
    const sectionWhere = { course_id };
    if (section && section !== 'all') {
      sectionWhere.section = section;
    }

    // Find course sections
    const sections = await CourseSection.findAll({
      where: sectionWhere,
      attributes: ['id'],
    });

    const sectionIds = sections.map(s => s.id);

    if (sectionIds.length === 0) {
      return res.json({
        success: true,
        data: {
          found: [],
          not_found: cleanedIds,
        },
      });
    }

    // Find students enrolled in these sections
    const enrollments = await CourseSectionStudent.findAll({
      where: {
        course_section_id: {
          [Op.in]: sectionIds,
        },
      },
      include: [{
        model: Student,
        as: 'student',
        where: {
          student_id: {
            [Op.in]: cleanedIds,
          },
          is_active: true,
        },
        attributes: ['id', 'student_id', 'full_name', 'email'],
      }],
    });

    students = enrollments
      .map(e => e.student)
      .filter(s => s !== null);
  } else {
    // Fallback: search all students (original behavior)
    students = await Student.findAll({
      where: {
        student_id: {
          [Op.in]: cleanedIds,
        },
        is_active: true,
      },
      attributes: ['id', 'student_id', 'full_name', 'email'],
    });
  }

  // Build result map for found students
  const foundMap = new Map();
  students.forEach(s => {
    foundMap.set(s.student_id.toLowerCase(), {
      id: s.id,
      student_id: s.student_id,
      full_name: s.full_name,
      email: s.email,
    });
  });

  // Separate found and not found
  const found = [];
  const not_found = [];

  cleanedIds.forEach(inputId => {
    const student = foundMap.get(inputId.toLowerCase());
    if (student) {
      found.push({
        input: inputId,
        student,
      });
    } else {
      not_found.push(inputId);
    }
  });

  res.json({
    success: true,
    data: {
      found,
      not_found,
    },
  });
});

module.exports = {
  getStudents,
  getStudentStats,
  getStudentById,
  createStudent,
  updateStudent,
  deleteStudent,
  toggleStudentStatus,
  importStudents,
  lookupStudentScores,
  searchStudentsByIds,
};