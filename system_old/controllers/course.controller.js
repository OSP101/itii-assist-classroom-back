/**
 * Course Controller - Handle course-related requests
 */

const { 
  Course, 
  CourseSection,
  CourseInstructor,
  CourseTA, 
  CourseSectionStudent, 
  User, 
  Student,
  Assignment,
  AssignmentSubItem,
  Score,
  AttendanceSession,
  AttendanceRecord,
  sequelize,
} = require('../models');
const { Op } = require('sequelize');
const ApiError = require('../utils/ApiError');
const asyncHandler = require('../utils/asyncHandler');
const { logCourseActivity } = require('../utils/courseActivityLogger');
const logger = require('../utils/logger');
const { cache, CACHE_TTL } = require('../utils/cache');
const { 
  batchCount, 
  batchCountByStatus,
  getStudentCountsByCourse,
  getAttendanceStatsBatch,
  getScoreStatsBatch,
} = require('../utils/queryHelpers');

/**
 * Check if user has access to course (is admin, instructor, or TA of course)
 */
const checkCourseAccess = async (courseId, user) => {
  if (user.role === 'admin') return true;
  
  const course = await Course.findByPk(courseId);
  if (!course) return false;
  
  // Check if user is one of the instructors
  if (user.role === 'instructor') {
    const isInstructor = await CourseInstructor.findOne({
      where: { course_id: courseId, user_id: user.id }
    });
    if (isInstructor) return true;
    // Also check legacy instructor_id field
    if (course.instructor_id === user.id) return true;
  }
  
  // Check if user is a TA of this course
  if (user.role === 'ta' || user.role === 'instructor') {
    const isTA = await CourseTA.findOne({
      where: { course_id: courseId, user_id: user.id }
    });
    if (isTA) return true;
  }
  
  return false;
};

/**
 * Get all courses with pagination and filters
 * @route GET /api/courses
 */
const getCourses = asyncHandler(async (req, res) => {
  const {
    page = 1,
    limit = 10,
    search = '',
    year = '',
    semester = '',
    status = '',
    sortBy = 'created_at',
    sortOrder = 'DESC',
  } = req.query;

  // Build where clause with proper AND/OR structure
  const whereConditions = [];

  // Search filter (OR between code and name)
  if (search && search.trim()) {
    whereConditions.push({
      [Op.or]: [
        { code: { [Op.like]: `%${search.trim()}%` } },
        { name: { [Op.like]: `%${search.trim()}%` } },
      ],
    });
  }

  // Year filter
  if (year && !isNaN(parseInt(year))) {
    whereConditions.push({ year: parseInt(year) });
  }

  // Semester filter
  if (semester && !isNaN(parseInt(semester))) {
    whereConditions.push({ semester: parseInt(semester) });
  }

  // Status filter
  if (status === 'active') {
    whereConditions.push({ is_active: true });
  } else if (status === 'inactive') {
    whereConditions.push({ is_active: false });
  }

  // Combine all conditions with AND
  const where = whereConditions.length > 0 ? { [Op.and]: whereConditions } : {};

  // Calculate offset
  const offset = (parseInt(page) - 1) * parseInt(limit);

  // Valid sort columns
  const validSortColumns = ['code', 'name', 'year', 'semester', 'is_active', 'created_at', 'updated_at'];
  const orderColumn = validSortColumns.includes(sortBy) ? sortBy : 'created_at';
  const orderDirection = sortOrder.toUpperCase() === 'ASC' ? 'ASC' : 'DESC';

  // Query courses
  const { count, rows: courses } = await Course.findAndCountAll({
    where,
    limit: parseInt(limit),
    offset,
    order: [[orderColumn, orderDirection]],
    include: [
      {
        model: User,
        as: 'instructor',
        attributes: ['id', 'full_name', 'email'],
      },
      {
        model: User,
        as: 'instructors',
        attributes: ['id', 'full_name', 'email'],
        through: { attributes: ['is_primary', 'assigned_at'] },
      },
      {
        model: CourseSection,
        as: 'sections',
        attributes: ['id', 'section_no', 'note'],
      },
    ],
  });

  // ✅ OPTIMIZED: Batch count instead of N+1 queries
  const courseIds = courses.map(c => c.id);
  
  // Get TA counts in single query
  const taCounts = await batchCount(CourseTA, 'course_id', courseIds);
  
  // Get student counts in single query (optimized with raw SQL)
  const studentCounts = await getStudentCountsByCourse(courseIds);

  // Map counts to courses (no additional queries needed)
  const coursesWithCounts = courses.map(course => ({
    ...course.toJSON(),
    taCount: taCounts[course.id] || 0,
    studentCount: studentCounts[course.id] || 0,
  }));

  // Calculate pagination info
  const totalPages = Math.ceil(count / parseInt(limit));

  res.json({
    success: true,
    data: {
      courses: coursesWithCounts,
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
 * Get course statistics
 * @route GET /api/courses/stats
 */
const getCourseStats = asyncHandler(async (req, res) => {
  const total = await Course.count();
  const active = await Course.count({ where: { is_active: true } });
  const inactive = await Course.count({ where: { is_active: false } });

  // Get current year courses
  const currentYear = new Date().getFullYear() + 543; // พ.ศ.
  const thisYear = await Course.count({ where: { year: currentYear } });

  // Get unique years
  const years = await Course.findAll({
    attributes: ['year'],
    group: ['year'],
    order: [['year', 'DESC']],
    raw: true,
  });

  res.json({
    success: true,
    data: {
      total,
      byStatus: {
        active,
        inactive,
      },
      thisYear,
      years: years.map(y => y.year),
    },
  });
});

/**
 * Get single course by ID
 * @route GET /api/courses/:id
 */
const getCourseById = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const course = await Course.findByPk(id, {
    include: [
      {
        model: User,
        as: 'instructor',
        attributes: ['id', 'full_name', 'email', 'username', 'avatar'],
      },
      {
        model: User,
        as: 'instructors',
        attributes: ['id', 'full_name', 'email', 'username', 'avatar'],
        through: { attributes: ['is_primary', 'assigned_at'] },
      },
      {
        model: CourseSection,
        as: 'sections',
        attributes: ['id', 'section_no', 'note', 'created_at'],
      },
      {
        model: User,
        as: 'tas',
        attributes: ['id', 'full_name', 'email', 'username', 'avatar'],
        through: { attributes: ['assigned_at'] },
      },
    ],
  });

  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  // Get student count per section
  const sectionsWithStudents = await Promise.all(
    course.sections.map(async (section) => {
      const studentCount = await CourseSectionStudent.count({
        where: { course_section_id: section.id },
      });
      return {
        ...section.toJSON(),
        studentCount,
      };
    })
  );

  res.json({
    success: true,
    data: {
      ...course.toJSON(),
      sections: sectionsWithStudents,
    },
  });
});

/**
 * Create new course
 * @route POST /api/courses
 */
const createCourse = asyncHandler(async (req, res) => {
  const { code, name, year, semester, instructor_id, instructor_ids, description, image, attention_threshold } = req.body;
  const currentUser = req.user;

  // Validate required fields
  if (!code || !name || !year || !semester) {
    throw new ApiError(400, 'กรุณากรอกข้อมูลที่จำเป็น (รหัสวิชา, ชื่อวิชา, ปีการศึกษา, ภาคเรียน)');
  }

  // Check if ACTIVE course already exists with same code/year/semester
  const existingCourse = await Course.findOne({
    where: { 
      code, 
      year: parseInt(year), 
      semester: parseInt(semester),
      is_active: true, // Only check active courses
    },
  });
  if (existingCourse) {
    throw new ApiError(400, 'รายวิชานี้มีเปิดใช้งานอยู่แล้ว (รหัส-ปี-ภาคเรียน ซ้ำ) กรุณาปิดใช้งานรายวิชาเดิมก่อน หรือใช้รหัสวิชาอื่น');
  }

  // Prepare instructor IDs (support both single and multiple)
  let instructorIdList = [];
  if (instructor_ids && Array.isArray(instructor_ids) && instructor_ids.length > 0) {
    instructorIdList = instructor_ids.map(id => parseInt(id));
  } else if (instructor_id) {
    instructorIdList = [parseInt(instructor_id)];
  }
  
  // IMPORTANT: Always add current user (the creator) as instructor if they are an instructor
  // and not already in the list
  if (currentUser.role === 'instructor' && !instructorIdList.includes(currentUser.id)) {
    // Add creator as first (primary) instructor
    instructorIdList = [currentUser.id, ...instructorIdList];
  }

  // If still no instructors (admin creating without specifying), default to empty
  // Admin must specify instructors explicitly

  // Debug log
  logger.debug('createCourse - currentUser:', { id: currentUser.id, role: currentUser.role });
  logger.debug('createCourse - instructor_ids from request:', instructor_ids);
  logger.debug('createCourse - instructor_id from request:', instructor_id);
  logger.debug('createCourse - final instructorIdList:', instructorIdList);

  // Validate all instructors
  if (instructorIdList.length > 0) {
    const instructors = await User.findAll({
      where: { id: instructorIdList, role: 'instructor' },
    });
    if (instructors.length !== instructorIdList.length) {
      throw new ApiError(400, 'ไม่พบอาจารย์บางคนที่ระบุในระบบ');
    }
  }

  // Set primary instructor (first one or legacy instructor_id)
  const primaryInstructorId = instructorIdList.length > 0 ? instructorIdList[0] : null;

  // Create course
  const course = await Course.create({
    code,
    name,
    year: parseInt(year),
    semester: parseInt(semester),
    instructor_id: primaryInstructorId, // Keep for backward compatibility
    description: description || null,
    image: image || null,
    is_active: true,
    attention_threshold: attention_threshold !== undefined ? parseInt(attention_threshold) : 60,
  });

  // Add all instructors to course_instructors table
  if (instructorIdList.length > 0) {
    const courseInstructorData = instructorIdList.map((userId, index) => ({
      course_id: course.id,
      user_id: userId,
      is_primary: index === 0, // First instructor is primary
    }));
    await CourseInstructor.bulkCreate(courseInstructorData);
  }

  // Reload with associations
  const createdCourse = await Course.findByPk(course.id, {
    include: [
      {
        model: User,
        as: 'instructor',
        attributes: ['id', 'full_name', 'email'],
      },
      {
        model: User,
        as: 'instructors',
        attributes: ['id', 'full_name', 'email'],
        through: { attributes: ['is_primary', 'assigned_at'] },
      },
    ],
  });

  logCourseActivity({ courseId: course.id, actorUserId: req.user.id, action: 'create_course', category: 'course', targetType: 'course', targetId: course.id, targetName: `${code} - ${name}` });

  res.status(201).json({
    success: true,
    message: 'สร้างรายวิชาสำเร็จ',
    data: createdCourse,
  });
});

/**
 * Update course
 * @route PUT /api/courses/:id
 */
const updateCourse = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { code, name, year, semester, instructor_id, instructor_ids, description, is_active, attention_threshold } = req.body;

  const course = await Course.findByPk(id);

  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  // Check for duplicate if code/year/semester changed (only check against ACTIVE courses)
  if (code || year || semester) {
    const checkCode = code || course.code;
    const checkYear = year ? parseInt(year) : course.year;
    const checkSemester = semester ? parseInt(semester) : course.semester;

    const existingCourse = await Course.findOne({
      where: {
        code: checkCode,
        year: checkYear,
        semester: checkSemester,
        id: { [Op.ne]: id },
        is_active: true, // Only check active courses
      },
    });
    if (existingCourse) {
      throw new ApiError(400, `รหัสวิชา "${checkCode}" ปีการศึกษา ${checkYear} ภาคเรียน ${checkSemester} มีเปิดใช้งานอยู่แล้ว กรุณาใช้รหัสวิชา ปีการศึกษา หรือภาคเรียนอื่น`);
    }
  }

  // Prepare instructor IDs if provided
  let instructorIdList = [];
  let shouldUpdateInstructors = false;
  
  if (instructor_ids !== undefined) {
    shouldUpdateInstructors = true;
    if (Array.isArray(instructor_ids)) {
      instructorIdList = instructor_ids.map(id => parseInt(id));
    }
  } else if (instructor_id !== undefined) {
    shouldUpdateInstructors = true;
    if (instructor_id) {
      instructorIdList = [parseInt(instructor_id)];
    }
  }

  // IMPORTANT: For instructor users editing their course, always include themselves
  // This ensures the creator/editor doesn't accidentally remove themselves
  const currentUser = req.user;
  if (shouldUpdateInstructors && currentUser.role === 'instructor' && !instructorIdList.includes(currentUser.id)) {
    // Check if current user is already an instructor of this course
    const isCurrentInstructor = await CourseInstructor.findOne({
      where: { course_id: id, user_id: currentUser.id }
    });
    if (isCurrentInstructor) {
      // Keep them as primary if they were primary, otherwise add at the end
      if (isCurrentInstructor.is_primary) {
        instructorIdList = [currentUser.id, ...instructorIdList];
      } else {
        instructorIdList = [...instructorIdList, currentUser.id];
      }
    }
  }

  // Debug log
  logger.debug('updateCourse - currentUser:', { id: currentUser.id, role: currentUser.role });
  logger.debug('updateCourse - instructor_ids from request:', instructor_ids);
  logger.debug('updateCourse - final instructorIdList:', instructorIdList);

  // Validate all instructors if updating
  if (shouldUpdateInstructors && instructorIdList.length > 0) {
    const instructors = await User.findAll({
      where: { id: instructorIdList, role: 'instructor' },
    });
    if (instructors.length !== instructorIdList.length) {
      throw new ApiError(400, 'ไม่พบอาจารย์บางคนที่ระบุในระบบ');
    }
  }

  // Update course
  const { image } = req.body;
  const primaryInstructorId = instructorIdList.length > 0 ? instructorIdList[0] : null;
  
  await course.update({
    code: code || course.code,
    name: name || course.name,
    year: year ? parseInt(year) : course.year,
    semester: semester ? parseInt(semester) : course.semester,
    instructor_id: shouldUpdateInstructors ? primaryInstructorId : course.instructor_id,
    description: description !== undefined ? description : course.description,
    image: image !== undefined ? image : course.image,
    is_active: is_active !== undefined ? is_active : course.is_active,
    attention_threshold: attention_threshold !== undefined ? parseInt(attention_threshold) : course.attention_threshold,
  });

  // Update course_instructors if needed
  if (shouldUpdateInstructors) {
    // Remove existing instructors
    await CourseInstructor.destroy({ where: { course_id: id } });
    
    // Add new instructors
    if (instructorIdList.length > 0) {
      const courseInstructorData = instructorIdList.map((userId, index) => ({
        course_id: id,
        user_id: userId,
        is_primary: index === 0,
      }));
      await CourseInstructor.bulkCreate(courseInstructorData);
    }
  }

  // Reload with associations
  const updatedCourse = await Course.findByPk(id, {
    include: [
      {
        model: User,
        as: 'instructor',
        attributes: ['id', 'full_name', 'email'],
      },
      {
        model: User,
        as: 'instructors',
        attributes: ['id', 'full_name', 'email'],
        through: { attributes: ['is_primary', 'assigned_at'] },
      },
    ],
  });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'update_course', category: 'course', targetType: 'course', targetId: id, targetName: updatedCourse.name, detail: { fields: Object.keys(req.body) } });

  res.json({
    success: true,
    message: 'อัปเดตรายวิชาสำเร็จ',
    data: updatedCourse,
  });
});

/**
 * Delete course
 * @route DELETE /api/courses/:id
 */
const deleteCourse = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const course = await Course.findByPk(id);

  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  // Instructor can only delete their own courses
  if (req.user.role === 'instructor') {
    const isInstructor = await CourseInstructor.findOne({
      where: { course_id: id, user_id: req.user.id },
    });
    if (!isInstructor) {
      throw new ApiError(403, 'คุณไม่มีสิทธิ์ลบรายวิชานี้ เฉพาะอาจารย์ผู้สอนของรายวิชาเท่านั้น');
    }
  }

  // Delete related data
  await CourseTA.destroy({ where: { course_id: id } });
  
  // Get sections
  const sections = await CourseSection.findAll({ where: { course_id: id } });
  const sectionIds = sections.map(s => s.id);
  
  // Delete section students
  if (sectionIds.length > 0) {
    await CourseSectionStudent.destroy({ where: { course_section_id: sectionIds } });
  }
  
  // Delete sections
  await CourseSection.destroy({ where: { course_id: id } });
  
  // Delete course
  const courseName = course.name;
  await course.destroy();

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'delete_course', category: 'course', targetType: 'course', targetId: id, targetName: courseName });

  res.json({
    success: true,
    message: 'ลบรายวิชาสำเร็จ',
  });
});

/**
 * Toggle course status
 * @route PATCH /api/courses/:id/toggle-status
 */
const toggleCourseStatus = asyncHandler(async (req, res) => {
  const { id } = req.params;

  const course = await Course.findByPk(id);

  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  const willBeActive = !course.is_active;

  // If trying to activate, check for duplicate active course
  if (willBeActive) {
    const existingActiveCourse = await Course.findOne({
      where: {
        code: course.code,
        year: course.year,
        semester: course.semester,
        is_active: true,
        id: { [Op.ne]: id },
      },
    });
    if (existingActiveCourse) {
      throw new ApiError(400, `ไม่สามารถเปิดใช้งานได้ เนื่องจากมีรายวิชา ${course.code} ปี ${course.year} ภาคเรียน ${course.semester} เปิดใช้งานอยู่แล้ว กรุณาปิดใช้งานรายวิชาดังกล่าวก่อน`);
    }
  }

  await course.update({ is_active: willBeActive });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: willBeActive ? 'activate_course' : 'deactivate_course', category: 'course', targetType: 'course', targetId: id, targetName: course.name });

  res.json({
    success: true,
    message: willBeActive ? 'เปิดใช้งานรายวิชาสำเร็จ' : 'ปิดใช้งานรายวิชาสำเร็จ',
    data: course,
  });
});

// ============================================
// Section Management
// ============================================

/**
 * Add section to course
 * @route POST /api/courses/:id/sections
 */
const addSection = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { section_no, note } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(id, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const course = await Course.findByPk(id);
  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  if (!section_no) {
    throw new ApiError(400, 'กรุณาระบุหมายเลขกลุ่มเรียน');
  }

  // Check duplicate
  const existingSection = await CourseSection.findOne({
    where: { course_id: id, section_no },
  });
  if (existingSection) {
    throw new ApiError(400, 'กลุ่มเรียนนี้มีอยู่แล้ว');
  }

  const section = await CourseSection.create({
    course_id: id,
    section_no,
    note: note || null,
  });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'add_section', category: 'course', targetType: 'section', targetId: section.id, targetName: `กลุ่ม ${section_no}` });

  res.status(201).json({
    success: true,
    message: 'เพิ่มกลุ่มเรียนสำเร็จ',
    data: section,
  });
});

/**
 * Remove section from course
 * @route DELETE /api/courses/:id/sections/:sectionId
 */
const removeSection = asyncHandler(async (req, res) => {
  const { id, sectionId } = req.params;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(id, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const section = await CourseSection.findOne({
    where: { id: sectionId, course_id: id },
  });

  if (!section) {
    throw new ApiError(404, 'ไม่พบกลุ่มเรียน');
  }

  // Delete students from section
  await CourseSectionStudent.destroy({ where: { course_section_id: sectionId } });

  const sectionNo = section.section_no;
  // Delete section
  await section.destroy();

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'remove_section', category: 'course', targetType: 'section', targetId: sectionId, targetName: `กลุ่ม ${sectionNo}` });

  res.json({
    success: true,
    message: 'ลบกลุ่มเรียนสำเร็จ',
  });
});

/**
 * Update section
 * @route PUT /api/courses/:id/sections/:sectionId
 */
const updateSection = asyncHandler(async (req, res) => {
  const { id, sectionId } = req.params;
  const { section_no, note } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(id, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const section = await CourseSection.findOne({
    where: { id: sectionId, course_id: id },
  });

  if (!section) {
    throw new ApiError(404, 'ไม่พบกลุ่มเรียน');
  }

  if (!section_no || !section_no.trim()) {
    throw new ApiError(400, 'กรุณาระบุหมายเลขกลุ่มเรียน');
  }

  // Check duplicate (exclude current section)
  const existingSection = await CourseSection.findOne({
    where: { 
      course_id: id, 
      section_no: section_no.trim(),
      id: { [Op.ne]: sectionId }
    },
  });
  
  if (existingSection) {
    throw new ApiError(400, `หมายเลขกลุ่มเรียน ${section_no} มีอยู่แล้ว`);
  }

  await section.update({
    section_no: section_no.trim(),
    note: note || null,
  });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'update_section', category: 'course', targetType: 'section', targetId: sectionId, targetName: `กลุ่ม ${section_no}` });

  res.json({
    success: true,
    message: 'แก้ไขกลุ่มเรียนสำเร็จ',
    data: section,
  });
});

// ============================================
// TA Management
// ============================================

/**
 * Add TA to course
 * @route POST /api/courses/:id/tas
 */
const addTA = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { user_id } = req.body;
  const currentUser = req.user;

  // Check course access (only admin or instructor of this course)
  if (currentUser.role !== 'admin') {
    const course = await Course.findByPk(id);
    if (!course || course.instructor_id !== currentUser.id) {
      throw new ApiError(403, 'คุณไม่มีสิทธิ์เพิ่มผู้ช่วยสอนในรายวิชานี้');
    }
  }

  const course = await Course.findByPk(id);
  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  const ta = await User.findOne({
    where: { id: user_id, role: 'ta' },
  });
  if (!ta) {
    throw new ApiError(400, 'ไม่พบผู้ช่วยสอนที่ระบุ');
  }

  // Check duplicate
  const existing = await CourseTA.findOne({
    where: { course_id: id, user_id },
  });
  if (existing) {
    throw new ApiError(400, 'ผู้ช่วยสอนนี้อยู่ในรายวิชาแล้ว');
  }

  // Cross-role conflict: check if this TA's email matches a student enrolled in this course
  if (ta.email) {
    const studentWithSameEmail = await Student.findOne({ where: { email: ta.email } });
    if (studentWithSameEmail) {
      const sectionsInCourse = await CourseSection.findAll({ where: { course_id: id }, attributes: ['id'] });
      const sectionIds = sectionsInCourse.map(s => s.id);
      if (sectionIds.length > 0) {
        const enrolledAsStudent = await CourseSectionStudent.findOne({
          where: { course_section_id: { [Op.in]: sectionIds }, student_id: studentWithSameEmail.id },
        });
        if (enrolledAsStudent) {
          throw new ApiError(400, `ไม่สามารถเพิ่มได้ — ${ta.full_name} (${ta.email}) เป็นนักศึกษาในรายวิชานี้อยู่แล้ว`);
        }
      }
    }
  }

  await CourseTA.create({ course_id: id, user_id });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'add_ta', category: 'member', targetType: 'ta', targetId: user_id, targetName: ta.full_name });

  res.status(201).json({
    success: true,
    message: 'เพิ่มผู้ช่วยสอนสำเร็จ',
    data: ta.toSafeObject(),
  });
});

/**
 * Add multiple TAs to course
 * @route POST /api/courses/:id/tas/bulk
 */
const bulkAddTAs = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { user_ids } = req.body; // Array of user IDs
  const currentUser = req.user;

  if (!Array.isArray(user_ids) || user_ids.length === 0) {
    throw new ApiError(400, 'กรุณาระบุรายชื่อผู้ช่วยสอน');
  }

  // Check course access (only admin or instructor of this course)
  if (currentUser.role !== 'admin') {
    const course = await Course.findByPk(id);
    if (!course || course.instructor_id !== currentUser.id) {
      throw new ApiError(403, 'คุณไม่มีสิทธิ์เพิ่มผู้ช่วยสอนในรายวิชานี้');
    }
  }

  const course = await Course.findByPk(id);
  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  // Validate all TAs exist
  const tas = await User.findAll({
    where: { id: user_ids, role: 'ta' },
  });

  if (tas.length === 0) {
    throw new ApiError(400, 'ไม่พบผู้ช่วยสอนที่ระบุ');
  }

  // Get existing TAs in course
  const existingTAs = await CourseTA.findAll({
    where: { course_id: id, user_id: user_ids },
  });
  const existingTAIds = existingTAs.map(ct => ct.user_id);

  // Filter out TAs that are already in the course
  const newTAs = tas.filter(ta => !existingTAIds.includes(ta.id));

  if (newTAs.length === 0) {
    throw new ApiError(400, 'ผู้ช่วยสอนที่เลือกทั้งหมดอยู่ในรายวิชาแล้ว');
  }

  // Cross-role conflict: check if any TA's email matches a student enrolled in this course
  const taEmails = newTAs.map(t => t.email).filter(Boolean);
  if (taEmails.length > 0) {
    const studentsWithSameEmail = await Student.findAll({ where: { email: { [Op.in]: taEmails } } });
    if (studentsWithSameEmail.length > 0) {
      const sectionsInCourse = await CourseSection.findAll({ where: { course_id: id }, attributes: ['id'] });
      const sectionIds = sectionsInCourse.map(s => s.id);
      if (sectionIds.length > 0) {
        const studentIds = studentsWithSameEmail.map(s => s.id);
        const enrolledConflicts = await CourseSectionStudent.findAll({
          where: { course_section_id: { [Op.in]: sectionIds }, student_id: { [Op.in]: studentIds } },
          include: [{ model: Student, as: 'student', attributes: ['full_name', 'email'] }],
        });
        if (enrolledConflicts.length > 0) {
          const names = enrolledConflicts.map(e => e.student?.full_name || 'ไม่ทราบชื่อ').join(', ');
          throw new ApiError(400, `ไม่สามารถเพิ่มได้ — ${names} เป็นนักศึกษาในรายวิชานี้อยู่แล้ว`);
        }
      }
    }
  }

  // Bulk create
  await CourseTA.bulkCreate(
    newTAs.map(ta => ({ course_id: id, user_id: ta.id }))
  );

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'bulk_add_tas', category: 'member', targetType: 'ta', targetName: `${newTAs.length} คน`, detail: { added: newTAs.map(t => ({ id: t.id, name: t.full_name })) } });

  res.status(201).json({
    success: true,
    message: `เพิ่มผู้ช่วยสอน ${newTAs.length} คนสำเร็จ`,
    data: {
      added: newTAs.map(ta => ta.toSafeObject()),
      skipped: existingTAIds.length,
    },
  });
});

/**
 * Remove TA from course
 * @route DELETE /api/courses/:id/tas/:userId
 */
const removeTA = asyncHandler(async (req, res) => {
  const { id, userId } = req.params;
  const currentUser = req.user;

  // Check course access (only admin or instructor of this course)
  if (currentUser.role !== 'admin') {
    const course = await Course.findByPk(id);
    if (!course || course.instructor_id !== currentUser.id) {
      throw new ApiError(403, 'คุณไม่มีสิทธิ์นำผู้ช่วยสอนออกจากรายวิชานี้');
    }
  }

  const deleted = await CourseTA.destroy({
    where: { course_id: id, user_id: userId },
  });

  if (!deleted) {
    throw new ApiError(404, 'ไม่พบผู้ช่วยสอนในรายวิชานี้');
  }

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'remove_ta', category: 'member', targetType: 'ta', targetId: userId });

  res.json({
    success: true,
    message: 'นำผู้ช่วยสอนออกสำเร็จ',
  });
});

// ============================================
// Instructor Management
// ============================================

/**
 * Add instructor to course
 * @route POST /api/courses/:id/instructors
 */
const addInstructor = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { user_id } = req.body;
  const currentUser = req.user;

  // Check course access (only admin or existing instructor of this course)
  const course = await Course.findByPk(id, {
    include: [{
      model: User,
      as: 'instructors',
      attributes: ['id'],
    }],
  });
  
  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  if (currentUser.role !== 'admin') {
    const isInstructor = course.instructors?.some(inst => inst.id === currentUser.id);
    if (!isInstructor) {
      throw new ApiError(403, 'คุณไม่มีสิทธิ์เพิ่มอาจารย์ในรายวิชานี้');
    }
  }

  const instructor = await User.findOne({
    where: { id: user_id, role: 'instructor' },
  });
  if (!instructor) {
    throw new ApiError(400, 'ไม่พบอาจารย์ที่ระบุ');
  }

  // Check duplicate
  const existing = await CourseInstructor.findOne({
    where: { course_id: id, user_id },
  });
  if (existing) {
    throw new ApiError(400, 'อาจารย์นี้อยู่ในรายวิชาแล้ว');
  }

  await CourseInstructor.create({ 
    course_id: id, 
    user_id,
    is_primary: false, // New instructors added later are not primary
  });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'add_instructor', category: 'member', targetType: 'instructor', targetId: user_id, targetName: instructor.full_name });

  res.status(201).json({
    success: true,
    message: 'เพิ่มอาจารย์สำเร็จ',
    data: instructor.toSafeObject(),
  });
});

/**
 * Add multiple instructors to course
 * @route POST /api/courses/:id/instructors/bulk
 */
const bulkAddInstructors = asyncHandler(async (req, res) => {
  const { id } = req.params;
  const { user_ids } = req.body; // Array of user IDs
  const currentUser = req.user;

  if (!Array.isArray(user_ids) || user_ids.length === 0) {
    throw new ApiError(400, 'กรุณาระบุรายชื่ออาจารย์');
  }

  // Check course access
  const course = await Course.findByPk(id, {
    include: [{
      model: User,
      as: 'instructors',
      attributes: ['id'],
    }],
  });
  
  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  if (currentUser.role !== 'admin') {
    const isInstructor = course.instructors?.some(inst => inst.id === currentUser.id);
    if (!isInstructor) {
      throw new ApiError(403, 'คุณไม่มีสิทธิ์เพิ่มอาจารย์ในรายวิชานี้');
    }
  }

  // Validate all instructors exist
  const instructors = await User.findAll({
    where: { id: user_ids, role: 'instructor' },
  });

  if (instructors.length === 0) {
    throw new ApiError(400, 'ไม่พบอาจารย์ที่ระบุ');
  }

  // Get existing instructors in course
  const existingInstructors = await CourseInstructor.findAll({
    where: { course_id: id, user_id: user_ids },
  });
  const existingIds = existingInstructors.map(ci => ci.user_id);

  // Filter out instructors that are already in the course
  const newInstructors = instructors.filter(inst => !existingIds.includes(inst.id));

  if (newInstructors.length === 0) {
    throw new ApiError(400, 'อาจารย์ที่เลือกทั้งหมดอยู่ในรายวิชาแล้ว');
  }

  // Bulk create
  await CourseInstructor.bulkCreate(
    newInstructors.map(inst => ({ 
      course_id: id, 
      user_id: inst.id,
      is_primary: false,
    }))
  );

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'bulk_add_instructors', category: 'member', targetType: 'instructor', targetName: `${newInstructors.length} คน`, detail: { added: newInstructors.map(i => ({ id: i.id, name: i.full_name })) } });

  res.status(201).json({
    success: true,
    message: `เพิ่มอาจารย์ ${newInstructors.length} คนสำเร็จ`,
    data: {
      added: newInstructors.map(inst => inst.toSafeObject()),
      skipped: existingIds.length,
    },
  });
});

/**
 * Remove instructor from course
 * @route DELETE /api/courses/:id/instructors/:userId
 */
const removeInstructor = asyncHandler(async (req, res) => {
  const { id, userId } = req.params;
  const currentUser = req.user;

  // Check course access
  const course = await Course.findByPk(id, {
    include: [{
      model: User,
      as: 'instructors',
      attributes: ['id'],
    }],
  });
  
  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  if (currentUser.role !== 'admin') {
    const isInstructor = course.instructors?.some(inst => inst.id === currentUser.id);
    if (!isInstructor) {
      throw new ApiError(403, 'คุณไม่มีสิทธิ์นำอาจารย์ออกจากรายวิชานี้');
    }
  }

  // Check if trying to remove self
  if (parseInt(userId) === currentUser.id && currentUser.role !== 'admin') {
    throw new ApiError(400, 'ไม่สามารถนำตัวเองออกจากรายวิชาได้');
  }

  // Check if this is the last instructor (primary)
  const instructorRecord = await CourseInstructor.findOne({
    where: { course_id: id, user_id: userId },
  });

  if (!instructorRecord) {
    throw new ApiError(404, 'ไม่พบอาจารย์ในรายวิชานี้');
  }

  if (instructorRecord.is_primary) {
    // Check if there are other instructors
    const otherInstructors = await CourseInstructor.count({
      where: { 
        course_id: id, 
        user_id: { [Op.ne]: userId },
      },
    });

    if (otherInstructors === 0) {
      throw new ApiError(400, 'ไม่สามารถนำอาจารย์หลักออกได้ เนื่องจากเป็นอาจารย์คนเดียวในรายวิชา');
    }

    // Transfer primary status to another instructor
    const nextInstructor = await CourseInstructor.findOne({
      where: { 
        course_id: id, 
        user_id: { [Op.ne]: userId },
      },
      order: [['assigned_at', 'ASC']],
    });

    if (nextInstructor) {
      await nextInstructor.update({ is_primary: true });
      // Also update course.instructor_id for backward compatibility
      await course.update({ instructor_id: nextInstructor.user_id });
    }
  }

  await instructorRecord.destroy();

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'remove_instructor', category: 'member', targetType: 'instructor', targetId: userId });

  res.json({
    success: true,
    message: 'นำอาจารย์ออกจากรายวิชาสำเร็จ',
  });
});

// ============================================
// Student Management
// ============================================

/**
 * Get students in section
 * @route GET /api/courses/:id/sections/:sectionId/students
 */
const getSectionStudents = asyncHandler(async (req, res) => {
  const { id, sectionId } = req.params;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(id, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const section = await CourseSection.findOne({
    where: { id: sectionId, course_id: id },
  });

  if (!section) {
    throw new ApiError(404, 'ไม่พบกลุ่มเรียน');
  }

  const students = await CourseSectionStudent.findAll({
    where: { course_section_id: sectionId },
    include: [{
      model: Student,
      as: 'student',
      attributes: ['id', 'student_id', 'full_name', 'email', 'is_active'],
    }],
    order: [['enrolled_at', 'ASC']],
  });

  res.json({
    success: true,
    data: students.map(s => ({
      ...s.student.toJSON(),
      enrolled_at: s.enrolled_at,
    })),
  });
});

/**
 * Add student to section
 * @route POST /api/courses/:id/sections/:sectionId/students
 */
const addStudentToSection = asyncHandler(async (req, res) => {
  const { id, sectionId } = req.params;
  const { student_id } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(id, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const section = await CourseSection.findOne({
    where: { id: sectionId, course_id: id },
  });
  if (!section) {
    throw new ApiError(404, 'ไม่พบกลุ่มเรียน');
  }

  const student = await Student.findByPk(student_id);
  if (!student) {
    throw new ApiError(400, 'ไม่พบนักศึกษาที่ระบุ');
  }

  // Check if student already in ANY section of this course
  const sectionsInCourse = await CourseSection.findAll({
    where: { course_id: id },
    attributes: ['id'],
  });
  const sectionIds = sectionsInCourse.map(s => s.id);
  
  const existingInCourse = await CourseSectionStudent.findOne({
    where: { 
      course_section_id: { [Op.in]: sectionIds },
      student_id 
    },
  });
  if (existingInCourse) {
    throw new ApiError(400, 'นักศึกษานี้อยู่ในรายวิชานี้แล้ว');
  }

  // Cross-role conflict: check if this student's email matches a TA in this course
  if (student.email) {
    const taUser = await User.findOne({ where: { email: student.email, role: 'ta' } });
    if (taUser) {
      const isTA = await CourseTA.findOne({ where: { course_id: id, user_id: taUser.id } });
      if (isTA) {
        throw new ApiError(400, `ไม่สามารถเพิ่มได้ — ${student.full_name} (${student.email}) เป็นผู้ช่วยสอน (TA) ในรายวิชานี้อยู่แล้ว`);
      }
    }
  }

  await CourseSectionStudent.create({
    course_section_id: sectionId,
    student_id,
  });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'add_student', category: 'member', targetType: 'student', targetId: student_id, targetName: student.full_name });

  res.status(201).json({
    success: true,
    message: 'เพิ่มนักศึกษาสำเร็จ',
    data: student,
  });
});

/**
 * Bulk add students to section
 * @route POST /api/courses/:id/sections/:sectionId/students/bulk
 */
const bulkAddStudentsToSection = asyncHandler(async (req, res) => {
  const { id, sectionId } = req.params;
  const { student_ids } = req.body;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(id, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const section = await CourseSection.findOne({
    where: { id: sectionId, course_id: id },
  });
  if (!section) {
    throw new ApiError(404, 'ไม่พบกลุ่มเรียน');
  }

  if (!student_ids || !Array.isArray(student_ids) || student_ids.length === 0) {
    throw new ApiError(400, 'กรุณาระบุรายชื่อนักศึกษา');
  }

  // Get all sections in this course to check for duplicates
  const sectionsInCourse = await CourseSection.findAll({
    where: { course_id: id },
    attributes: ['id'],
  });
  const sectionIds = sectionsInCourse.map(s => s.id);

  // Get already enrolled students in this course
  const existingEnrollments = await CourseSectionStudent.findAll({
    where: { 
      course_section_id: { [Op.in]: sectionIds },
      student_id: { [Op.in]: student_ids }
    },
    attributes: ['student_id'],
  });
  const alreadyEnrolledIds = new Set(existingEnrollments.map(e => e.student_id));

  // Filter out already enrolled students
  const newStudentIds = student_ids.filter(id => !alreadyEnrolledIds.has(id));

  if (newStudentIds.length === 0) {
    throw new ApiError(400, 'นักศึกษาทั้งหมดอยู่ในรายวิชานี้แล้ว');
  }

  // Verify all students exist
  const students = await Student.findAll({
    where: { id: { [Op.in]: newStudentIds } },
  });

  // Cross-role conflict: check if any student's email matches a TA in this course
  const studentEmails = students.map(s => s.email).filter(Boolean);
  if (studentEmails.length > 0) {
    const taUsers = await User.findAll({ where: { email: { [Op.in]: studentEmails }, role: 'ta' } });
    if (taUsers.length > 0) {
      const taUserIds = taUsers.map(u => u.id);
      const taConflicts = await CourseTA.findAll({ where: { course_id: id, user_id: { [Op.in]: taUserIds } } });
      if (taConflicts.length > 0) {
        const conflictEmails = new Set(taConflicts.map(tc => taUsers.find(u => u.id === tc.user_id)?.email));
        const conflictStudents = students.filter(s => s.email && conflictEmails.has(s.email));
        const names = conflictStudents.map(s => s.full_name).join(', ');
        throw new ApiError(400, `ไม่สามารถเพิ่มได้ — ${names} เป็นผู้ช่วยสอน (TA) ในรายวิชานี้อยู่แล้ว`);
      }
    }
  }

  const validStudentIds = students.map(s => s.id);

  // Bulk create enrollments
  const enrollments = validStudentIds.map(studentId => ({
    course_section_id: parseInt(sectionId),
    student_id: studentId,
  }));

  await CourseSectionStudent.bulkCreate(enrollments, { ignoreDuplicates: true });

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'bulk_add_students', category: 'member', targetType: 'student', targetName: `${validStudentIds.length} คน`, detail: { sectionId: sectionId, count: validStudentIds.length } });

  res.status(201).json({
    success: true,
    message: `เพิ่มนักศึกษาสำเร็จ ${validStudentIds.length} คน`,
    data: {
      addedCount: validStudentIds.length,
      skippedCount: student_ids.length - validStudentIds.length,
      addedStudentIds: validStudentIds,
    },
  });
});

/**
 * Remove student from section
 * @route DELETE /api/courses/:id/sections/:sectionId/students/:studentId
 */
const removeStudentFromSection = asyncHandler(async (req, res) => {
  const { id, sectionId, studentId } = req.params;
  const currentUser = req.user;

  // Check course access
  const hasAccess = await checkCourseAccess(id, currentUser);
  if (!hasAccess) {
    throw new ApiError(403, 'คุณไม่มีสิทธิ์เข้าถึงรายวิชานี้');
  }

  const deleted = await CourseSectionStudent.destroy({
    where: { course_section_id: sectionId, student_id: studentId },
  });

  if (!deleted) {
    throw new ApiError(404, 'ไม่พบนักศึกษาในกลุ่มเรียนนี้');
  }

  logCourseActivity({ courseId: id, actorUserId: req.user.id, action: 'remove_student', category: 'member', targetType: 'student', targetId: studentId });

  res.json({
    success: true,
    message: 'นำนักศึกษาออกสำเร็จ',
  });
});

/**
 * Get instructors list for dropdown
 * @route GET /api/courses/instructors
 */
const getInstructors = asyncHandler(async (req, res) => {
  const instructors = await User.findAll({
    where: { role: 'instructor', is_active: true },
    attributes: ['id', 'full_name', 'email', 'username', 'avatar'],
    order: [['full_name', 'ASC']],
  });

  res.json({
    success: true,
    data: instructors,
  });
});

/**
 * Get TAs list for dropdown
 * @route GET /api/courses/tas-list
 */
const getTAsList = asyncHandler(async (req, res) => {
  const tas = await User.findAll({
    where: { role: 'ta', is_active: true },
    attributes: ['id', 'full_name', 'email', 'username', 'avatar'],
    order: [['full_name', 'ASC']],
  });

  res.json({
    success: true,
    data: tas,
  });
});

/**
 * Get my courses (for instructor/TA)
 * @route GET /api/courses/my-courses
 */
const getMyCourses = asyncHandler(async (req, res) => {
  const userId = req.user.id;
  const userRole = req.user.role;
  const {
    page = 1,
    limit = 12,
    search = '',
    year = '',
    semester = '',
    status = '',
    sortBy = 'created_at',
    sortOrder = 'DESC',
  } = req.query;

  // Debug log
  logger.debug('getMyCourses params:', { page, limit, search, year, semester, status, userId, userRole });

  // Build where clause with proper AND/OR structure
  const whereConditions = [];

  // Note: For instructor, we now use CourseInstructor join instead of instructor_id

  // Search filter (OR between code and name)
  if (search && search.trim()) {
    whereConditions.push({
      [Op.or]: [
        { code: { [Op.like]: `%${search.trim()}%` } },
        { name: { [Op.like]: `%${search.trim()}%` } },
      ],
    });
  }

  // Year filter
  if (year && !isNaN(parseInt(year))) {
    whereConditions.push({ year: parseInt(year) });
  }

  // Semester filter
  if (semester && !isNaN(parseInt(semester))) {
    whereConditions.push({ semester: parseInt(semester) });
  }

  // Status filter
  if (status === 'active') {
    whereConditions.push({ is_active: true });
  } else if (status === 'inactive') {
    whereConditions.push({ is_active: false });
  }

  // Combine all conditions with AND
  const where = whereConditions.length > 0 ? { [Op.and]: whereConditions } : {};

  // Debug log
  logger.debug('getMyCourses whereConditions:', JSON.stringify(whereConditions, null, 2));
  logger.debug('getMyCourses final where:', JSON.stringify(where, null, 2));

  // Calculate offset
  const offset = (parseInt(page) - 1) * parseInt(limit);

  // Valid sort columns
  const validSortColumns = ['code', 'name', 'year', 'semester', 'is_active', 'created_at', 'updated_at'];
  const orderColumn = validSortColumns.includes(sortBy) ? sortBy : 'created_at';
  const orderDirection = sortOrder.toUpperCase() === 'ASC' ? 'ASC' : 'DESC';

  let courses, count;

  if (userRole === 'ta') {
    // For TA - get courses they are assigned to
    const { count: taCount, rows: taCourses } = await Course.findAndCountAll({
      where,
      limit: parseInt(limit),
      offset,
      order: [[orderColumn, orderDirection]],
      include: [
        {
          model: User,
          as: 'instructor',
          attributes: ['id', 'full_name', 'email', 'avatar'],
        },
        {
          model: CourseSection,
          as: 'sections',
          attributes: ['id', 'section_no', 'note'],
        },
        {
          model: User,
          as: 'tas',
          attributes: ['id'],
          through: { attributes: [] },
          where: { id: userId },
          required: true, // INNER JOIN - only courses where this TA is assigned
        },
      ],
    });
    courses = taCourses;
    count = taCount;
  } else {
    // For instructor - courses they are assigned to via CourseInstructor
    const { count: instructorCount, rows: instructorCourses } = await Course.findAndCountAll({
      where,
      limit: parseInt(limit),
      offset,
      order: [[orderColumn, orderDirection]],
      include: [
        {
          model: User,
          as: 'instructor',
          attributes: ['id', 'full_name', 'email', 'avatar'],
        },
        {
          model: CourseSection,
          as: 'sections',
          attributes: ['id', 'section_no', 'note'],
        },
        {
          model: User,
          as: 'instructors',
          attributes: ['id'],
          through: { attributes: ['is_primary'] },
          where: { id: userId },
          required: true, // INNER JOIN - only courses where this instructor is assigned
        },
      ],
    });
    courses = instructorCourses;
    count = instructorCount;
  }

  // Get TA count and student count for each course
  const coursesWithCounts = await Promise.all(
    courses.map(async (course) => {
      const taCount = await CourseTA.count({ where: { course_id: course.id } });
      const studentCount = await CourseSectionStudent.count({
        include: [{
          model: CourseSection,
          as: 'section',
          where: { course_id: course.id },
          attributes: [],
        }],
      });
      return {
        ...course.toJSON(),
        taCount,
        studentCount,
      };
    })
  );

  // Calculate pagination info
  const totalPages = Math.ceil(count / parseInt(limit));

  res.json({
    success: true,
    data: {
      courses: coursesWithCounts,
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
 * Get my courses stats (for instructor/TA)
 * @route GET /api/courses/my-courses/stats
 */
const getMyCoursesStats = asyncHandler(async (req, res) => {
  const userId = req.user.id;
  const userRole = req.user.role;

  let courseIds = [];

  if (userRole === 'instructor') {
    // Get courses where user is instructor (via CourseInstructor table)
    const instructorAssignments = await CourseInstructor.findAll({
      where: { user_id: userId },
      attributes: ['course_id'],
      raw: true,
    });
    courseIds = instructorAssignments.map(i => i.course_id);
  } else if (userRole === 'ta') {
    // Get courses where user is TA
    const taAssignments = await CourseTA.findAll({
      where: { user_id: userId },
      attributes: ['course_id'],
      raw: true,
    });
    courseIds = taAssignments.map(t => t.course_id);
  }

  // Count stats
  const total = courseIds.length;
  
  let active = 0;
  let inactive = 0;
  let years = [];
  
  if (total > 0) {
    active = await Course.count({ 
      where: { 
        id: courseIds, 
        is_active: true 
      } 
    });
    inactive = await Course.count({ 
      where: { 
        id: courseIds, 
        is_active: false 
      } 
    });

    // Get unique years
    const yearsResult = await Course.findAll({
      where: { id: courseIds },
      attributes: ['year'],
      group: ['year'],
      order: [['year', 'DESC']],
      raw: true,
    });
    years = yearsResult.map(y => y.year);
  }

  res.json({
    success: true,
    data: {
      total,
      byStatus: {
        active,
        inactive,
      },
      years,
    },
  });
});

/**
 * Get course overview dashboard data with real statistics
 * @route GET /api/courses/:id/overview
 */
const getCourseOverview = asyncHandler(async (req, res) => {
  const { id } = req.params;
  
  logger.debug(`[Overview] Fetching overview for course: ${id}`);
  const startTime = Date.now();

  // Get course with sections and TAs
  const course = await Course.findByPk(id, {
    include: [
      {
        model: CourseSection,
        as: 'sections',
        attributes: ['id', 'section_no'],
      },
      {
        model: User,
        as: 'tas',
        attributes: ['id', 'full_name', 'email', 'avatar'],
        through: { attributes: ['assigned_at'] },
      },
    ],
  });

  if (!course) {
    throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
  }

  // Get all students in course
  const sectionIds = course.sections.map(s => s.id);
  
  let allStudents = [];
  let totalStudents = 0;

  if (sectionIds.length > 0) {
    const enrollments = await CourseSectionStudent.findAll({
      where: { course_section_id: sectionIds },
      include: [
        {
          model: Student,
          as: 'student',
          attributes: ['id', 'student_id', 'full_name', 'email', 'is_active'],
        },
        {
          model: CourseSection,
          as: 'section',
          attributes: ['id', 'section_no'],
        },
      ],
    });

    allStudents = enrollments.map(e => ({
      id: e.student.id,
      student_id: e.student.student_id,
      full_name: e.student.full_name,
      email: e.student.email,
      section_id: e.section.id,
      section_no: e.section.section_no,
      enrolled_at: e.enrolled_at || e.createdAt,
    }));

    totalStudents = allStudents.length;
  }

  // ========================================
  // Get real assignment statistics
  // ========================================
  const assignments = await Assignment.findAll({
    where: { 
      course_id: id,
      is_active: true  // Only get active (non-deleted) assignments
    },
    include: [
      {
        model: AssignmentSubItem,
        as: 'subItems',
        attributes: ['id', 'name', 'max_score'],
      },
    ],
    order: [['created_at', 'DESC']],
  });

  const totalAssignments = assignments.length;
  
  // Calculate total max scores for the course
  let totalMaxScore = 0;
  assignments.forEach(assignment => {
    if (assignment.subItems && assignment.subItems.length > 0) {
      totalMaxScore += assignment.subItems.reduce((sum, item) => sum + (parseFloat(item.max_score) || 0), 0);
    } else {
      totalMaxScore += parseFloat(assignment.max_score) || 0;
    }
  });

  // ========================================
  // Get all scores for this course's assignments
  // ========================================
  const assignmentIds = assignments.map(a => a.id);
  let allScores = [];
  
  if (assignmentIds.length > 0) {
    allScores = await Score.findAll({
      where: { 
        assignment_id: { [Op.in]: assignmentIds },
        score: { [Op.not]: null },
      },
      include: [
        {
          model: User,
          as: 'grader',
          attributes: ['id', 'full_name'],
        },
        {
          model: Assignment,
          as: 'assignment',
          attributes: ['id', 'name', 'max_score', 'assignment_type'],
        },
      ],
    });
  }

  // ========================================
  // Calculate student scores and rankings
  // ========================================
  const studentScoreMap = new Map();
  
  // Initialize all students with 0 score
  allStudents.forEach(student => {
    studentScoreMap.set(student.id, {
      id: student.id,
      student_id: student.student_id,
      full_name: student.full_name,
      totalScore: 0,
      assignmentsGraded: 0,
    });
  });

  // Calculate total scores per student
  allScores.forEach(score => {
    if (score.student_id && studentScoreMap.has(score.student_id)) {
      const studentData = studentScoreMap.get(score.student_id);
      studentData.totalScore += parseFloat(score.score) || 0;
      studentData.assignmentsGraded += 1;
    }
  });

  // Convert to array and sort by score
  const studentScores = Array.from(studentScoreMap.values());
  studentScores.sort((a, b) => b.totalScore - a.totalScore);

  // Top 5 students
  const topStudents = studentScores
    .filter(s => s.totalScore > 0)
    .slice(0, 5)
    .map(s => ({
      ...s,
      percentage: totalMaxScore > 0 ? Math.round((s.totalScore / totalMaxScore) * 100) : 0,
    }));

  // Low performers (< attention_threshold% and have at least 1 graded assignment)
  const attentionThreshold = course.attention_threshold || 60;
  const lowPerformers = studentScores
    .filter(s => {
      const percentage = totalMaxScore > 0 ? (s.totalScore / totalMaxScore) * 100 : 0;
      return s.assignmentsGraded > 0 && percentage < attentionThreshold;
    })
    .slice(0, 8)
    .map(s => ({
      ...s,
      percentage: totalMaxScore > 0 ? Math.round((s.totalScore / totalMaxScore) * 100) : 0,
    }));

  // ========================================
  // Calculate submission rate
  // ========================================
  let totalExpectedScores = 0;
  let totalReceivedScores = 0;

  assignments.forEach(assignment => {
    // individual and assignment types are both individual work
    const isGroupAssignment = assignment.assignment_type !== 'individual' && assignment.assignment_type !== 'assignment';
    
    if (!isGroupAssignment) {
      // For individual assignments (Lab and Assignment/Homework)
      const studentsWithScores = new Set(
        allScores
          .filter(s => s.assignment_id === assignment.id && s.student_id)
          .map(s => s.student_id)
      );
      totalReceivedScores += studentsWithScores.size;
      totalExpectedScores += totalStudents;
    }
  });

  const submissionRate = totalExpectedScores > 0 
    ? Math.round((totalReceivedScores / totalExpectedScores) * 100) 
    : 0;

  // ========================================
  // ✅ OPTIMIZED: Calculate TA activity with batch queries
  // ========================================
  let taActivity = [];
  if ((course.tas || []).length > 0 && assignmentIds.length > 0) {
    const taIds = course.tas.map(ta => ta.id);
    
    // Single query for all TA grading counts
    const [taGradingStats] = await sequelize.query(`
      SELECT 
        graded_by,
        COUNT(*) as graded_count,
        MAX(graded_at) as last_graded_at
      FROM scores
      WHERE assignment_id IN (?)
        AND graded_by IN (?)
        AND score IS NOT NULL
      GROUP BY graded_by
    `, {
      replacements: [assignmentIds, taIds],
    });
    
    const taStatsMap = {};
    taGradingStats.forEach(row => {
      taStatsMap[row.graded_by] = {
        gradedCount: parseInt(row.graded_count),
        lastActive: row.last_graded_at,
      };
    });
    
    taActivity = course.tas.map(ta => ({
      id: ta.id,
      full_name: ta.full_name,
      email: ta.email,
      avatar: ta.avatar,
      assignedAt: ta.CourseTA?.assigned_at,
      gradedCount: taStatsMap[ta.id]?.gradedCount || 0,
      lastActive: taStatsMap[ta.id]?.lastActive || null,
    }));
    
    // Sort TA by graded count
    taActivity.sort((a, b) => b.gradedCount - a.gradedCount);
  }

  // ========================================
  // ✅ OPTIMIZED: Get attendance statistics with single query
  // ========================================
  let attendanceRate = 0;
  let totalAttendanceSessions = 0;
  
  // Single query for both session count and attendance stats
  const [attendanceStats] = await sequelize.query(`
    SELECT 
      COUNT(DISTINCT ats.id) as total_sessions,
      SUM(CASE WHEN ar.status IN ('present', 'late') THEN 1 ELSE 0 END) as present_count,
      COUNT(ar.id) as total_records
    FROM attendance_sessions ats
    LEFT JOIN attendance_records ar ON ats.id = ar.attendance_session_id
    WHERE ats.course_id = ?
  `, {
    replacements: [id],
  });
  
  if (attendanceStats && attendanceStats[0]) {
    totalAttendanceSessions = parseInt(attendanceStats[0].total_sessions) || 0;
    const presentCount = parseInt(attendanceStats[0].present_count) || 0;
    const totalExpected = totalAttendanceSessions * totalStudents;
    attendanceRate = totalExpected > 0 ? Math.round((presentCount / totalExpected) * 100) : 0;
  }

  // ========================================
  // ✅ OPTIMIZED: Get recent activities with limit (last 10)
  // ========================================
  const recentScores = assignmentIds.length > 0 ? await Score.findAll({
    where: {
      assignment_id: { [Op.in]: assignmentIds },
      score: { [Op.not]: null },
    },
    include: [
      {
        model: User,
        as: 'grader',
        attributes: ['id', 'full_name', 'avatar'],
      },
      {
        model: Student,
        as: 'student',
        attributes: ['id', 'full_name', 'student_id'],
      },
      {
        model: Assignment,
        as: 'assignment',
        attributes: ['id', 'name'],
      },
    ],
    order: [['graded_at', 'DESC']],
    limit: 10,
  }) : [];

  const recentActivities = recentScores.map(score => ({
    id: score.id,
    type: 'score',
    description: `ให้คะแนน ${score.student?.full_name || 'กลุ่ม'} - ${score.assignment?.name}`,
    score: parseFloat(score.score),
    user: score.grader ? {
      id: score.grader.id,
      full_name: score.grader.full_name,
      avatar: score.grader.avatar,
    } : null,
    timestamp: score.graded_at,
  }));

  // ========================================
  // Get score distribution for chart การคำนวณการกระจายคะแนนโดยใช้ข้อมูลคะแนนที่มีอยู่แล้ว
  // ========================================
  const scoreDistribution = {
    excellent: 0, // >= 80%
    good: 0,      // 60-79%
    average: 0,   // 40-59%
    poor: 0,      // < 40%
  };

  studentScores.forEach(student => {
    if (student.assignmentsGraded === 0) return;
    const percentage = totalMaxScore > 0 ? (student.totalScore / totalMaxScore) * 100 : 0;
    if (percentage >= 80) scoreDistribution.excellent++;
    else if (percentage >= 60) scoreDistribution.good++;
    else if (percentage >= 40) scoreDistribution.average++;
    else scoreDistribution.poor++;
  });

  // ========================================
  // ✅ OPTIMIZED: Assignment statistics for table - no additional queries
  // ========================================
  const assignmentStats = assignments.slice(0, 10).map(assignment => {
    // individual and assignment types are both individual work
    const isGroupAssignment = assignment.assignment_type !== 'individual' && assignment.assignment_type !== 'assignment';
    
    // Get scores for this assignment from already-fetched data
    const assignmentScores = allScores.filter(s => s.assignment_id === assignment.id);
    
    // Calculate average score per student/group (sum sub-item scores first)
    let avgScore = null;
    const hasSubItems = assignment.subItems && assignment.subItems.length > 0;

    if (assignmentScores.length > 0) {
      if (isGroupAssignment) {
        // Group by group_id, sum scores per group
        const groupTotals = new Map();
        for (const s of assignmentScores) {
          if (!s.group_id) continue;
          groupTotals.set(s.group_id, (groupTotals.get(s.group_id) || 0) + (parseFloat(s.score) || 0));
        }
        if (groupTotals.size > 0) {
          const totals = Array.from(groupTotals.values());
          avgScore = totals.reduce((a, b) => a + b, 0) / totals.length;
        }
      } else {
        // Group by student_id, sum scores per student
        const studentTotals = new Map();
        for (const s of assignmentScores) {
          if (!s.student_id) continue;
          studentTotals.set(s.student_id, (studentTotals.get(s.student_id) || 0) + (parseFloat(s.score) || 0));
        }
        if (studentTotals.size > 0) {
          const totals = Array.from(studentTotals.values());
          avgScore = totals.reduce((a, b) => a + b, 0) / totals.length;
        }
      }
    }

    // Count unique students/groups scored
    let scoredCount = 0;
    let totalExpected = 0;
    
    if (isGroupAssignment) {
      scoredCount = new Set(assignmentScores.filter(s => s.group_id).map(s => s.group_id)).size;
      // For group assignments, we need to count total groups
      // This is approximate - ideally we'd query the groups table
      totalExpected = scoredCount; // Can't calculate not scored for groups easily
    } else {
      scoredCount = new Set(assignmentScores.filter(s => s.student_id).map(s => s.student_id)).size;
      totalExpected = totalStudents;
    }

    const notScoredCount = isGroupAssignment 
      ? 0
      : Math.max(0, totalStudents - scoredCount);

    const submittedRate = isGroupAssignment 
      ? (scoredCount > 0 ? 100 : 0) // Show 100% if any group scored
      : (totalStudents > 0 ? Math.round((scoredCount / totalStudents) * 100) : 0);

    // Calculate actual max score (considering sub-items)
    let actualMaxScore = 0;
    if (assignment.subItems && assignment.subItems.length > 0) {
      actualMaxScore = assignment.subItems.reduce((sum, item) => sum + (parseFloat(item.max_score) || 0), 0);
    } else {
      actualMaxScore = parseFloat(assignment.max_score) || 0;
    }

    return {
      id: assignment.id,
      name: assignment.name,
      max_score: actualMaxScore,
      assignment_type: assignment.assignment_type,
      is_score_visible: assignment.is_score_visible !== false, // default true
      avgScore: avgScore !== null ? Math.round(avgScore * 10) / 10 : null,
      scoredCount,
      notScoredCount,
      submittedRate,
      hasSubItems: assignment.subItems && assignment.subItems.length > 0,
      subItemsCount: assignment.subItems ? assignment.subItems.length : 0,
    };
  });  // ✅ Changed from async Promise.all to sync map

  // ========================================
  // Assignment statistics by type (NEW)
  // ========================================
  const assignmentStatsByType = {};
  assignments.forEach(assignment => {
    const type = assignment.assignment_type || 'individual';
    if (!assignmentStatsByType[type]) {
      assignmentStatsByType[type] = {
        count: 0,
        totalMaxScore: 0,
        totalScored: 0,
        totalExpected: 0,
      };
    }
    
    // Calculate actual max score for this assignment
    let assignmentMaxScore = 0;
    if (assignment.subItems && assignment.subItems.length > 0) {
      assignmentMaxScore = assignment.subItems.reduce((sum, item) => sum + (parseFloat(item.max_score) || 0), 0);
    } else {
      assignmentMaxScore = parseFloat(assignment.max_score) || 0;
    }
    
    assignmentStatsByType[type].count += 1;
    assignmentStatsByType[type].totalMaxScore += assignmentMaxScore;
    
    // Count scored for this assignment
    const isGroupAssignment = type !== 'individual' && type !== 'assignment';
    const assignmentScores = allScores.filter(s => s.assignment_id === assignment.id);
    
    if (isGroupAssignment) {
      const groupsScored = new Set(assignmentScores.filter(s => s.group_id).map(s => s.group_id)).size;
      assignmentStatsByType[type].totalScored += groupsScored > 0 ? 1 : 0; // Count assignment as scored if any group scored
      assignmentStatsByType[type].totalExpected += 1;
    } else {
      const studentsScored = new Set(assignmentScores.filter(s => s.student_id).map(s => s.student_id)).size;
      assignmentStatsByType[type].totalScored += studentsScored;
      assignmentStatsByType[type].totalExpected += totalStudents;
    }
  });

  // Calculate progress percentage for each type
  Object.keys(assignmentStatsByType).forEach(type => {
    const stats = assignmentStatsByType[type];
    stats.progressRate = stats.totalExpected > 0 
      ? Math.round((stats.totalScored / stats.totalExpected) * 100) 
      : 0;
  });

  // ========================================
  // Course summary
  // ========================================
  const summary = {
    totalStudents,
    totalSections: course.sections.length,
    totalTAs: (course.tas || []).length,
    totalAssignments,
    totalMaxScore,
    submissionRate,
    attendanceRate,
    totalAttendanceSessions,
    averageScore: studentScores.filter(s => s.assignmentsGraded > 0).length > 0
      ? Math.round(studentScores.filter(s => s.assignmentsGraded > 0).reduce((sum, s) => sum + s.totalScore, 0) / studentScores.filter(s => s.assignmentsGraded > 0).length * 10) / 10
      : 0,
    trend: submissionRate > 70 ? 'up' : submissionRate > 40 ? 'stable' : 'down',
    trendValue: submissionRate,
  };

  res.json({
    success: true,
    data: {
      summary,
      topStudents,
      lowPerformers,
      taActivity,
      assignments: assignmentStats,
      assignmentStatsByType,
      recentActivities,
      scoreDistribution,
    },
  });
  
  const endTime = Date.now();
  logger.debug(`[Overview] Completed for course ${id} in ${endTime - startTime}ms`);
});

module.exports = {
  getCourses,
  getCourseStats,
  getCourseById,
  createCourse,
  updateCourse,
  deleteCourse,
  toggleCourseStatus,
  addSection,
  updateSection,
  removeSection,
  addTA,
  bulkAddTAs,
  removeTA,
  addInstructor,
  bulkAddInstructors,
  removeInstructor,
  getSectionStudents,
  addStudentToSection,
  bulkAddStudentsToSection,
  removeStudentFromSection,
  getInstructors,
  getTAsList,
  getMyCourses,
  getMyCoursesStats,
  getCourseOverview,
};
