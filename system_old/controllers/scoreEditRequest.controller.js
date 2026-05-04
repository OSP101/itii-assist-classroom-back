const { ScoreEditRequest, Score, Assignment, AssignmentSubItem, Student, User, Course, CourseSection, CourseSectionStudent, CourseTA, sequelize } = require('../models');
const { Op } = require('sequelize');
const asyncHandler = require('../utils/asyncHandler');
const ApiError = require('../utils/ApiError');

const getRequesterDisplayName = (requester) => requester?.full_name || requester?.username || `user_id:${requester?.id ?? '-'}`;

const findPendingRequestForStudentAssignment = async (assignmentId, studentId) => {
    return ScoreEditRequest.findOne({
        where: { status: 'pending' },
        include: [
            {
                model: Score,
                as: 'score',
                required: true,
                where: {
                    assignment_id: assignmentId,
                    student_id: studentId,
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
                attributes: ['id', 'username', 'full_name'],
            },
        ],
    });
};

/**
 * Get all score edit requests for a course (instructor or TA of this course)
 * - Instructor: sees all requests
 * - TA: sees only their own requests (requested_by = userId)
 */
const getEditRequests = asyncHandler(async (req, res) => {
    const { course_id, status } = req.query;
    const userId = req.user.id;
    const userRole = req.user.role;

    if (!course_id) {
        throw new ApiError(400, 'course_id is required');
    }

    // Verify user is instructor or TA of this course
    const course = await Course.findByPk(course_id);
    if (!course) {
        throw new ApiError(404, 'Course not found');
    }

    const isInstructor = String(course.instructor_id) === String(userId);
    let isTA = false;
    if (!isInstructor) {
        const taRecord = await CourseTA.findOne({ where: { course_id, user_id: userId } });
        isTA = !!taRecord;
    }

    if (!isInstructor && !isTA) {
        throw new ApiError(403, 'Only instructor or TA can view edit requests');
    }

    // Build where clause
    const whereClause = {};
    if (status) {
        whereClause.status = status;
    }
    // TA can only see their own requests
    if (isTA && !isInstructor) {
        whereClause.requested_by = userId;
    }

    // Get all edit requests for assignments in this course
    const editRequests = await ScoreEditRequest.findAll({
        where: whereClause,
        include: [
            {
                model: Score,
                as: 'score',
                required: true,
                include: [
                    {
                        model: Assignment,
                        as: 'assignment',
                        where: { course_id },
                        include: [
                            {
                                model: AssignmentSubItem,
                                as: 'subItems',
                            }
                        ]
                    },
                    {
                        model: Student,
                        as: 'student',
                        attributes: ['id', 'student_id', 'full_name'],
                    },
                    {
                        model: AssignmentSubItem,
                        as: 'subItem',
                        attributes: ['id', 'name', 'max_score'],
                    },
                ],
            },
            {
                model: User,
                as: 'requester',
                attributes: ['id', 'username', 'full_name'],
            },
            {
                model: User,
                as: 'reviewer',
                attributes: ['id', 'username', 'full_name'],
            },
        ],
        order: [['created_at', 'DESC']],
    });

    // Format response
    const formatted = editRequests.map(req => ({
        id: req.id,
        status: req.status,
        old_score: req.old_score ? parseFloat(req.old_score) : null,
        new_score: parseFloat(req.new_score),
        reason: req.reason,
        images: req.images || [],
        review_comment: req.review_comment,
        created_at: req.created_at,
        reviewed_at: req.reviewed_at,
        score: {
            id: req.score.id,
            current_score: req.score.score ? parseFloat(req.score.score) : null,
        },
        assignment: {
            id: req.score.assignment.id,
            name: req.score.assignment.name,
            max_score: parseFloat(req.score.assignment.max_score),
        },
        sub_item: req.score.subItem ? {
            id: req.score.subItem.id,
            name: req.score.subItem.name,
            max_score: parseFloat(req.score.subItem.max_score),
        } : null,
        student: {
            id: req.score.student.id,
            student_id: req.score.student.student_id,
            full_name: req.score.student.full_name,
        },
        requester: {
            id: req.requester.id,
            username: req.requester.username,
            full_name: req.requester.full_name,
        },
        reviewer: req.reviewer ? {
            id: req.reviewer.id,
            username: req.reviewer.username,
            full_name: req.reviewer.full_name,
        } : null,
    }));

    // Count by status (TA sees only their own counts)
    const countWhereBase = isTA && !isInstructor ? { requested_by: userId } : {};

    const pendingCount = await ScoreEditRequest.count({
        include: [{
            model: Score,
            as: 'score',
            required: true,
            include: [{
                model: Assignment,
                as: 'assignment',
                where: { course_id },
            }]
        }],
        where: { ...countWhereBase, status: 'pending' },
    });

    const approvedCount = await ScoreEditRequest.count({
        include: [{
            model: Score,
            as: 'score',
            required: true,
            include: [{
                model: Assignment,
                as: 'assignment',
                where: { course_id },
            }]
        }],
        where: { ...countWhereBase, status: 'approved' },
    });

    const rejectedCount = await ScoreEditRequest.count({
        include: [{
            model: Score,
            as: 'score',
            required: true,
            include: [{
                model: Assignment,
                as: 'assignment',
                where: { course_id },
            }]
        }],
        where: { ...countWhereBase, status: 'rejected' },
    });

    res.json({
        success: true,
        data: formatted,
        counts: {
            pending: pendingCount,
            approved: approvedCount,
            rejected: rejectedCount,
        },
        role: isInstructor ? 'instructor' : 'ta',
    });
});

/**
 * Get pending count for a course (for badge)
 * - Instructor: count of all pending requests
 * - TA: count of their own pending requests
 */
const getPendingCount = asyncHandler(async (req, res) => {
    const { course_id } = req.query;
    const userId = req.user.id;

    if (!course_id) {
        throw new ApiError(400, 'course_id is required');
    }

    // Verify user is instructor or TA of this course
    const course = await Course.findByPk(course_id);
    if (!course) {
        throw new ApiError(404, 'Course not found');
    }

    const isInstructor = String(course.instructor_id) === String(userId);
    let isTA = false;
    if (!isInstructor) {
        const taRecord = await CourseTA.findOne({ where: { course_id, user_id: userId } });
        isTA = !!taRecord;
    }

    if (!isInstructor && !isTA) {
        throw new ApiError(403, 'Only instructor or TA can view pending count');
    }

    // TA can only see count of their own requests
    const countWhere = { status: 'pending' };
    if (isTA && !isInstructor) {
        countWhere.requested_by = userId;
    }

    const count = await ScoreEditRequest.count({
        include: [{
            model: Score,
            as: 'score',
            required: true,
            include: [{
                model: Assignment,
                as: 'assignment',
                where: { course_id },
            }]
        }],
        where: countWhere,
    });

    res.json({
        success: true,
        count,
    });
});

/**
 * Create a score edit request (TA only)
 */
const createEditRequest = asyncHandler(async (req, res) => {
    let { score_id, new_score, reason } = req.body;
    const userId = req.user.id;

    // Parse values from FormData (strings)
    if (typeof score_id === 'string') {
        score_id = parseInt(score_id, 10);
    }
    if (typeof new_score === 'string') {
        new_score = parseFloat(new_score);
    }

    if (!score_id || new_score === undefined || isNaN(new_score)) {
        throw new ApiError(400, 'score_id and new_score are required');
    }

    // Get the score with assignment info
    const score = await Score.findByPk(score_id, {
        include: [
            {
                model: Assignment,
                as: 'assignment',
                include: [{ model: Course, as: 'course' }],
            },
            {
                model: AssignmentSubItem,
                as: 'subItem',
            },
        ],
    });

    if (!score) {
        throw new ApiError(404, 'Score not found');
    }

    // Validate new_score
    const maxScore = score.subItem ? score.subItem.max_score : score.assignment.max_score;
    if (new_score < 0 || new_score > maxScore) {
        throw new ApiError(400, `Score must be between 0 and ${maxScore}`);
    }

    // Block duplicate pending requests at assignment-level for this student.
    const existingRequest = await findPendingRequestForStudentAssignment(score.assignment_id, score.student_id);

    if (existingRequest) {
        const studentName = existingRequest.score?.student?.full_name || `student_id:${score.student_id}`;
        const requesterName = getRequesterDisplayName(existingRequest.requester);
        throw new ApiError(400, `นักศึกษา ${studentName} มีคำร้องแก้ไขคะแนนที่รออนุมัติอยู่แล้ว (ส่งโดย ${requesterName})`);
    }

    // Process uploaded images
    let imagePaths = null;
    if (req.files && req.files.length > 0) {
        imagePaths = req.files.map(file => `uploads/score-edit-requests/${file.filename}`);
    }

    // Create edit request
    const editRequest = await ScoreEditRequest.create({
        score_id,
        old_score: score.score,
        new_score,
        reason: reason || null,
        images: imagePaths,
        status: 'pending',
        requested_by: userId,
    });

    res.status(201).json({
        success: true,
        message: 'Edit request created successfully',
        data: {
            id: editRequest.id,
            status: editRequest.status,
            images: editRequest.images,
        },
    });
});

/**
 * Create batch score edit requests for group assignment (TA only)
 * This creates multiple edit requests at once for all members in a group
 */
const createBatchEditRequest = asyncHandler(async (req, res) => {
    // Handle both JSON and FormData
    let score_ids = req.body.score_ids;
    let new_score = req.body.new_score;
    const { reason } = req.body;
    const userId = req.user.id;

    // If score_ids is a string (from FormData), parse it
    if (typeof score_ids === 'string') {
        try {
            score_ids = JSON.parse(score_ids);
        } catch (e) {
            throw new ApiError(400, 'Invalid score_ids format');
        }
    }

    // Parse new_score if it's a string (from FormData)
    if (typeof new_score === 'string') {
        new_score = parseFloat(new_score);
    }

    if (!score_ids || !Array.isArray(score_ids) || score_ids.length === 0) {
        throw new ApiError(400, 'score_ids array is required and must not be empty');
    }

    if (new_score === undefined || isNaN(new_score)) {
        throw new ApiError(400, 'new_score is required and must be a valid number');
    }

    // Get all scores with assignment info
    const scores = await Score.findAll({
        where: {
            id: { [Op.in]: score_ids },
        },
        include: [
            {
                model: Assignment,
                as: 'assignment',
                include: [{ model: Course, as: 'course' }],
            },
            {
                model: AssignmentSubItem,
                as: 'subItem',
            },
            {
                model: Student,
                as: 'student',
                attributes: ['id', 'student_id', 'full_name'],
            },
        ],
    });

    if (scores.length === 0) {
        throw new ApiError(404, 'No scores found');
    }

    // Validate new_score against first score's max (all should have same max)
    const maxScore = scores[0].subItem ? scores[0].subItem.max_score : scores[0].assignment.max_score;
    if (new_score < 0 || new_score > maxScore) {
        throw new ApiError(400, `Score must be between 0 and ${maxScore}`);
    }

    const assignmentIds = [...new Set(scores.map(score => score.assignment_id))];
    if (assignmentIds.length !== 1) {
        throw new ApiError(400, 'All score_ids must belong to the same assignment');
    }

    // Check for existing pending requests by assignment + student.
    const studentIds = [...new Set(scores.map(score => score.student_id))];
    const existingRequests = await ScoreEditRequest.findAll({
        where: {
            status: 'pending',
        },
        include: [
            {
                model: Score,
                as: 'score',
                required: true,
                where: {
                    assignment_id: assignmentIds[0],
                    student_id: { [Op.in]: studentIds },
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
                attributes: ['id', 'username', 'full_name'],
            },
        ],
    });

    // Separate scores into those that can proceed and those already pending
    const alreadyPendingStudentIds = new Set(
        existingRequests.map(r => r.score?.student_id).filter(Boolean)
    );
    const scoresToProcess = scores.filter(score => !alreadyPendingStudentIds.has(score.student_id));

    // If ALL submitted members already have pending requests, return an error
    if (scoresToProcess.length === 0) {
        const pendingDetails = new Map();
        for (const request of existingRequests) {
            const studentId = request.score?.student_id;
            if (!pendingDetails.has(studentId)) {
                const studentName = request.score?.student?.full_name || `student_id:${studentId}`;
                const requesterName = getRequesterDisplayName(request.requester);
                pendingDetails.set(studentId, `${studentName} (ส่งโดย ${requesterName})`);
            }
        }
        throw new ApiError(400, `มีคำร้องแก้ไขคะแนนที่รออนุมัติอยู่แล้ว: ${[...pendingDetails.values()].join(', ')}`);
    }

    // Build info about skipped members (already have pending requests)
    const skippedDetails = [];
    for (const request of existingRequests) {
        const studentId = request.score?.student_id;
        if (alreadyPendingStudentIds.has(studentId)) {
            const studentName = request.score?.student?.full_name || `student_id:${studentId}`;
            skippedDetails.push(studentName);
        }
    }

    // Process uploaded images - same images for all batch requests
    let imagePaths = null;
    if (req.files && req.files.length > 0) {
        imagePaths = req.files.map(file => `uploads/score-edit-requests/${file.filename}`);
    }

    // Start transaction
    const t = await sequelize.transaction();

    try {
        // Create edit requests only for scores without existing pending requests
        const editRequests = await Promise.all(
            scoresToProcess.map(score =>
                ScoreEditRequest.create({
                    score_id: score.id,
                    old_score: score.score,
                    new_score,
                    reason: reason || null,
                    images: imagePaths,
                    status: 'pending',
                    requested_by: userId,
                }, { transaction: t })
            )
        );

        await t.commit();

        const message = skippedDetails.length > 0
            ? `สร้างคำร้องแก้ไข ${editRequests.length} รายการ (ข้าม ${skippedDetails.length} รายการที่มีคำร้องรออนุมัติอยู่แล้ว: ${skippedDetails.join(', ')})`
            : `Created ${editRequests.length} edit request(s) successfully`;

        res.status(201).json({
            success: true,
            message,
            data: {
                count: editRequests.length,
                skipped: skippedDetails.length,
                skipped_names: skippedDetails,
                requests: editRequests.map(r => ({
                    id: r.id,
                    score_id: r.score_id,
                    status: r.status,
                    images: r.images,
                })),
            },
        });
    } catch (error) {
        await t.rollback();
        throw error;
    }
});

/**
 * Approve a score edit request (instructor only)
 */
const approveEditRequest = asyncHandler(async (req, res) => {
    const { id } = req.params;
    const { comment } = req.body;
    const userId = req.user.id;

    const editRequest = await ScoreEditRequest.findByPk(id, {
        include: [
            {
                model: Score,
                as: 'score',
                include: [
                    {
                        model: Assignment,
                        as: 'assignment',
                        include: [{ model: Course, as: 'course' }],
                    },
                ],
            },
        ],
    });

    if (!editRequest) {
        throw new ApiError(404, 'Edit request not found');
    }

    // Verify user is instructor
    if (editRequest.score.assignment.course.instructor_id !== userId) {
        throw new ApiError(403, 'Only instructor can approve edit requests');
    }

    if (editRequest.status !== 'pending') {
        throw new ApiError(400, 'This request has already been processed');
    }

    // Start transaction
    const t = await sequelize.transaction();

    try {
        // Update the score
        await Score.update(
            {
                score: editRequest.new_score,
                graded_at: new Date(),
            },
            {
                where: { id: editRequest.score_id },
                transaction: t,
            }
        );

        // Update the edit request
        await editRequest.update(
            {
                status: 'approved',
                reviewed_by: userId,
                reviewed_at: new Date(),
                review_comment: comment || null,
            },
            { transaction: t }
        );

        await t.commit();

        res.json({
            success: true,
            message: 'Score edit request approved',
        });
    } catch (error) {
        await t.rollback();
        throw error;
    }
});

/**
 * Reject a score edit request (instructor only)
 */
const rejectEditRequest = asyncHandler(async (req, res) => {
    const { id } = req.params;
    const { comment } = req.body;
    const userId = req.user.id;

    if (!comment) {
        throw new ApiError(400, 'Rejection reason is required');
    }

    const editRequest = await ScoreEditRequest.findByPk(id, {
        include: [
            {
                model: Score,
                as: 'score',
                include: [
                    {
                        model: Assignment,
                        as: 'assignment',
                        include: [{ model: Course, as: 'course' }],
                    },
                ],
            },
        ],
    });

    if (!editRequest) {
        throw new ApiError(404, 'Edit request not found');
    }

    // Verify user is instructor
    if (editRequest.score.assignment.course.instructor_id !== userId) {
        throw new ApiError(403, 'Only instructor can reject edit requests');
    }

    if (editRequest.status !== 'pending') {
        throw new ApiError(400, 'This request has already been processed');
    }

    // Update the edit request
    await editRequest.update({
        status: 'rejected',
        reviewed_by: userId,
        reviewed_at: new Date(),
        review_comment: comment,
    });

    res.json({
        success: true,
        message: 'Score edit request rejected',
    });
});

/**
 * Cancel a pending score edit request (requester only)
 */
const cancelEditRequest = asyncHandler(async (req, res) => {
    const { id } = req.params;
    const userId = req.user.id;

    const editRequest = await ScoreEditRequest.findByPk(id);

    if (!editRequest) {
        throw new ApiError(404, 'Edit request not found');
    }

    if (String(editRequest.requested_by) !== String(userId)) {
        throw new ApiError(403, 'Only the requester can cancel this edit request');
    }

    if (editRequest.status !== 'pending') {
        throw new ApiError(400, 'Only pending edit requests can be cancelled');
    }

    await editRequest.destroy();

    res.json({
        success: true,
        message: 'Score edit request cancelled',
    });
});

/**
 * Batch approve multiple score edit requests (instructor only)
 * Used for approving group edit requests at once
 */
const batchApproveEditRequests = asyncHandler(async (req, res) => {
    const { request_ids, comment } = req.body;
    const userId = req.user.id;

    if (!request_ids || !Array.isArray(request_ids) || request_ids.length === 0) {
        throw new ApiError(400, 'request_ids array is required');
    }

    // Get all edit requests with their scores
    const editRequests = await ScoreEditRequest.findAll({
        where: {
            id: { [Op.in]: request_ids },
            status: 'pending',
        },
        include: [
            {
                model: Score,
                as: 'score',
                include: [
                    {
                        model: Assignment,
                        as: 'assignment',
                        include: [{ model: Course, as: 'course' }],
                    },
                ],
            },
        ],
    });

    if (editRequests.length === 0) {
        throw new ApiError(404, 'No pending edit requests found');
    }

    // Verify user is instructor for all requests (should be same course)
    const courseId = editRequests[0].score.assignment.course.id;
    const instructorId = editRequests[0].score.assignment.course.instructor_id;
    
    if (String(instructorId) !== String(userId)) {
        throw new ApiError(403, 'Only instructor can approve edit requests');
    }

    // Verify all requests are from the same course
    const allSameCourse = editRequests.every(
        r => r.score.assignment.course.id === courseId
    );
    if (!allSameCourse) {
        throw new ApiError(400, 'All requests must be from the same course');
    }

    // Start transaction
    const t = await sequelize.transaction();

    try {
        // Update all scores and edit requests
        for (const editRequest of editRequests) {
            // Update the score
            await Score.update(
                {
                    score: editRequest.new_score,
                    graded_at: new Date(),
                },
                {
                    where: { id: editRequest.score_id },
                    transaction: t,
                }
            );

            // Update the edit request
            await editRequest.update(
                {
                    status: 'approved',
                    reviewed_by: userId,
                    reviewed_at: new Date(),
                    review_comment: comment || null,
                },
                { transaction: t }
            );
        }

        await t.commit();

        res.json({
            success: true,
            message: `Approved ${editRequests.length} edit request(s)`,
            count: editRequests.length,
        });
    } catch (error) {
        await t.rollback();
        throw error;
    }
});

/**
 * Batch reject multiple score edit requests (instructor only)
 * Used for rejecting group edit requests at once
 */
const batchRejectEditRequests = asyncHandler(async (req, res) => {
    const { request_ids, comment } = req.body;
    const userId = req.user.id;

    if (!request_ids || !Array.isArray(request_ids) || request_ids.length === 0) {
        throw new ApiError(400, 'request_ids array is required');
    }

    if (!comment) {
        throw new ApiError(400, 'Rejection reason is required');
    }

    // Get all edit requests
    const editRequests = await ScoreEditRequest.findAll({
        where: {
            id: { [Op.in]: request_ids },
            status: 'pending',
        },
        include: [
            {
                model: Score,
                as: 'score',
                include: [
                    {
                        model: Assignment,
                        as: 'assignment',
                        include: [{ model: Course, as: 'course' }],
                    },
                ],
            },
        ],
    });

    if (editRequests.length === 0) {
        throw new ApiError(404, 'No pending edit requests found');
    }

    // Verify user is instructor
    const instructorId = editRequests[0].score.assignment.course.instructor_id;
    if (String(instructorId) !== String(userId)) {
        throw new ApiError(403, 'Only instructor can reject edit requests');
    }

    // Start transaction
    const t = await sequelize.transaction();

    try {
        for (const editRequest of editRequests) {
            await editRequest.update(
                {
                    status: 'rejected',
                    reviewed_by: userId,
                    reviewed_at: new Date(),
                    review_comment: comment,
                },
                { transaction: t }
            );
        }

        await t.commit();

        res.json({
            success: true,
            message: `Rejected ${editRequests.length} edit request(s)`,
            count: editRequests.length,
        });
    } catch (error) {
        await t.rollback();
        throw error;
    }
});

module.exports = {
    getEditRequests,
    getPendingCount,
    createEditRequest,
    createBatchEditRequest,
    cancelEditRequest,
    approveEditRequest,
    rejectEditRequest,
    batchApproveEditRequests,
    batchRejectEditRequests,
};
