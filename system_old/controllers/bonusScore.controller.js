/**
 * BonusScore Controller - Handle bonus score related requests
 * คะแนนพิเศษจากการถามตอบในห้องเรียน
 */

const { BonusScore, Student, User, Course, CourseSection, CourseSectionStudent } = require('../models');
const { Op } = require('sequelize');
const { sequelize } = require('../config/database');
const ApiError = require('../utils/ApiError');
const asyncHandler = require('../utils/asyncHandler');

/**
 * Give bonus score to a student (+1 by default)
 * @route POST /api/bonus-scores
 */
const giveBonusScore = asyncHandler(async (req, res) => {
    const { course_id, student_id, score = 1, reason = 'ตอบคำถามในห้องเรียน' } = req.body;
    const userId = req.user.id;

    // Validate required fields
    if (!course_id || !student_id) {
        throw new ApiError(400, 'กรุณาระบุรหัสวิชาและรหัสนักศึกษา');
    }

    // Verify student exists
    const student = await Student.findByPk(student_id);
    if (!student) {
        throw new ApiError(404, 'ไม่พบข้อมูลนักศึกษา');
    }

    // Verify course exists
    const course = await Course.findByPk(course_id);
    if (!course) {
        throw new ApiError(404, 'ไม่พบข้อมูลรายวิชา');
    }

    // Create bonus score record
    const bonusScore = await BonusScore.create({
        course_id,
        student_id,
        score: parseFloat(score),
        reason,
        given_by: userId,
        given_at: new Date(),
    });

    // Fetch with associations for response
    const result = await BonusScore.findByPk(bonusScore.id, {
        include: [
            {
                model: Student,
                as: 'student',
                attributes: ['id', 'student_id', 'full_name'],
            },
            {
                model: User,
                as: 'giver',
                attributes: ['id', 'full_name'],
            },
        ],
    });

    // Calculate total bonus score for this student in this course
    const totalBonus = await BonusScore.sum('score', {
        where: {
            course_id,
            student_id,
        },
    });

    res.status(201).json({
        success: true,
        message: `ให้คะแนนพิเศษ ${score} คะแนนสำเร็จ`,
        data: {
            bonusScore: result,
            totalBonus: totalBonus || 0,
        },
    });
});

/**
 * Get bonus scores for a course (grouped by student)
 * @route GET /api/bonus-scores/course/:courseId
 */
const getBonusScoresByCourse = asyncHandler(async (req, res) => {
    const { courseId } = req.params;

    // Get all bonus scores for this course grouped by student
    const bonusScores = await BonusScore.findAll({
        where: { course_id: courseId },
        include: [
            {
                model: Student,
                as: 'student',
                attributes: ['id', 'student_id', 'full_name'],
            },
            {
                model: User,
                as: 'giver',
                attributes: ['id', 'full_name'],
            },
        ],
        order: [['given_at', 'DESC']],
    });

    // Group by student and calculate totals
    const studentTotals = {};
    bonusScores.forEach(bs => {
        const studentKey = bs.student_id;
        if (!studentTotals[studentKey]) {
            studentTotals[studentKey] = {
                student: bs.student,
                totalScore: 0,
                records: [],
            };
        }
        studentTotals[studentKey].totalScore += parseFloat(bs.score);
        studentTotals[studentKey].records.push({
            id: bs.id,
            score: parseFloat(bs.score),
            reason: bs.reason,
            giver: bs.giver,
            given_at: bs.given_at,
        });
    });

    // Convert to array and sort by total score descending
    const result = Object.values(studentTotals).sort((a, b) => b.totalScore - a.totalScore);

    res.json({
        success: true,
        data: {
            studentBonusScores: result,
            totalRecords: bonusScores.length,
        },
    });
});

/**
 * Get bonus score history for a specific student in a course
 * @route GET /api/bonus-scores/course/:courseId/student/:studentId
 */
const getStudentBonusHistory = asyncHandler(async (req, res) => {
    const { courseId, studentId } = req.params;

    const bonusScores = await BonusScore.findAll({
        where: {
            course_id: courseId,
            student_id: studentId,
        },
        include: [
            {
                model: User,
                as: 'giver',
                attributes: ['id', 'full_name'],
            },
        ],
        order: [['given_at', 'DESC']],
    });

    const totalScore = bonusScores.reduce((sum, bs) => sum + parseFloat(bs.score), 0);

    res.json({
        success: true,
        data: {
            records: bonusScores,
            totalScore,
        },
    });
});

/**
 * Get students enrolled in a course for bonus score selection
 * @route GET /api/bonus-scores/course/:courseId/students
 */
const getEnrolledStudentsForBonus = asyncHandler(async (req, res) => {
    const { courseId } = req.params;

    // Get all sections in this course
    const sections = await CourseSection.findAll({
        where: { course_id: courseId },
        attributes: ['id'],
    });

    const sectionIds = sections.map(s => s.id);

    if (sectionIds.length === 0) {
        return res.json({
            success: true,
            data: { students: [] },
        });
    }

    // Get enrolled students with their bonus scores
    const enrollments = await CourseSectionStudent.findAll({
        where: { course_section_id: { [Op.in]: sectionIds } },
        include: [
            {
                model: Student,
                as: 'student',
                attributes: ['id', 'student_id', 'full_name'],
            },
            {
                model: CourseSection,
                as: 'section',
                attributes: ['id', 'section_no'],
            },
        ],
    });

    // Get bonus scores for these students
    const studentIds = enrollments.map(e => e.student.id);
    const bonusScores = await BonusScore.findAll({
        where: {
            course_id: courseId,
            student_id: { [Op.in]: studentIds },
        },
        attributes: ['student_id', [sequelize.fn('SUM', sequelize.col('score')), 'total']],
        group: ['student_id'],
    });

    const bonusMap = {};
    bonusScores.forEach(bs => {
        bonusMap[bs.student_id] = parseFloat(bs.dataValues.total) || 0;
    });

    // Build result
    const students = enrollments.map(e => ({
        id: e.student.id,
        student_id: e.student.student_id,
        full_name: e.student.full_name,
        section_no: e.section.section_no,
        totalBonus: bonusMap[e.student.id] || 0,
    }));

    // Sort by student_id
    students.sort((a, b) => a.student_id.localeCompare(b.student_id));

    res.json({
        success: true,
        data: { students },
    });
});

/**
 * Delete a bonus score record
 * @route DELETE /api/bonus-scores/:id
 */
const deleteBonusScore = asyncHandler(async (req, res) => {
    const { id } = req.params;

    const bonusScore = await BonusScore.findByPk(id);
    if (!bonusScore) {
        throw new ApiError(404, 'ไม่พบข้อมูลคะแนนพิเศษ');
    }

    await bonusScore.destroy();

    res.json({
        success: true,
        message: 'ลบคะแนนพิเศษสำเร็จ',
    });
});

/**
 * Get bonus score summary for a course
 * @route GET /api/bonus-scores/course/:courseId/summary
 */
const getBonusScoreSummary = asyncHandler(async (req, res) => {
    const { courseId } = req.params;

    // Total bonus given
    const totalGiven = await BonusScore.sum('score', {
        where: { course_id: courseId },
    });

    // Total records
    const totalRecords = await BonusScore.count({
        where: { course_id: courseId },
    });

    // Unique students who received bonus
    const uniqueStudents = await BonusScore.count({
        where: { course_id: courseId },
        distinct: true,
        col: 'student_id',
    });

    // Top 5 students
    const topStudents = await BonusScore.findAll({
        where: { course_id: courseId },
        attributes: [
            'student_id',
            [sequelize.fn('SUM', sequelize.col('score')), 'total'],
        ],
        include: [
            {
                model: Student,
                as: 'student',
                attributes: ['student_id', 'full_name'],
            },
        ],
        group: ['student_id'],
        order: [[sequelize.fn('SUM', sequelize.col('score')), 'DESC']],
        limit: 5,
    });

    res.json({
        success: true,
        data: {
            totalGiven: totalGiven || 0,
            totalRecords,
            uniqueStudents,
            topStudents: topStudents.map(s => ({
                student_id: s.student.student_id,
                full_name: s.student.full_name,
                total: parseFloat(s.dataValues.total),
            })),
        },
    });
});

module.exports = {
    giveBonusScore,
    getBonusScoresByCourse,
    getStudentBonusHistory,
    getEnrolledStudentsForBonus,
    deleteBonusScore,
    getBonusScoreSummary,
};
