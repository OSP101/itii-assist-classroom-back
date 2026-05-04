/**
 * Exam Score Controller
 * จัดการคะแนนสอบกลางภาคและปลายภาค
 */

const { 
    ExamSetting, 
    ExamScore, 
    Course, 
    Student, 
    User, 
    CourseSectionStudent, 
    CourseSection 
} = require('../models');
const { Op } = require('sequelize');

/**
 * Get exam settings for a course
 * GET /api/courses/:courseId/exam-settings
 */
exports.getExamSettings = async (req, res, next) => {
    try {
        const { courseId } = req.params;

        // Check if course exists
        const course = await Course.findByPk(courseId);
        if (!course) {
            return res.status(404).json({
                success: false,
                message: 'ไม่พบรายวิชา',
            });
        }

        // Get or create default settings
        let settings = await ExamSetting.findAll({
            where: { course_id: courseId },
            order: [['exam_type', 'ASC'], ['component', 'ASC']],
        });

        // If no settings exist, create default 4 settings
        if (settings.length === 0) {
            const defaultSettings = [
                { course_id: courseId, exam_type: 'midterm', component: 'lecture', max_score: 0, is_active: false },
                { course_id: courseId, exam_type: 'midterm', component: 'lab', max_score: 0, is_active: false },
                { course_id: courseId, exam_type: 'final', component: 'lecture', max_score: 0, is_active: false },
                { course_id: courseId, exam_type: 'final', component: 'lab', max_score: 0, is_active: false },
            ];
            settings = await ExamSetting.bulkCreate(defaultSettings, { returning: true });
        }

        res.json({
            success: true,
            data: settings,
        });
    } catch (error) {
        next(error);
    }
};

/**
 * Update exam setting
 * PUT /api/courses/:courseId/exam-settings/:settingId
 */
exports.updateExamSetting = async (req, res, next) => {
    try {
        const { courseId, settingId } = req.params;
        const { max_score, is_visible, is_active } = req.body;

        const setting = await ExamSetting.findOne({
            where: { id: settingId, course_id: courseId },
        });

        if (!setting) {
            return res.status(404).json({
                success: false,
                message: 'ไม่พบการตั้งค่า',
            });
        }

        // Update fields
        if (max_score !== undefined) setting.max_score = max_score;
        if (is_visible !== undefined) setting.is_visible = is_visible;
        if (is_active !== undefined) setting.is_active = is_active;

        await setting.save();

        res.json({
            success: true,
            data: setting,
            message: 'บันทึกการตั้งค่าสำเร็จ',
        });
    } catch (error) {
        next(error);
    }
};

/**
 * Get exam scores for a course
 * GET /api/courses/:courseId/exam-scores
 */
exports.getExamScores = async (req, res, next) => {
    try {
        const { courseId } = req.params;

        // Get all enrolled students in the course
        const sections = await CourseSection.findAll({
            where: { course_id: courseId },
            include: [{
                model: Student,
                as: 'students',
                through: { attributes: [] },
                attributes: ['id', 'student_id', 'full_name'],
            }],
        });

        // Flatten students and add section info
        const studentMap = new Map();
        sections.forEach(section => {
            section.students.forEach(student => {
                if (!studentMap.has(student.id)) {
                    studentMap.set(student.id, {
                        id: student.id,
                        student_id: student.student_id,
                        full_name: student.full_name,
                        section_no: section.section_no,
                    });
                }
            });
        });
        const students = Array.from(studentMap.values()).sort((a, b) => 
            a.student_id.localeCompare(b.student_id)
        );

        // Get settings with scores
        const settings = await ExamSetting.findAll({
            where: { course_id: courseId },
            include: [{
                model: ExamScore,
                as: 'scores',
                include: [{
                    model: User,
                    as: 'grader',
                    attributes: ['id', 'full_name'],
                }],
            }],
            order: [['exam_type', 'ASC'], ['component', 'ASC']],
        });

        res.json({
            success: true,
            data: {
                students,
                settings: settings.map(s => ({
                    id: s.id,
                    exam_type: s.exam_type,
                    component: s.component,
                    max_score: s.max_score,
                    is_visible: s.is_visible,
                    is_active: s.is_active,
                    scores: s.scores.map(score => ({
                        id: score.id,
                        student_id: score.student_id,
                        score: score.score,
                        grader_id: score.graded_by,
                        grader_name: score.grader?.full_name || null,
                        graded_at: score.graded_at,
                    })),
                })),
            },
        });
    } catch (error) {
        next(error);
    }
};

/**
 * Get exam score statistics
 * GET /api/courses/:courseId/exam-scores/stats
 */
exports.getExamScoreStats = async (req, res, next) => {
    try {
        const { courseId } = req.params;

        const settings = await ExamSetting.findAll({
            where: { course_id: courseId, is_active: true },
            include: [{
                model: ExamScore,
                as: 'scores',
                where: { score: { [Op.ne]: null } },
                required: false,
            }],
        });

        const stats = settings.map(setting => {
            const scores = setting.scores.map(s => parseFloat(s.score));
            const count = scores.length;
            const avg = count > 0 ? scores.reduce((a, b) => a + b, 0) / count : 0;
            const max = count > 0 ? Math.max(...scores) : 0;
            const min = count > 0 ? Math.min(...scores) : 0;

            return {
                id: setting.id,
                exam_type: setting.exam_type,
                component: setting.component,
                max_score: setting.max_score,
                stats: {
                    count,
                    average: Math.round(avg * 100) / 100,
                    max,
                    min,
                },
            };
        });

        res.json({
            success: true,
            data: stats,
        });
    } catch (error) {
        next(error);
    }
};

/**
 * Save single exam score
 * POST /api/courses/:courseId/exam-scores
 */
exports.saveExamScore = async (req, res, next) => {
    try {
        const { courseId } = req.params;
        const { exam_setting_id, student_id, score } = req.body;
        const graderId = req.user.id;

        // Validate setting
        const setting = await ExamSetting.findOne({
            where: { id: exam_setting_id, course_id: courseId },
        });

        if (!setting) {
            return res.status(404).json({
                success: false,
                message: 'ไม่พบการตั้งค่าการสอบ',
            });
        }

        // Validate score
        if (score !== null && score !== undefined) {
            if (score < 0) {
                return res.status(400).json({
                    success: false,
                    message: 'คะแนนต้องไม่ติดลบ',
                });
            }
            if (score > setting.max_score) {
                return res.status(400).json({
                    success: false,
                    message: `คะแนนเกินคะแนนเต็ม (${setting.max_score})`,
                });
            }
        }

        // Find or create score
        let examScore = await ExamScore.findOne({
            where: { exam_setting_id, student_id },
        });

        if (examScore) {
            examScore.score = score;
            examScore.graded_by = graderId;
            examScore.graded_at = new Date();
            await examScore.save();
        } else {
            examScore = await ExamScore.create({
                exam_setting_id,
                student_id,
                score,
                graded_by: graderId,
                graded_at: new Date(),
            });
        }

        // Get grader info
        const grader = await User.findByPk(graderId, { attributes: ['id', 'full_name'] });

        res.json({
            success: true,
            data: {
                id: examScore.id,
                exam_setting_id: examScore.exam_setting_id,
                student_id: examScore.student_id,
                score: examScore.score,
                grader_id: examScore.graded_by,
                grader_name: grader?.full_name || null,
                graded_at: examScore.graded_at,
            },
            message: 'บันทึกคะแนนสำเร็จ',
        });
    } catch (error) {
        next(error);
    }
};

/**
 * Bulk save exam scores
 * POST /api/courses/:courseId/exam-scores/bulk
 */
exports.bulkSaveExamScores = async (req, res, next) => {
    try {
        const { courseId } = req.params;
        const { exam_setting_id, scores } = req.body;
        const graderId = req.user.id;

        // Validate setting
        const setting = await ExamSetting.findOne({
            where: { id: exam_setting_id, course_id: courseId },
        });

        if (!setting) {
            return res.status(404).json({
                success: false,
                message: 'ไม่พบการตั้งค่าการสอบ',
            });
        }

        // Get all students
        const sections = await CourseSection.findAll({
            where: { course_id: courseId },
            include: [{
                model: Student,
                as: 'students',
                through: { attributes: [] },
                attributes: ['id', 'student_id'],
            }],
        });

        const studentMap = new Map();
        sections.forEach(section => {
            section.students.forEach(student => {
                studentMap.set(student.student_id.toLowerCase(), student.id);
            });
        });

        const errors = [];
        let savedCount = 0;

        for (const item of scores) {
            const studentDbId = studentMap.get(item.student_id.toLowerCase());
            
            if (!studentDbId) {
                errors.push({ student_id: item.student_id, reason: 'ไม่พบนักศึกษา' });
                continue;
            }

            // Validate score
            if (item.score !== null && item.score !== undefined) {
                if (item.score < 0) {
                    errors.push({ student_id: item.student_id, reason: 'คะแนนต้องไม่ติดลบ' });
                    continue;
                }
                if (item.score > setting.max_score) {
                    errors.push({ student_id: item.student_id, reason: `คะแนนเกินคะแนนเต็ม (${setting.max_score})` });
                    continue;
                }
            }

            // Find or create score
            let examScore = await ExamScore.findOne({
                where: { exam_setting_id, student_id: studentDbId },
            });

            if (examScore) {
                examScore.score = item.score;
                examScore.graded_by = graderId;
                examScore.graded_at = new Date();
                await examScore.save();
            } else {
                await ExamScore.create({
                    exam_setting_id,
                    student_id: studentDbId,
                    score: item.score,
                    graded_by: graderId,
                    graded_at: new Date(),
                });
            }
            savedCount++;
        }

        res.json({
            success: true,
            data: {
                saved: savedCount,
                errors,
            },
            message: `บันทึกคะแนน ${savedCount} รายการสำเร็จ`,
        });
    } catch (error) {
        next(error);
    }
};

/**
 * Delete exam score
 * DELETE /api/courses/:courseId/exam-scores/:scoreId
 */
exports.deleteExamScore = async (req, res, next) => {
    try {
        const { courseId, scoreId } = req.params;

        const score = await ExamScore.findOne({
            where: { id: scoreId },
            include: [{
                model: ExamSetting,
                as: 'examSetting',
                where: { course_id: courseId },
            }],
        });

        if (!score) {
            return res.status(404).json({
                success: false,
                message: 'ไม่พบคะแนน',
            });
        }

        await score.destroy();

        res.json({
            success: true,
            message: 'ลบคะแนนสำเร็จ',
        });
    } catch (error) {
        next(error);
    }
};
