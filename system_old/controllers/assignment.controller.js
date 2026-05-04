const { Assignment, AssignmentSubItem, AssignmentAttendanceLink, Score, Course, Student, User, StudentGroup, AttendanceSession, sequelize } = require('../models');
const { Op } = require('sequelize');
const asyncHandler = require('../utils/asyncHandler');
const ApiError = require('../utils/ApiError');
const { logCourseActivity } = require('../utils/courseActivityLogger');

/**
 * Get all assignments for a course
 */
const getAssignments = asyncHandler(async (req, res) => {
    const { course_id } = req.query;

    if (!course_id) {
        throw new ApiError(400, 'course_id is required');
    }

    const assignments = await Assignment.findAll({
        where: { 
            course_id,
            is_active: true 
        },
        include: [
            {
                model: AssignmentSubItem,
                as: 'subItems',
                order: [['order_index', 'ASC']],
            },
            {
                model: User,
                as: 'creator',
                attributes: ['id', 'full_name'],
            },
            // Legacy single link (backward compatibility)
            {
                model: AttendanceSession,
                as: 'linkedAttendanceSession',
                attributes: ['id', 'title', 'start_time', 'end_time', 'session_type'],
            },
            // New many-to-many links
            {
                model: AttendanceSession,
                as: 'linkedAttendanceSessions',
                attributes: ['id', 'title', 'start_time', 'end_time', 'session_type', 'course_section_id'],
                through: { attributes: [] },
            },
        ],
        order: [['order_index', 'ASC'], ['created_at', 'DESC']],
    });

    res.json({
        success: true,
        data: assignments,
    });
});

/**
 * Get single assignment with details
 */
const getAssignment = asyncHandler(async (req, res) => {
    const { id } = req.params;

    

    const assignment = await Assignment.findByPk(id, {
        include: [
            {
                model: AssignmentSubItem,
                as: 'subItems',
                order: [['order_index', 'ASC']],
            },
            {
                model: User,
                as: 'creator',
                attributes: ['id', 'full_name'],
            },
            {
                model: AttendanceSession,
                as: 'linkedAttendanceSession',
                attributes: ['id', 'title', 'start_time', 'end_time', 'session_type'],
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

    res.json({
        success: true,
        data: assignment,
    });
});

/**
 * Create new assignment
 */
const createAssignment = asyncHandler(async (req, res) => {
    const { 
        course_id, 
        name, 
        description, 
        assignment_type, 
        week_number,
        linked_attendance_session_id, // Legacy: single session
        linked_attendance_session_ids, // New: array of session IDs
        attendance_condition, // 'and' or 'or'
        max_score, 
        sub_items,
        due_date,
        is_score_visible, // Whether students can see their scores
    } = req.body;

    if (!course_id || !name) {
        throw new ApiError(400, 'course_id and name are required');
    }

    // Validate linked attendance sessions if provided (new array format)
    const sessionIdsToLink = linked_attendance_session_ids || 
        (linked_attendance_session_id ? [linked_attendance_session_id] : []);
    
    if (sessionIdsToLink.length > 0) {
        const validSessions = await AttendanceSession.findAll({
            where: { 
                id: { [Op.in]: sessionIdsToLink },
                course_id: course_id 
            }
        });
        if (validSessions.length !== sessionIdsToLink.length) {
            throw new ApiError(400, 'One or more attendance sessions are invalid or do not belong to this course');
        }
    }

    // Get max order_index for this course
    const maxOrder = await Assignment.max('order_index', {
        where: { course_id },
    }) || 0;

    const transaction = await sequelize.transaction();

    try {
        // Create assignment
        const assignment = await Assignment.create({
            course_id,
            name,
            description,
            assignment_type: assignment_type || 'individual',
            week_number,
            // Legacy field - still set for backward compatibility
            linked_attendance_session_id: sessionIdsToLink.length === 1 ? sessionIdsToLink[0] : null,
            attendance_condition: attendance_condition || 'or',
            max_score: max_score || 10,
            due_date,
            order_index: maxOrder + 1,
            is_score_visible: is_score_visible !== false, // Default to true
            created_by: req.user.id,
        }, { transaction });

        // Create attendance links (new many-to-many)
        if (sessionIdsToLink.length > 0) {
            const linkData = sessionIdsToLink.map(sessionId => ({
                assignment_id: assignment.id,
                attendance_session_id: sessionId,
            }));
            await AssignmentAttendanceLink.bulkCreate(linkData, { transaction });
        }

        // Create sub-items if any
        if (sub_items && sub_items.length > 0) {
            const subItemsData = sub_items.map((item, index) => ({
                assignment_id: assignment.id,
                name: item.name,
                max_score: item.max_score || 10,
                order_index: index,
            }));

            await AssignmentSubItem.bulkCreate(subItemsData, { transaction });

            // Update assignment max_score to sum of sub-items
            const totalScore = sub_items.reduce((sum, item) => sum + (item.max_score || 10), 0);
            await assignment.update({ max_score: totalScore }, { transaction });
        }

        await transaction.commit();

        // Fetch complete assignment with sub-items and attendance sessions
        const completeAssignment = await Assignment.findByPk(assignment.id, {
            include: [
                {
                    model: AssignmentSubItem,
                    as: 'subItems',
                    order: [['order_index', 'ASC']],
                },
                {
                    model: AttendanceSession,
                    as: 'linkedAttendanceSession',
                    attributes: ['id', 'title', 'start_time', 'end_time', 'session_type'],
                },
                {
                    model: AttendanceSession,
                    as: 'linkedAttendanceSessions',
                    attributes: ['id', 'title', 'start_time', 'end_time', 'session_type', 'course_section_id'],
                    through: { attributes: [] },
                },
            ],
        });

        logCourseActivity({ courseId: course_id, actorUserId: req.user.id, action: 'create_assignment', category: 'assignment', targetType: 'assignment', targetId: assignment.id, targetName: name, detail: { assignment_type: assignment_type || 'individual', max_score } });

        res.status(201).json({
            success: true,
            data: completeAssignment,
            message: 'Assignment created successfully',
        });
    } catch (error) {
        await transaction.rollback();
        throw error;
    }
});

/**
 * Update assignment
 */
const updateAssignment = asyncHandler(async (req, res) => {
    const { id } = req.params;
    const { 
        name, 
        description, 
        assignment_type,
        week_number,
        linked_attendance_session_id, // Legacy: single session
        linked_attendance_session_ids, // New: array of session IDs
        attendance_condition, // 'and' or 'or'
        max_score, 
        sub_items,
        due_date,
        is_score_visible, // Whether students can see their scores
    } = req.body;

    const assignment = await Assignment.findByPk(id);

    if (!assignment) {
        throw new ApiError(404, 'Assignment not found');
    }

    // Determine which session IDs to use
    const sessionIdsToLink = linked_attendance_session_ids !== undefined 
        ? linked_attendance_session_ids 
        : (linked_attendance_session_id !== undefined 
            ? (linked_attendance_session_id ? [linked_attendance_session_id] : [])
            : null); // null means don't update

    // Validate linked attendance sessions if provided
    if (sessionIdsToLink !== null && sessionIdsToLink.length > 0) {
        const validSessions = await AttendanceSession.findAll({
            where: { 
                id: { [Op.in]: sessionIdsToLink },
                course_id: assignment.course_id 
            }
        });
        if (validSessions.length !== sessionIdsToLink.length) {
            throw new ApiError(400, 'One or more attendance sessions are invalid or do not belong to this course');
        }
    }

    const transaction = await sequelize.transaction();

    try {
        // Update assignment
        const updateData = {
            name: name || assignment.name,
            description: description !== undefined ? description : assignment.description,
            assignment_type: assignment_type || assignment.assignment_type,
            week_number: week_number !== undefined ? week_number : assignment.week_number,
            max_score: max_score !== undefined ? max_score : assignment.max_score,
            due_date: due_date !== undefined ? due_date : assignment.due_date,
        };

        // Update is_score_visible if provided
        if (is_score_visible !== undefined) {
            updateData.is_score_visible = is_score_visible;
        }

        // Update attendance condition if provided
        if (attendance_condition !== undefined) {
            updateData.attendance_condition = attendance_condition;
        }

        // Update legacy field for backward compatibility
        if (sessionIdsToLink !== null) {
            updateData.linked_attendance_session_id = sessionIdsToLink.length === 1 ? sessionIdsToLink[0] : null;
        }

        await assignment.update(updateData, { transaction });

        // Update attendance links if session IDs were provided
        if (sessionIdsToLink !== null) {
            // Delete existing links
            await AssignmentAttendanceLink.destroy({
                where: { assignment_id: id },
                transaction,
            });

            // Create new links
            if (sessionIdsToLink.length > 0) {
                const linkData = sessionIdsToLink.map(sessionId => ({
                    assignment_id: parseInt(id),
                    attendance_session_id: sessionId,
                }));
                await AssignmentAttendanceLink.bulkCreate(linkData, { transaction });
            }
        }

        // Handle sub-items update if provided
        // IMPORTANT: We need to preserve scores when updating sub-items
        if (sub_items !== undefined) {
            // Get existing sub-items
            const existingSubItems = await AssignmentSubItem.findAll({
                where: { assignment_id: id },
                order: [['order_index', 'ASC']],
                transaction,
            });

            // Create a map of existing sub-items by ID for quick lookup
            const existingSubItemsMap = new Map();
            existingSubItems.forEach(item => existingSubItemsMap.set(item.id, item));

            if (sub_items && sub_items.length > 0) {
                // Track which existing sub_item IDs are still being used
                const usedExistingIds = new Set();
                // Track IDs of all final sub-items (for cleanup)
                const finalSubItemIds = new Set();

                for (let index = 0; index < sub_items.length; index++) {
                    const newItem = sub_items[index];
                    
                    if (newItem.id && existingSubItemsMap.has(newItem.id)) {
                        // Case 1: Update existing sub-item by ID
                        const existingItem = existingSubItemsMap.get(newItem.id);
                        await existingItem.update({
                            name: newItem.name,
                            max_score: newItem.max_score || 10,
                            order_index: index,
                        }, { transaction });
                        
                        usedExistingIds.add(newItem.id);
                        finalSubItemIds.add(newItem.id);
                    } else {
                        // Case 2: Create new sub-item (no ID or ID not found)
                        const createdItem = await AssignmentSubItem.create({
                            assignment_id: id,
                            name: newItem.name,
                            max_score: newItem.max_score || 10,
                            order_index: index,
                        }, { transaction });
                        
                        finalSubItemIds.add(createdItem.id);
                    }
                }

                // Find sub-items to delete (those that exist but are not used in new list)
                const subItemsToDelete = existingSubItems.filter(e => !usedExistingIds.has(e.id));
                
                if (subItemsToDelete.length > 0) {
                    const idsToDelete = subItemsToDelete.map(s => s.id);
                    
                    // Update scores: set sub_item_id to null for orphaned scores
                    // This preserves the score data but disassociates from deleted sub-items
                    await Score.update(
                        { sub_item_id: null },
                        { 
                            where: { 
                                assignment_id: id,
                                sub_item_id: { [Op.in]: idsToDelete }
                            },
                            transaction 
                        }
                    );

                    // Now safe to delete the sub-items
                    await AssignmentSubItem.destroy({
                        where: { id: { [Op.in]: idsToDelete } },
                        transaction,
                    });
                }

                // Update max_score to sum of sub-items
                const totalScore = sub_items.reduce((sum, item) => sum + (item.max_score || 10), 0);
                await assignment.update({ max_score: totalScore }, { transaction });
            } else {
                // sub_items is empty array - remove all sub-items
                // First, set all scores' sub_item_id to null to preserve scores
                await Score.update(
                    { sub_item_id: null },
                    { 
                        where: { 
                            assignment_id: id,
                            sub_item_id: { [Op.ne]: null }
                        },
                        transaction 
                    }
                );

                // Then delete all sub-items
                await AssignmentSubItem.destroy({
                    where: { assignment_id: id },
                    transaction,
                });
            }
        }

        await transaction.commit();

        // Fetch updated assignment
        const updatedAssignment = await Assignment.findByPk(id, {
            include: [
                {
                    model: AssignmentSubItem,
                    as: 'subItems',
                    order: [['order_index', 'ASC']],
                },
                {
                    model: AttendanceSession,
                    as: 'linkedAttendanceSession',
                    attributes: ['id', 'title', 'start_time', 'end_time', 'session_type'],
                },
                {
                    model: AttendanceSession,
                    as: 'linkedAttendanceSessions',
                    attributes: ['id', 'title', 'start_time', 'end_time', 'session_type', 'course_section_id'],
                    through: { attributes: [] },
                },
            ],
        });

        logCourseActivity({ courseId: updatedAssignment.course_id, actorUserId: req.user.id, action: 'update_assignment', category: 'assignment', targetType: 'assignment', targetId: id, targetName: updatedAssignment.name, detail: { fields: Object.keys(req.body) } });

        res.json({
            success: true,
            data: updatedAssignment,
            message: 'Assignment updated successfully',
        });
    } catch (error) {
        await transaction.rollback();
        throw error;
    }
});

/**
 * Delete assignment (soft delete)
 */
const deleteAssignment = asyncHandler(async (req, res) => {
    const { id } = req.params;

    const assignment = await Assignment.findByPk(id);

    if (!assignment) {
        throw new ApiError(404, 'Assignment not found');
    }

    // Soft delete by setting is_active to false
    await assignment.update({ is_active: false });

    logCourseActivity({ courseId: assignment.course_id, actorUserId: req.user.id, action: 'delete_assignment', category: 'assignment', targetType: 'assignment', targetId: id, targetName: assignment.name });

    res.json({
        success: true,
        message: 'Assignment deleted successfully',
    });
});

/**
 * Reorder assignments
 */
const reorderAssignments = asyncHandler(async (req, res) => {
    const { assignments } = req.body; // Array of { id, order_index }

    if (!assignments || !Array.isArray(assignments)) {
        throw new ApiError(400, 'assignments array is required');
    }

    const transaction = await sequelize.transaction();

    try {
        for (const item of assignments) {
            await Assignment.update(
                { order_index: item.order_index },
                { where: { id: item.id }, transaction }
            );
        }

        await transaction.commit();

        res.json({
            success: true,
            message: 'Assignments reordered successfully',
        });
    } catch (error) {
        await transaction.rollback();
        throw error;
    }
});

module.exports = {
    getAssignments,
    getAssignment,
    createAssignment,
    updateAssignment,
    deleteAssignment,
    reorderAssignments,
};
