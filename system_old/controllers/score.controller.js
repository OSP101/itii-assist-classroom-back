const { Score, Assignment, AssignmentSubItem, Student, User, StudentGroup, StudentGroupMember, CourseSectionStudent, CourseSection, ScoreEditRequest, AttendanceSession, AttendanceRecord, BonusScore, AssignmentAttendanceLink, Course, sequelize } = require('../models');
const { Op } = require('sequelize');
const asyncHandler = require('../utils/asyncHandler');
const ApiError = require('../utils/ApiError');
const { logCourseActivity } = require('../utils/courseActivityLogger');
const logger = require('../utils/logger');

/**
 * Helper: check if course is active, throw 403 if not
 */
const ensureCourseActive = async (courseId) => {
    const course = await Course.findByPk(courseId, { attributes: ['id', 'is_active'] });
    if (course && !course.is_active) {
        throw new ApiError(403, 'รายวิชานี้ปิดใช้งานอยู่ กรุณาเปิดใช้งานก่อนทำการแก้ไข');
    }
};

/**
 * Check if student attended the linked attendance session(s)
 * Supports multiple attendance sessions with AND/OR conditions
 * Returns: { attended: boolean, status: string | null, message: string, details: Array }
 */
const checkStudentAttendance = async (assignmentId, studentId) => {
    const assignment = await Assignment.findByPk(assignmentId, {
        include: [{
            model: AttendanceSession,
            as: 'linkedAttendanceSessions',
            attributes: ['id', 'title', 'start_time', 'end_time', 'session_type', 'course_section_id'],
            through: { attributes: [] },
        }],
    });
    
    if (!assignment) {
        return { attended: true, status: null, message: 'ไม่พบงาน', details: [] };
    }

    const linkedSessions = assignment.linkedAttendanceSessions || [];
    
    // Fallback: Check legacy single linked session if no many-to-many links exist
    if (linkedSessions.length === 0) {
        if (!assignment.linked_attendance_session_id) {
            return { attended: true, status: null, message: 'ไม่มีการลิงก์กับการเช็คชื่อ', details: [] };
        }
        
        // Legacy single session check
        const record = await AttendanceRecord.findOne({
            where: {
                attendance_session_id: assignment.linked_attendance_session_id,
                student_id: studentId,
            },
        });
        
        if (!record) {
            return { attended: false, status: 'absent', message: 'นักศึกษาไม่มีข้อมูลการเช็คชื่อ', details: [] };
        }
        
        if (record.status === 'absent') {
            return { attended: false, status: 'absent', message: 'นักศึกษาขาดเรียน ไม่อนุญาตให้ลงคะแนน', details: [] };
        }
        
        return { attended: true, status: record.status, message: 'นักศึกษามาเรียน', details: [] };
    }
    
    // Multi-session check
    const sessionIds = linkedSessions.map(s => s.id);
    const attendanceRecords = await AttendanceRecord.findAll({
        where: {
            attendance_session_id: { [Op.in]: sessionIds },
            student_id: studentId,
        },
    });
    
    // Build status for each linked session
    const recordMap = {};
    attendanceRecords.forEach(record => {
        recordMap[record.attendance_session_id] = record.status;
    });
    
    const details = linkedSessions.map(session => {
        const status = recordMap[session.id] || 'absent';
        const attended = status !== 'absent';
        return {
            session_id: session.id,
            session_title: session.title,
            start_time: session.start_time,
            end_time: session.end_time,
            status: status,
            attended: attended,
        };
    });
    
    const attendedCount = details.filter(d => d.attended).length;
    const totalCount = details.length;
    const condition = assignment.attendance_condition || 'or';
    
    let attended, message;
    
    if (condition === 'and') {
        // Must attend ALL linked sessions
        attended = attendedCount === totalCount;
        if (attended) {
            message = `นักศึกษามาเรียนครบทุกรอบ (${attendedCount}/${totalCount})`;
        } else {
            message = `นักศึกษาต้องมาเรียนครบทุกรอบ (มา ${attendedCount}/${totalCount}) ไม่อนุญาตให้ลงคะแนน`;
        }
    } else {
        // Must attend at least ONE linked session
        attended = attendedCount > 0;
        if (attended) {
            message = `นักศึกษามาเรียนอย่างน้อย 1 รอบ (${attendedCount}/${totalCount})`;
        } else {
            message = `นักศึกษาต้องมาเรียนอย่างน้อย 1 รอบ (มา 0/${totalCount}) ไม่อนุญาตให้ลงคะแนน`;
        }
    }
    
    return { 
        attended, 
        status: attended ? (attendedCount === totalCount ? 'present' : 'partial') : 'absent', 
        message, 
        details,
        condition,
        attended_count: attendedCount,
        total_count: totalCount,
    };
};

/**
 * Get scores for an assignment
 */
const getScores = asyncHandler(async (req, res) => {
    const { assignment_id, course_id } = req.query;

    if (!assignment_id) {
        throw new ApiError(400, 'assignment_id is required');
    }

    // ✅ OPTIMIZED: Only select needed attributes
    const assignment = await Assignment.findByPk(assignment_id, {
        attributes: ['id', 'course_id', 'name', 'max_score', 'assignment_type', 'linked_attendance_session_id', 'attendance_condition'],
        include: [
            {
                model: AssignmentSubItem,
                as: 'subItems',
                attributes: ['id', 'name', 'max_score', 'order_index'],
            },
            {
                model: AttendanceSession,
                as: 'linkedAttendanceSession',
                attributes: ['id', 'title', 'start_time', 'end_time'],
            },
            {
                model: AttendanceSession,
                as: 'linkedAttendanceSessions',
                attributes: ['id', 'title', 'start_time', 'end_time', 'session_type', 'course_section_id'],
                through: { attributes: [] },
            },
        ],
    });

    if (!assignment) {
        throw new ApiError(404, 'Assignment not found');
    }

    // ✅ OPTIMIZED: Get all students in single query with raw SQL for better performance
    const [studentsResult] = await sequelize.query(`
        SELECT DISTINCT s.id, s.student_id, s.full_name, s.email
        FROM students s
        INNER JOIN course_section_students css ON s.id = css.student_id
        INNER JOIN course_sections cs ON css.course_section_id = cs.id
        WHERE cs.course_id = ?
        ORDER BY s.student_id
    `, {
        replacements: [assignment.course_id],
    });
    
    const uniqueStudents = studentsResult;

    // ✅ OPTIMIZED: Get existing scores with minimal attributes
    const scores = await Score.findAll({
        where: { assignment_id },
        attributes: ['id', 'student_id', 'sub_item_id', 'score', 'comment', 'status', 'graded_by', 'graded_at'],
        include: [
            {
                model: User,
                as: 'grader',
                attributes: ['id', 'full_name'],
            },
        ],
    });

    // Build score map by student_id (for main scores - sub_item_id is null)
    const scoreMap = {};
    // Build sub-item score map by student_id -> sub_item_id
    const subItemScoreMap = {};
    
    scores.forEach(score => {
        if (score.sub_item_id) {
            // This is a sub-item score
            if (!subItemScoreMap[score.student_id]) {
                subItemScoreMap[score.student_id] = {};
            }
            subItemScoreMap[score.student_id][score.sub_item_id] = score;
        } else {
            // This is a main score
            scoreMap[score.student_id] = score;
        }
    });

    // Get attendance records for linked sessions (both legacy single and multi-session)
    const linkedSessions = assignment.linkedAttendanceSessions || [];
    const hasMultiSessions = linkedSessions.length > 0;
    const condition = assignment.attendance_condition || 'or';
    
    let attendanceDataMap = {}; // student_id -> { status, canScore, details }
    
    if (hasMultiSessions) {
        // Multi-session attendance check
        const sessionIds = linkedSessions.map(s => s.id);
        const attendanceRecords = await AttendanceRecord.findAll({
            where: { attendance_session_id: { [Op.in]: sessionIds } },
            attributes: ['student_id', 'attendance_session_id', 'status'],
        });
        
        // Build record map: student_id -> session_id -> status
        const recordsByStudent = {};
        attendanceRecords.forEach(record => {
            if (!recordsByStudent[record.student_id]) {
                recordsByStudent[record.student_id] = {};
            }
            recordsByStudent[record.student_id][record.attendance_session_id] = record.status;
        });
        
        // Calculate attendance status for each student
        uniqueStudents.forEach(student => {
            const studentRecords = recordsByStudent[student.id] || {};
            const details = linkedSessions.map(session => {
                const status = studentRecords[session.id] || 'absent';
                return {
                    session_id: session.id,
                    session_title: session.title,
                    start_time: session.start_time,
                    status: status,
                    attended: status !== 'absent',
                };
            });
            
            const attendedCount = details.filter(d => d.attended).length;
            const totalCount = details.length;
            
            let canScore, overallStatus;
            if (condition === 'and') {
                canScore = attendedCount === totalCount;
                overallStatus = canScore ? 'present' : 'absent';
            } else {
                canScore = attendedCount > 0;
                overallStatus = canScore ? (attendedCount === totalCount ? 'present' : 'partial') : 'absent';
            }
            
            attendanceDataMap[student.id] = {
                status: overallStatus,
                canScore: canScore,
                details: details,
                attended_count: attendedCount,
                total_count: totalCount,
            };
        });
    } else if (assignment.linked_attendance_session_id) {
        // Legacy single session check
        const attendanceRecords = await AttendanceRecord.findAll({
            where: { attendance_session_id: assignment.linked_attendance_session_id },
            attributes: ['student_id', 'status'],
        });
        attendanceRecords.forEach(record => {
            attendanceDataMap[record.student_id] = {
                status: record.status,
                canScore: record.status !== 'absent',
                details: [],
            };
        });
        // Fill in missing students as absent
        uniqueStudents.forEach(student => {
            if (!attendanceDataMap[student.id]) {
                attendanceDataMap[student.id] = {
                    status: 'absent',
                    canScore: false,
                    details: [],
                };
            }
        });
    }

    // Build response with all students
    const studentScores = uniqueStudents.map(student => {
        const existingScore = scoreMap[student.id];
        const studentSubItemScores = subItemScoreMap[student.id] || {};
        
        // Check attendance status
        const attendanceData = attendanceDataMap[student.id];
        const hasAttendanceLink = hasMultiSessions || assignment.linked_attendance_session_id;
        const attendanceStatus = hasAttendanceLink ? (attendanceData?.status || 'absent') : null;
        const canScore = !hasAttendanceLink || (attendanceData?.canScore ?? true);
        
        // Build sub-item scores array
        const subItemScores = assignment.subItems ? assignment.subItems.map(subItem => {
            const subScore = studentSubItemScores[subItem.id];
            return {
                sub_item_id: subItem.id,
                sub_item_name: subItem.name,
                max_score: subItem.max_score,
                score: subScore ? parseFloat(subScore.score) : null,
                score_id: subScore ? subScore.id : null,
                graded_by: subScore && subScore.grader ? {
                    id: subScore.grader.id,
                    display_name: subScore.grader.full_name,
                } : null,
                graded_at: subScore ? subScore.graded_at : null,
            };
        }) : [];
        
        return {
            student,
            score: existingScore ? existingScore.score : null,
            score_id: existingScore ? existingScore.id : null,
            max_score: assignment.max_score,
            comment: existingScore ? existingScore.comment : null,
            status: existingScore ? existingScore.status : 'pending',
            graded_by: existingScore && existingScore.grader ? {
                id: existingScore.grader.id,
                display_name: existingScore.grader.full_name,
            } : null,
            graded_at: existingScore ? existingScore.graded_at : null,
            sub_item_scores: subItemScores,
            // Attendance info
            attendance_status: attendanceStatus,
            can_score: canScore,
            attendance_details: attendanceData?.details || [],
            attendance_condition: hasMultiSessions ? condition : null,
            attended_count: attendanceData?.attended_count,
            total_count: attendanceData?.total_count,
        };
    });

    res.json({
        success: true,
        data: {
            assignment: {
                ...assignment.toJSON(),
                attendance_condition: assignment.attendance_condition,
                linked_sessions_count: linkedSessions.length,
            },
            student_scores: studentScores,
        },
    });
});

/**
 * Submit/Update a single score (with optional sub_item_id)
 */
const submitScore = asyncHandler(async (req, res) => {
    const { assignment_id, student_id, score, comment, sub_item_id } = req.body;

    if (!assignment_id || !student_id || score === undefined) {
        throw new ApiError(400, 'assignment_id, student_id and score are required');
    }

    const assignment = await Assignment.findByPk(assignment_id, {
        include: [{
            model: AssignmentSubItem,
            as: 'subItems',
        }],
    });
    if (!assignment) {
        throw new ApiError(404, 'Assignment not found');
    }

    // Check if course is active
    await ensureCourseActive(assignment.course_id);

    // Check attendance if assignment is linked to attendance session
    const attendanceCheck = await checkStudentAttendance(assignment_id, student_id);
    if (!attendanceCheck.attended) {
        throw new ApiError(400, attendanceCheck.message);
    }

    // Validate score against max
    let maxScore = parseFloat(assignment.max_score);
    
    // If sub_item_id is provided, validate against sub-item max
    if (sub_item_id) {
        const subItem = assignment.subItems?.find(si => si.id === sub_item_id);
        if (!subItem) {
            throw new ApiError(404, 'Sub-item not found');
        }
        maxScore = parseFloat(subItem.max_score);
    }

    if (score < 0 || score > maxScore) {
        throw new ApiError(400, `Score must be between 0 and ${maxScore}`);
    }

    // Build where clause
    const whereClause = {
        assignment_id,
        student_id,
    };
    
    // Handle sub_item_id (null vs specific value)
    if (sub_item_id) {
        whereClause.sub_item_id = sub_item_id;
    } else {
        whereClause.sub_item_id = null;
    }

    // Find existing score or create new
    const [scoreRecord, created] = await Score.findOrCreate({
        where: whereClause,
        defaults: {
            score,
            comment,
            sub_item_id: sub_item_id || null,
            graded_by: req.user.id,
            graded_at: new Date(),
            status: 'graded',
        },
    });

    if (!created) {
        // Update existing score
        await scoreRecord.update({
            score,
            comment: comment !== undefined ? comment : scoreRecord.comment,
            graded_by: req.user.id,
            graded_at: new Date(),
            status: 'graded',
        });
    }

    logCourseActivity({ courseId: assignment.course_id, actorUserId: req.user.id, action: 'submit_score', category: 'score', targetType: 'score', targetId: scoreRecord.id, targetName: assignment.name, detail: { student_id, score, sub_item_id: sub_item_id || null, created } });

    res.json({
        success: true,
        data: scoreRecord,
        message: created ? 'Score submitted successfully' : 'Score updated successfully',
    });
});

/**
 * Submit bulk scores
 */
const submitBulkScores = asyncHandler(async (req, res) => {
    const { assignment_id, scores } = req.body;

    if (!assignment_id || !scores || !Array.isArray(scores)) {
        throw new ApiError(400, 'assignment_id and scores array are required');
    }

    const assignment = await Assignment.findByPk(assignment_id);
    if (!assignment) {
        throw new ApiError(404, 'Assignment not found');
    }

    // Check if course is active
    await ensureCourseActive(assignment.course_id);

    const transaction = await sequelize.transaction();
    const results = { created: 0, updated: 0 };

    try {
        for (const item of scores) {
            const { student_id, score, comment } = item;

            if (student_id === undefined || score === undefined) continue;

            const [scoreRecord, created] = await Score.findOrCreate({
                where: {
                    assignment_id,
                    student_id,
                },
                defaults: {
                    score,
                    comment,
                    graded_by: req.user.id,
                    graded_at: new Date(),
                    status: 'graded',
                },
                transaction,
            });

            if (!created) {
                await scoreRecord.update({
                    score,
                    comment: comment !== undefined ? comment : scoreRecord.comment,
                    graded_by: req.user.id,
                    graded_at: new Date(),
                    status: 'graded',
                }, { transaction });
                results.updated++;
            } else {
                results.created++;
            }
        }

        await transaction.commit();

        logCourseActivity({ courseId: assignment.course_id, actorUserId: req.user.id, action: 'submit_bulk_scores', category: 'score', targetType: 'assignment', targetId: assignment_id, targetName: assignment.name, detail: { created: results.created, updated: results.updated } });

        res.json({
            success: true,
            message: `${results.created} scores created, ${results.updated} scores updated`,
        });
    } catch (error) {
        await transaction.rollback();
        throw error;
    }
});

/**
 * Submit group score (applies to all members or selected members, with optional sub_item_id)
 */
const submitGroupScore = asyncHandler(async (req, res) => {
    const { assignment_id, group_id, score, comment, sub_item_id, student_ids } = req.body;

    if (!assignment_id || !group_id || score === undefined) {
        throw new ApiError(400, 'assignment_id, group_id and score are required');
    }

    // Validate assignment and sub-item if provided
    const assignment = await Assignment.findByPk(assignment_id, {
        include: [{
            model: AssignmentSubItem,
            as: 'subItems',
        }],
    });
    
    if (!assignment) {
        throw new ApiError(404, 'Assignment not found');
    }

    // Check if course is active
    await ensureCourseActive(assignment.course_id);

    let maxScore = parseFloat(assignment.max_score);
    if (sub_item_id) {
        const subItem = assignment.subItems?.find(si => si.id === sub_item_id);
        if (!subItem) {
            throw new ApiError(404, 'Sub-item not found');
        }
        maxScore = parseFloat(subItem.max_score);
    }

    if (score < 0 || score > maxScore) {
        throw new ApiError(400, `Score must be between 0 and ${maxScore}`);
    }

    // Get group members
    const groupMembers = await StudentGroupMember.findAll({
        where: { group_id },
        attributes: ['student_id'],
    });

    if (groupMembers.length === 0) {
        throw new ApiError(404, 'No members found in this group');
    }

    // Filter members if student_ids is provided (for selective grading)
    let targetMembers = groupMembers;
    if (student_ids && Array.isArray(student_ids) && student_ids.length > 0) {
        // Validate that all provided student_ids are members of the group
        const memberIds = groupMembers.map(m => m.student_id);
        const invalidIds = student_ids.filter(id => !memberIds.includes(id));
        if (invalidIds.length > 0) {
            throw new ApiError(400, `Students ${invalidIds.join(', ')} are not members of this group`);
        }
        targetMembers = groupMembers.filter(m => student_ids.includes(m.student_id));
    }

    const transaction = await sequelize.transaction();

    try {
        for (const member of targetMembers) {
            // Build where clause for finding existing score
            const whereClause = {
                assignment_id,
                student_id: member.student_id,
            };
            if (sub_item_id) {
                whereClause.sub_item_id = sub_item_id;
            } else {
                whereClause.sub_item_id = null;
            }

            const [scoreRecord, created] = await Score.findOrCreate({
                where: whereClause,
                defaults: {
                    assignment_id,
                    student_id: member.student_id,
                    group_id,
                    sub_item_id: sub_item_id || null,
                    score,
                    comment,
                    graded_by: req.user.id,
                    graded_at: new Date(),
                    status: 'graded',
                },
                transaction,
            });

            if (!created) {
                await scoreRecord.update({
                    score,
                    comment: comment !== undefined ? comment : scoreRecord.comment,
                    graded_by: req.user.id,
                    graded_at: new Date(),
                    status: 'graded',
                }, { transaction });
            }
        }

        await transaction.commit();

        logCourseActivity({ courseId: assignment.course_id, actorUserId: req.user.id, action: 'submit_group_score', category: 'score', targetType: 'assignment', targetId: assignment_id, targetName: assignment.name, detail: { group_id, score, members: targetMembers.length, sub_item_id: sub_item_id || null } });

        res.json({
            success: true,
            message: `Score submitted for ${targetMembers.length} group members`,
        });
    } catch (error) {
        await transaction.rollback();
        throw error;
    }
});

/**
 * Request score edit (for TA)
 */
const requestScoreEdit = asyncHandler(async (req, res) => {
    const { score_id, new_score, reason } = req.body;

    if (!score_id || new_score === undefined || !reason) {
        throw new ApiError(400, 'score_id, new_score and reason are required');
    }

    const existingScore = await Score.findByPk(score_id);
    if (!existingScore) {
        throw new ApiError(404, 'Score not found');
    }

    // Check if course is active (via score -> assignment)
    const scoreAssignment = await Assignment.findByPk(existingScore.assignment_id, { attributes: ['id', 'course_id'] });
    if (scoreAssignment) {
        await ensureCourseActive(scoreAssignment.course_id);
    }

    // Check if there's already a pending request for this student in this assignment
    const pendingRequest = await ScoreEditRequest.findOne({
        where: {
            status: 'pending',
        },
        include: [
            {
                model: Score,
                as: 'score',
                required: true,
                where: {
                    assignment_id: existingScore.assignment_id,
                    student_id: existingScore.student_id,
                },
                include: [
                    {
                        model: Student,
                        as: 'student',
                        attributes: ['id', 'full_name'],
                    },
                ],
            },
            {
                model: User,
                as: 'requester',
                attributes: ['id', 'full_name'],
            },
        ],
    });

    if (pendingRequest) {
        const studentName = pendingRequest.score?.student?.full_name || `student_id:${existingScore.student_id}`;
        const requesterName = pendingRequest.requester?.full_name || `user_id:${pendingRequest.requested_by}`;
        throw new ApiError(400, `นักศึกษา ${studentName} มีคำร้องแก้ไขคะแนนที่รออนุมัติอยู่แล้ว (ส่งโดย ${requesterName})`);
    }

    const editRequest = await ScoreEditRequest.create({
        score_id,
        old_score: existingScore.score,
        new_score,
        reason,
        requested_by: req.user.id,
    });

    // Log to course activity (need to find courseId via score -> assignment)
    const relAssignment = await Assignment.findByPk(existingScore.assignment_id, { attributes: ['id', 'name', 'course_id'] });
    if (relAssignment) {
      logCourseActivity({ courseId: relAssignment.course_id, actorUserId: req.user.id, action: 'request_score_edit', category: 'score', targetType: 'score_edit_request', targetId: editRequest.id, targetName: relAssignment.name, detail: { score_id, old_score: existingScore.score, new_score, reason } });
    }

    res.status(201).json({
        success: true,
        data: editRequest,
        message: 'Score edit request submitted for approval',
    });
});

/**
 * Get pending edit requests (for Instructor)
 */
const getPendingEditRequests = asyncHandler(async (req, res) => {
    const { course_id } = req.query;

    const whereClause = { status: 'pending' };

    const requests = await ScoreEditRequest.findAll({
        where: whereClause,
        include: [
            {
                model: Score,
                as: 'score',
                include: [
                    {
                        model: Assignment,
                        as: 'assignment',
                        where: course_id ? { course_id } : {},
                    },
                    {
                        model: Student,
                        as: 'student',
                        attributes: ['id', 'student_id', 'full_name'],
                    },
                ],
            },
            {
                model: User,
                as: 'requester',
                attributes: ['id', 'full_name'],
            },
        ],
        order: [['created_at', 'DESC']],
    });

    // Transform to include display_name
    const transformedRequests = requests.map(req => {
        const data = req.toJSON();
        if (data.requester) {
            data.requester = {
                id: data.requester.id,
                display_name: data.requester.full_name,
            };
        }
        return data;
    });

    res.json({
        success: true,
        data: transformedRequests,
    });
});

/**
 * Review edit request (approve/reject)
 */
const reviewEditRequest = asyncHandler(async (req, res) => {
    const { id } = req.params;
    const { status, review_comment } = req.body;

    if (!status || !['approved', 'rejected'].includes(status)) {
        throw new ApiError(400, 'status must be either "approved" or "rejected"');
    }

    const editRequest = await ScoreEditRequest.findByPk(id, {
        include: [{ model: Score, as: 'score' }],
    });

    if (!editRequest) {
        throw new ApiError(404, 'Edit request not found');
    }

    if (editRequest.status !== 'pending') {
        throw new ApiError(400, 'This request has already been reviewed');
    }

    const transaction = await sequelize.transaction();

    try {
        // Update request status
        await editRequest.update({
            status,
            reviewed_by: req.user.id,
            reviewed_at: new Date(),
            review_comment,
        }, { transaction });

        // If approved, update the actual score
        if (status === 'approved') {
            await editRequest.score.update({
                score: editRequest.new_score,
                graded_by: req.user.id,
                graded_at: new Date(),
            }, { transaction });
        }

        await transaction.commit();

        // Log to course activity (need courseId via score -> assignment)
        if (editRequest.score?.assignment_id) {
          const relAssignment2 = await Assignment.findByPk(editRequest.score.assignment_id, { attributes: ['id', 'name', 'course_id'] });
          if (relAssignment2) {
            logCourseActivity({ courseId: relAssignment2.course_id, actorUserId: req.user.id, action: status === 'approved' ? 'approve_score_edit' : 'reject_score_edit', category: 'score', targetType: 'score_edit_request', targetId: id, targetName: relAssignment2.name, detail: { status, review_comment } });
          }
        }

        res.json({
            success: true,
            message: status === 'approved' 
                ? 'Score edit approved and applied' 
                : 'Score edit request rejected',
        });
    } catch (error) {
        await transaction.rollback();
        throw error;
    }
});

/**
 * Get student scores summary for a course
 */
const getStudentScoresSummary = asyncHandler(async (req, res) => {
    const { course_id, student_id } = req.query;

    if (!course_id) {
        throw new ApiError(400, 'course_id is required');
    }

    const whereClause = { course_id, is_active: true };
    
    const assignments = await Assignment.findAll({
        where: whereClause,
        include: [
            {
                model: AssignmentSubItem,
                as: 'subItems',
            },
            {
                model: Score,
                as: 'scores',
                where: student_id ? { student_id } : {},
                required: false,
                include: [
                    {
                        model: Student,
                        as: 'student',
                        attributes: ['id', 'student_id', 'full_name'],
                    },
                ],
            },
        ],
        order: [['order_index', 'ASC']],
    });

    res.json({
        success: true,
        data: assignments,
    });
});

/**
 * Get Score Summary Matrix - All students x All assignments
 */
const getScoreSummaryMatrix = asyncHandler(async (req, res) => {
    const { course_id, section_id, assignment_type } = req.query;

    if (!course_id) {
        throw new ApiError(400, 'course_id is required');
    }

    // Get all sections for this course
    const sections = await CourseSection.findAll({
        where: { course_id },
        order: [['section_no', 'ASC']],
    });

    // Build where clause for students by section
    let sectionFilter = {};
    if (section_id) {
        sectionFilter = { id: section_id };
    } else {
        sectionFilter = { course_id };
    }

    // Get all students in the course (or filtered by section)
    const courseSections = await CourseSection.findAll({
        where: sectionFilter,
        include: [
            {
                model: Student,
                as: 'students',
                through: { attributes: [] },
                attributes: ['id', 'student_id', 'full_name'],
            },
        ],
        order: [['section_no', 'ASC']],
    });

    // Flatten students and add section info
    const studentsWithSection = [];
    const studentSectionMap = {};
    
    for (const section of courseSections) {
        for (const student of section.students) {
            if (!studentSectionMap[student.id]) {
                studentSectionMap[student.id] = section.section_no;
                studentsWithSection.push({
                    id: student.id,
                    student_id: student.student_id,
                    full_name: student.full_name,
                    section_number: section.section_no,
                });
            }
        }
    }

    // Sort students by section, then student_id
    studentsWithSection.sort((a, b) => {
        if (a.section_number !== b.section_number) {
            return a.section_number - b.section_number;
        }
        return a.student_id.localeCompare(b.student_id);
    });

    // Build assignment type filter
    let assignmentTypeFilter = {};
    logger.debug(`[getScoreSummaryMatrix] assignment_type param: "${assignment_type}"`);
    
    if (assignment_type === 'individual') {
        // Lab assignments (individual work done in class)
        assignmentTypeFilter = { assignment_type: 'individual' };
    } else if (assignment_type === 'assignment') {
        // Assignment/homework (individual work done at home)
        assignmentTypeFilter = { assignment_type: 'assignment' };
    } else if (assignment_type === 'group') {
        assignmentTypeFilter = { 
            assignment_type: { [Op.in]: ['permanent_group', 'weekly_group'] } 
        };
    } else if (assignment_type === 'permanent_group') {
        assignmentTypeFilter = { assignment_type: 'permanent_group' };
    } else if (assignment_type === 'weekly_group') {
        assignmentTypeFilter = { assignment_type: 'weekly_group' };
    } else if (assignment_type === 'permanent_group') {
        assignmentTypeFilter = { assignment_type: 'permanent_group' };
    } else if (assignment_type === 'weekly_group') {
        assignmentTypeFilter = { assignment_type: 'weekly_group' };
    }
    
    logger.debug(`[getScoreSummaryMatrix] assignmentTypeFilter:`, JSON.stringify(assignmentTypeFilter));

    // Get all assignments for this course
    const assignments = await Assignment.findAll({
        where: { 
            course_id, 
            is_active: true,
            ...assignmentTypeFilter,
        },
        include: [
            {
                model: AssignmentSubItem,
                as: 'subItems',
                order: [['order_index', 'ASC']],
            },
        ],
        order: [['order_index', 'ASC']],
    });

    logger.debug(`[getScoreSummaryMatrix] Found ${assignments.length} assignments with filter`);

    // Get all scores for these students and assignments
    const studentIds = studentsWithSection.map(s => s.id);
    const assignmentIds = assignments.map(a => a.id);

    // For permanent group assignments: fetch group membership per student (one group per student).
    const studentGroupMap = {}; // studentDbId → { group_id, group_name }
    if ((assignment_type === 'group' || assignment_type === 'permanent_group') && studentIds.length > 0) {
        const groupMembers = await StudentGroupMember.findAll({
            where: { student_id: { [Op.in]: studentIds } },
            include: [{
                model: StudentGroup,
                as: 'group',
                where: { course_id, group_type: 'permanent' },
                attributes: ['id', 'name'],
                required: true,
            }],
        });
        for (const gm of groupMembers) {
            studentGroupMap[gm.student_id] = {
                group_id: gm.group.id,
                group_name: gm.group.name,
            };
        }
    }

    // For weekly group: include StudentGroup in scores so we can return group_name per score.
    const scoreInclude = [
        { model: User, as: 'grader', attributes: ['id', 'full_name'] },
    ];
    if (assignment_type === 'weekly_group' || assignment_type === 'group') {
        scoreInclude.push({
            model: StudentGroup,
            as: 'group',
            attributes: ['id', 'name'],
            required: false,
        });
    }

    const scores = await Score.findAll({
        where: {
            student_id: { [Op.in]: studentIds },
            assignment_id: { [Op.in]: assignmentIds },
        },
        include: scoreInclude,
    });

    // Get bonus scores for all students in this course
    const bonusScoreRecords = await BonusScore.findAll({
        where: {
            course_id,
            student_id: { [Op.in]: studentIds },
        },
        attributes: ['student_id', 'score'],
    });

    // Create bonus score map: { student_id: totalBonusScore }
    const bonusScoreMap = {};
    for (const record of bonusScoreRecords) {
        if (!bonusScoreMap[record.student_id]) {
            bonusScoreMap[record.student_id] = 0;
        }
        bonusScoreMap[record.student_id] += parseFloat(record.score) || 0;
    }

    // Create score lookup map with full info: { `${student_id}_${assignment_id}_${sub_item_id}`: scoreObj }
    const scoreMap = {};
    const scoreIdToKeyMap = {}; // score.id -> scoreMap key
    for (const score of scores) {
        const key = `${score.student_id}_${score.assignment_id}_${score.sub_item_id || 'main'}`;
        scoreMap[key] = {
            score_id: score.id,
            score: parseFloat(score.score) || 0,
            graded_by: score.grader?.full_name || null,
            graded_at: score.graded_at || score.createdAt,
            updated_at: score.updatedAt,
            comment: score.comment || null,
            group_name: score.group?.name || null,
            edit_requests: [],
        };
        scoreIdToKeyMap[score.id] = key;
    }

    // Fetch approved score edit requests for all scores in this matrix
    const scoreIds = scores.map(s => s.id);
    if (scoreIds.length > 0) {
        const editRequests = await ScoreEditRequest.findAll({
            where: {
                score_id: { [Op.in]: scoreIds },
                status: 'approved',
            },
            include: [
                { model: User, as: 'requester', attributes: ['id', 'full_name'] },
                { model: User, as: 'reviewer', attributes: ['id', 'full_name'] },
            ],
            order: [['reviewed_at', 'DESC']],
        });
        for (const er of editRequests) {
            const mapKey = scoreIdToKeyMap[er.score_id];
            if (mapKey && scoreMap[mapKey]) {
                scoreMap[mapKey].edit_requests.push({
                    old_score: er.old_score !== null ? parseFloat(er.old_score) : null,
                    new_score: parseFloat(er.new_score),
                    reason: er.reason || null,
                    requester: er.requester?.full_name || null,
                    reviewer: er.reviewer?.full_name || null,
                    reviewed_at: er.reviewed_at || null,
                    review_comment: er.review_comment || null,
                });
            }
        }
    }

    // Build matrix data
    const matrixData = studentsWithSection.map(student => {
        const groupInfo = studentGroupMap[student.id] ?? null;
        const row = {
            student_id: student.student_id,
            full_name: student.full_name,
            section_number: student.section_number,
            bonus_score: bonusScoreMap[student.id] || 0,
            group_id: groupInfo?.group_id ?? null,
            group_name: groupInfo?.group_name ?? null,
            scores: {},
            total_score: 0,
            total_max_score: 0,
            scored_count: 0,
            total_items: 0,
        };

        for (const assignment of assignments) {
            const hasSubItems = assignment.subItems && assignment.subItems.length > 0;
            // Parse assignment max_score to number
            const assignmentMaxScore = parseFloat(assignment.max_score) || 0;

            if (hasSubItems) {
                // Process sub-items
                let assignmentTotal = 0;
                let assignmentMax = 0;
                let scoredSubItems = 0;

                for (const subItem of assignment.subItems) {
                    const key = `${student.id}_${assignment.id}_${subItem.id}`;
                    const scoreObj = scoreMap[key];
                    const subItemMaxScore = parseFloat(subItem.max_score) || 0;
                    
                    row.scores[`${assignment.id}_${subItem.id}`] = {
                        score: scoreObj?.score !== undefined ? scoreObj.score : null,
                        max_score: subItemMaxScore,
                        sub_item_name: subItem.name,
                        graded_by: scoreObj?.graded_by || null,
                        graded_at: scoreObj?.graded_at || null,
                        updated_at: scoreObj?.updated_at || null,
                        comment: scoreObj?.comment || null,
                        group_name: scoreObj?.group_name || null,
                        edit_requests: scoreObj?.edit_requests || [],
                    };

                    if (scoreObj?.score !== undefined) {
                        assignmentTotal += scoreObj.score;
                        scoredSubItems++;
                    }
                    assignmentMax += subItemMaxScore;
                    row.total_items++;
                }

                row.total_score += assignmentTotal;
                row.total_max_score += assignmentMax;
                row.scored_count += scoredSubItems;
            } else {
                // No sub-items, use main score
                const key = `${student.id}_${assignment.id}_main`;
                const scoreObj = scoreMap[key];

                row.scores[`${assignment.id}_main`] = {
                    score: scoreObj?.score !== undefined ? scoreObj.score : null,
                    max_score: assignmentMaxScore,
                    graded_by: scoreObj?.graded_by || null,
                    graded_at: scoreObj?.graded_at || null,
                    updated_at: scoreObj?.updated_at || null,
                    comment: scoreObj?.comment || null,
                    group_name: scoreObj?.group_name || null,
                    edit_requests: scoreObj?.edit_requests || [],
                };

                if (scoreObj?.score !== undefined) {
                    row.total_score += scoreObj.score;
                    row.scored_count++;
                }
                row.total_max_score += assignmentMaxScore;
                row.total_items++;
            }
        }

        return row;
    });

    // For permanent group assignments, re-sort by group_id then student_id so members are contiguous
    if (assignment_type === 'group' || assignment_type === 'permanent_group') {
        matrixData.sort((a, b) => {
            const gA = a.group_id ?? Infinity;
            const gB = b.group_id ?? Infinity;
            if (gA !== gB) return gA - gB;
            return a.student_id.localeCompare(b.student_id);
        });
    }

    // Calculate class averages per assignment
    const averages = {};
    for (const assignment of assignments) {
        const hasSubItems = assignment.subItems && assignment.subItems.length > 0;

        if (hasSubItems) {
            for (const subItem of assignment.subItems) {
                const key = `${assignment.id}_${subItem.id}`;
                const scores = matrixData
                    .map(row => row.scores[key]?.score)
                    .filter(s => s !== null && s !== undefined);
                
                averages[key] = scores.length > 0 
                    ? (scores.reduce((a, b) => a + b, 0) / scores.length).toFixed(2)
                    : null;
            }
        } else {
            const key = `${assignment.id}_main`;
            const scores = matrixData
                .map(row => row.scores[key]?.score)
                .filter(s => s !== null && s !== undefined);
            
            averages[key] = scores.length > 0 
                ? (scores.reduce((a, b) => a + b, 0) / scores.length).toFixed(2)
                : null;
        }
    }

    // Format assignments for response
    const formattedAssignments = assignments.map(a => ({
        id: a.id,
        title: a.name,
        short_title: a.name,
        max_score: parseFloat(a.max_score) || 0,
        assignment_type: a.assignment_type,
        subItems: a.subItems?.map(si => ({
            id: si.id,
            name: si.name,
            max_score: parseFloat(si.max_score) || 0,
        })) || [],
    }));

    res.json({
        success: true,
        data: {
            sections: sections.map(s => ({
                id: s.id,
                section_number: s.section_no,
            })),
            assignments: formattedAssignments,
            students: matrixData,
            averages,
            summary: {
                total_students: matrixData.length,
                total_assignments: assignments.length,
            },
        },
    });
});

/**
 * Search students for autocomplete
 */
const searchStudents = asyncHandler(async (req, res) => {
    const { course_id, query } = req.query;

    if (!course_id) {
        throw new ApiError(400, 'course_id is required');
    }

    // ✅ OPTIMIZED: Use raw SQL for better performance and reliability
    // Previous Sequelize include with where: {} had issues when some sections had no students
    let sql = `
        SELECT DISTINCT s.id, s.student_id, s.full_name, s.email
        FROM students s
        INNER JOIN course_section_students css ON s.id = css.student_id
        INNER JOIN course_sections cs ON css.course_section_id = cs.id
        WHERE cs.course_id = ?
    `;
    const replacements = [course_id];

    // Add search filter if query provided
    if (query && query.trim()) {
        sql += ` AND (s.student_id LIKE ? OR s.full_name LIKE ?)`;
        const searchPattern = `%${query.trim()}%`;
        replacements.push(searchPattern, searchPattern);
    }

    sql += ` ORDER BY s.student_id`;

    const [students] = await sequelize.query(sql, { replacements });

    res.json({
        success: true,
        data: students,
        total: students.length,
    });
});

/**
 * Get groups for assignment scoring
 */
const getGroupsForAssignment = asyncHandler(async (req, res) => {
    const { assignment_id } = req.query;

    if (!assignment_id) {
        throw new ApiError(400, 'assignment_id is required');
    }

    const assignment = await Assignment.findByPk(assignment_id);
    if (!assignment) {
        throw new ApiError(404, 'Assignment not found');
    }

    let groups;
    if (assignment.assignment_type === 'permanent_group') {
        groups = await StudentGroup.findAll({
            where: {
                course_id: assignment.course_id,
                group_type: 'permanent',
            },
            include: [
                {
                    model: Student,
                    as: 'members',
                    through: { attributes: [] },
                },
            ],
        });
    } else if (assignment.assignment_type === 'weekly_group') {
        groups = await StudentGroup.findAll({
            where: {
                course_id: assignment.course_id,
                group_type: 'temporary',
                week_number: assignment.week_number,
            },
            include: [
                {
                    model: Student,
                    as: 'members',
                    through: { attributes: [] },
                },
            ],
        });
    } else {
        groups = [];
    }

    res.json({
        success: true,
        data: groups,
    });
});

/**
 * Get ungraded summary - students without scores per assignment (top 3 + count) เพิ่มเติม ดูคนที่ยังไม่มีคะแนน 3 คน
 */
const getUngradedSummary = asyncHandler(async (req, res) => {
    const { course_id } = req.query;

    if (!course_id) {
        throw new ApiError(400, 'course_id is required');
    }

    // Get all active assignments for this course (include subItems to know structure)
    const assignments = await Assignment.findAll({
        where: { course_id, is_active: true },
        attributes: ['id'],
        include: [{
            model: AssignmentSubItem,
            as: 'subItems',
            attributes: ['id'],
        }],
    });

    if (assignments.length === 0) {
        return res.json({ success: true, data: {} });
    }

    const assignmentIds = assignments.map(a => a.id);

    // Build map of assignments that have sub-items
    const assignmentSubItemMap = {};
    for (const a of assignments) {
        assignmentSubItemMap[a.id] = (a.subItems && a.subItems.length > 0) ? a.subItems.length : 0;
    }

    // Get all students enrolled in this course
    const [studentsResult] = await sequelize.query(`
        SELECT DISTINCT s.id, s.student_id, s.full_name
        FROM students s
        INNER JOIN course_section_students css ON s.id = css.student_id
        INNER JOIN course_sections cs ON css.course_section_id = cs.id
        WHERE cs.course_id = ?
        ORDER BY s.student_id
    `, {
        replacements: [course_id],
    });

    const totalStudents = studentsResult.length;

    if (totalStudents === 0) {
        return res.json({ success: true, data: {} });
    }

    const studentIds = studentsResult.map(s => s.id);

    // Get ALL score records (both main and sub-item) for these assignments and students
    // A student is considered "graded" if they have ANY score record for that assignment
    const scores = await Score.findAll({
        where: {
            assignment_id: { [Op.in]: assignmentIds },
            student_id: { [Op.in]: studentIds },
        },
        attributes: ['assignment_id', 'student_id'],
        group: ['assignment_id', 'student_id'],
    });

    // Build set of scored student IDs per assignment
    const scoredMap = {};
    for (const score of scores) {
        if (!scoredMap[score.assignment_id]) {
            scoredMap[score.assignment_id] = new Set();
        }
        scoredMap[score.assignment_id].add(score.student_id);
    }

    // Build student lookup
    const studentMap = {};
    for (const s of studentsResult) {
        studentMap[s.id] = { student_id: s.student_id, full_name: s.full_name };
    }

    // For each assignment, find ungraded students (top 3 + total count)
    const summary = {};
    for (const assignmentId of assignmentIds) {
        const scored = scoredMap[assignmentId] || new Set();
        const ungradedStudents = [];

        for (const sid of studentIds) {
            if (!scored.has(sid)) {
                ungradedStudents.push(studentMap[sid]);
            }
        }

        summary[assignmentId] = {
            ungraded_count: ungradedStudents.length,
            total_students: totalStudents,
            graded_count: scored.size,
            students: ungradedStudents.slice(0, 3),
        };
    }

    res.json({
        success: true,
        data: summary,
    });
});

module.exports = {
    getScores,
    submitScore,
    submitBulkScores,
    submitGroupScore,
    requestScoreEdit,
    getPendingEditRequests,
    reviewEditRequest,
    getStudentScoresSummary,
    getScoreSummaryMatrix,
    searchStudents,
    getGroupsForAssignment,
    getUngradedSummary,
};
