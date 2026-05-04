/**
 * Queue Controller
 * ระบบจองคิวตรวจงาน
 * 
 * REFACTORED: Real-time states now stored in Redis for performance.
 * MySQL is used only for persistence of completed/cancelled bookings.
 */

const { Op } = require('sequelize');
const {
    QueueSession,
    QueueWorker,
    QueueBooking,
    QueueDeskStatus,
    Course,
    CourseSection,
    CourseSectionStudent,
    Classroom,
    Desk,
    Zone,
    Assignment,
    AssignmentSubItem,
    AttendanceSession,
    AttendanceRecord,
    Student,
    User,
    Score,
    sequelize,
} = require('../models');
const logger = require('../utils/logger');
const fcmService = require('../utils/fcmService');
const { logCourseActivity } = require('../utils/courseActivityLogger');

/**
 * Find which zone a desk belongs to based on its (x, y) position
 * Returns the zone object if desk center is inside, or null
 */
const findZoneForDesk = async (desk, classroomId) => {
    if (!desk || !classroomId) return null;
    try {
        const zones = await Zone.findAll({
            where: { classroom_id: classroomId },
            attributes: ['id', 'name', 'x', 'y', 'width', 'height', 'color'],
        });
        // Desk (x, y) is top-left corner, check if it falls within any zone
        const deskX = desk.x || 0;
        const deskY = desk.y || 0;
        for (const zone of zones) {
            if (
                deskX >= zone.x &&
                deskX < zone.x + zone.width &&
                deskY >= zone.y &&
                deskY < zone.y + zone.height
            ) {
                return { id: zone.id, name: zone.name, color: zone.color };
            }
        }
        return null;
    } catch (err) {
        logger.error('Error finding zone for desk:', err);
        return null;
    }
};

/**
 * Enrich a booking object with zone info from its desk
 */
const enrichBookingWithZone = async (booking, classroomId) => {
    if (!booking || !booking.desk) return booking;
    const zone = await findZoneForDesk(booking.desk, classroomId);
    const plain = booking.toJSON ? booking.toJSON() : { ...booking };
    plain.zone = zone;
    return plain;
};

// Redis Queue Service - handles real-time states
const redisQueue = require('../utils/redisQueueService');
const { triggerAssignmentForSession, invalidateSessionCache } = require('../utils/queueAssignmentWorker');

// ============================================
// Queue Session Management (Instructor/TA)
// ============================================

/**
 * Get all queue sessions for a course
 */
const getQueueSessions = async (req, res) => {
    try {
        const { courseId } = req.params;
        const { status } = req.query;

        const where = { course_id: courseId };
        if (status) {
            where.status = status;
        }

        const sessions = await QueueSession.findAll({
            where,
            include: [
                {
                    model: Classroom,
                    as: 'classroom',
                    attributes: ['id', 'name', 'building', 'floor'],
                },
                {
                    model: Assignment,
                    as: 'linkedAssignment',
                    attributes: ['id', 'name', 'max_score'],
                },
                {
                    model: AttendanceSession,
                    as: 'linkedAttendanceSession',
                    attributes: ['id', 'title'],
                },
                {
                    model: User,
                    as: 'creator',
                    attributes: ['id', 'full_name'],
                },
            ],
            order: [['created_at', 'DESC']],
        });

        // ✅ OPTIMIZED: Get statistics for ALL sessions in a single batch query
        const sessionIds = sessions.map(s => s.id);
        
        const allStats = sessionIds.length > 0 ? await QueueBooking.findAll({
            where: { queue_session_id: { [Op.in]: sessionIds } },
            attributes: [
                'queue_session_id',
                'status',
                [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
            ],
            group: ['queue_session_id', 'status'],
            raw: true,
        }) : [];

        // Build stats map: session_id -> { waiting, in_progress, completed, total }
        const statsMap = {};
        sessionIds.forEach(id => {
            statsMap[id] = { total: 0, waiting: 0, in_progress: 0, completed: 0 };
        });
        allStats.forEach(row => {
            const sessionId = row.queue_session_id;
            const status = row.status;
            const count = parseInt(row.count);
            if (statsMap[sessionId]) {
                statsMap[sessionId][status] = count;
                statsMap[sessionId].total += count;
            }
        });

        // Map sessions with stats (no additional queries)
        const sessionsWithStats = sessions.map(session => ({
            ...session.toJSON(),
            stats: statsMap[session.id] || { total: 0, waiting: 0, in_progress: 0, completed: 0 },
        }));

        res.json({
            success: true,
            data: sessionsWithStats,
        });
    } catch (error) {
        console.error('Error getting queue sessions:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Get single queue session details
 */
const getQueueSession = async (req, res) => {
    try {
        const { sessionId } = req.params;

        const session = await QueueSession.findByPk(sessionId, {
            include: [
                {
                    model: Classroom,
                    as: 'classroom',
                    include: [
                        {
                            model: Desk,
                            as: 'desks',
                            where: { is_enabled: true },
                            required: false,
                            order: [['number', 'ASC']],
                        },
                    ],
                },
                {
                    model: Assignment,
                    as: 'linkedAssignment',
                    include: [
                        {
                            model: AssignmentSubItem,
                            as: 'subItems',
                        },
                    ],
                },
                {
                    model: AttendanceSession,
                    as: 'linkedAttendanceSession',
                },
                {
                    model: User,
                    as: 'creator',
                    attributes: ['id', 'full_name'],
                },
                {
                    model: QueueWorker,
                    as: 'workers',
                    include: [
                        {
                            model: User,
                            as: 'user',
                            attributes: ['id', 'full_name', 'avatar'],
                        },
                    ],
                },
            ],
        });

        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        // Get desk statuses
        const deskStatuses = await QueueDeskStatus.findAll({
            where: { queue_session_id: sessionId },
            include: [
                {
                    model: Desk,
                    as: 'desk',
                },
            ],
        });

        // Get booking statistics
        const stats = await QueueBooking.findAll({
            where: { queue_session_id: sessionId },
            attributes: [
                'booking_type',
                'status',
                [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
            ],
            group: ['booking_type', 'status'],
            raw: true,
        });

        res.json({
            success: true,
            data: {
                ...session.toJSON(),
                deskStatuses,
                statistics: stats,
            },
        });
    } catch (error) {
        console.error('Error getting queue session:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Create new queue session
 */
const createQueueSession = async (req, res) => {
    const transaction = await sequelize.transaction();

    try {
        const { courseId } = req.params;
        const {
            title,
            description,
            classroom_id,
            linked_assignment_id,
            require_attendance,
            linked_attendance_session_id,
        } = req.body;

        // Validate classroom exists
        const classroom = await Classroom.findByPk(classroom_id);
        if (!classroom) {
            await transaction.rollback();
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบห้องเรียน' },
            });
        }

        // Generate PIN code
        const pin_code = QueueSession.generatePIN();

        // Create session
        const session = await QueueSession.create(
            {
                course_id: courseId,
                classroom_id,
                title,
                description,
                pin_code,
                linked_assignment_id: linked_assignment_id || null,
                require_attendance: require_attendance || false,
                linked_attendance_session_id: linked_attendance_session_id || null,
                status: 'draft',
                created_by: req.user.id,
            },
            { transaction }
        );

        // Initialize desk statuses
        const desks = await Desk.findAll({
            where: { classroom_id, is_enabled: true },
        });

        const deskStatusRecords = desks.map((desk) => ({
            queue_session_id: session.id,
            desk_id: desk.id,
            grading_status: 'not_started',
            help_status: 'none',
        }));

        await QueueDeskStatus.bulkCreate(deskStatusRecords, { transaction });

        await transaction.commit();

        // Fetch complete session
        const completeSession = await QueueSession.findByPk(session.id, {
            include: [
                {
                    model: Classroom,
                    as: 'classroom',
                },
                {
                    model: Assignment,
                    as: 'linkedAssignment',
                },
            ],
        });

        logCourseActivity({ courseId: courseId, actorUserId: req.user.id, action: 'create_queue_session', category: 'queue', targetType: 'queue_session', targetId: session.id, targetName: title });

        res.status(201).json({
            success: true,
            data: completeSession,
        });
    } catch (error) {
        await transaction.rollback();
        console.error('Error creating queue session:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Update queue session
 */
const updateQueueSession = async (req, res) => {
    try {
        const { sessionId } = req.params;
        const updates = req.body;

        const session = await QueueSession.findByPk(sessionId);
        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        // Don't allow changing certain fields if session is active
        if (session.status === 'active') {
            delete updates.classroom_id;
            delete updates.course_id;
        }

        await session.update(updates);

        logCourseActivity({ courseId: session.course_id, actorUserId: req.user.id, action: 'update_queue_session', category: 'queue', targetType: 'queue_session', targetId: sessionId, targetName: session.title, detail: { fields: Object.keys(updates) } });

        res.json({
            success: true,
            data: session,
        });
    } catch (error) {
        console.error('Error updating queue session:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Update queue session status
 */
const updateQueueSessionStatus = async (req, res) => {
    try {
        const { sessionId } = req.params;
        const { status } = req.body;

        const session = await QueueSession.findByPk(sessionId);
        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        // Validate status transition
        const validTransitions = {
            draft: ['active'],
            active: ['paused', 'closed'],
            paused: ['active', 'closed'],
            closed: [],
        };

        if (!validTransitions[session.status].includes(status)) {
            return res.status(400).json({
                success: false,
                error: {
                    message: `ไม่สามารถเปลี่ยนสถานะจาก ${session.status} เป็น ${status}`,
                },
            });
        }

        // Set start_time when activating
        const updateData = { status };
        if (status === 'active' && !session.start_time) {
            updateData.start_time = new Date();
        }
        if (status === 'closed') {
            updateData.end_time = new Date();
        }

        await session.update(updateData);
        invalidateSessionCache();

        logCourseActivity({ courseId: session.course_id, actorUserId: req.user.id, action: `queue_session_${status}`, category: 'queue', targetType: 'queue_session', targetId: sessionId, targetName: session.title, detail: { status } });

        // Emit socket event for real-time updates
        const io = req.app.get('io');
        if (io) {
            io.to(`queue-${sessionId}`).emit('session-status-changed', {
                sessionId,
                status,
            });
        }

        res.json({
            success: true,
            data: session,
        });
    } catch (error) {
        console.error('Error updating queue session status:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Delete queue session
 */
const deleteQueueSession = async (req, res) => {
    try {
        const { sessionId } = req.params;

        const session = await QueueSession.findByPk(sessionId);
        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        // Can only delete if no pending bookings (waiting/in_progress)
        const pendingCount = await QueueBooking.count({
            where: {
                queue_session_id: sessionId,
                status: { [Op.in]: ['waiting', 'in_progress'] },
            },
        });

        if (pendingCount > 0) {
            return res.status(400).json({
                success: false,
                error: { message: `ยังมีคิวค้างอยู่ ${pendingCount} รายการ ไม่สามารถลบได้` },
            });
        }

        const sessionTitle = session.title;
        const sessionCourseId = session.course_id;
        await session.destroy();

        logCourseActivity({ courseId: sessionCourseId, actorUserId: req.user.id, action: 'delete_queue_session', category: 'queue', targetType: 'queue_session', targetId: sessionId, targetName: sessionTitle });

        res.json({
            success: true,
            message: 'ลบ Queue Session สำเร็จ',
        });
    } catch (error) {
        console.error('Error deleting queue session:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Regenerate PIN code
 */
const regeneratePIN = async (req, res) => {
    try {
        const { sessionId } = req.params;

        const session = await QueueSession.findByPk(sessionId);
        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        const newPIN = QueueSession.generatePIN();
        await session.update({ pin_code: newPIN });

        // Emit socket event
        const io = req.app.get('io');
        if (io) {
            io.to(`queue-${sessionId}`).emit('pin-changed', {
                sessionId,
                pin_code: newPIN,
            });
        }

        res.json({
            success: true,
            data: { pin_code: newPIN },
        });
    } catch (error) {
        console.error('Error regenerating PIN:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

// ============================================
// Worker Management
// ============================================

/**
 * Join as worker
 * REFACTORED: Worker state is now stored in Redis for real-time availability.
 * MySQL is updated for persistence only.
 */
const joinAsWorker = async (req, res) => {
    try {
        const { sessionId } = req.params;
        const { accept_grading, accept_help } = req.body;

        const session = await QueueSession.findByPk(sessionId);
        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        // Check if session is active or paused (workers can join to handle existing bookings)
        if (session.status !== 'active' && session.status !== 'paused') {
            return res.status(400).json({
                success: false,
                error: { message: 'Session ยังไม่เปิดใช้งาน' },
            });
        }

        // [MySQL] Create or update worker record for persistence
        const [worker, created] = await QueueWorker.findOrCreate({
            where: {
                queue_session_id: sessionId,
                user_id: req.user.id,
            },
            defaults: {
                accept_grading: accept_grading !== false,
                accept_help: accept_help !== false,
                status: 'online',
                last_active_at: new Date(),
            },
        });

        if (!created) {
            await worker.update({
                accept_grading: accept_grading !== false,
                accept_help: accept_help !== false,
                status: 'online',
                last_active_at: new Date(),
            });
        }

        // [Redis] Set worker state for real-time availability
        await redisQueue.setWorkerState(sessionId, req.user.id, {
            status: 'online',
            acceptGrading: accept_grading !== false,
            acceptHelp: accept_help !== false,
            completedGrading: worker.total_grading_completed || 0,
            completedHelp: worker.total_help_completed || 0,
        });

        // Emit socket event
        const io = req.app.get('io');
        
        if (io) {
            io.to(`queue-${sessionId}`).emit('worker-joined', {
                workerId: worker.id,
                userId: req.user.id,
                userName: req.user.full_name,
            });
        }

        // [Background Worker] Trigger assignment check (non-blocking)
        // This replaces the old tryAssignWaitingBookingToWorker call
        triggerAssignmentForSession(sessionId).catch(err => {
            logger.error('Error triggering assignment:', err);
        });

        res.json({
            success: true,
            data: worker,
            // Note: assignedBooking is now handled asynchronously via socket
        });
    } catch (error) {
        console.error('Error joining as worker:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Leave as worker (go offline)
 * REFACTORED: Updates Redis state first for immediate effect,
 * then persists to MySQL asynchronously.
 */
const leaveAsWorker = async (req, res) => {
    try {
        const { sessionId } = req.params;

        // [Redis] Check current worker state
        const redisWorker = await redisQueue.getWorkerState(sessionId, req.user.id);
        
        // [MySQL] Also check for persistence and current_booking_id
        const worker = await QueueWorker.findOne({
            where: {
                queue_session_id: sessionId,
                user_id: req.user.id,
            },
        });

        if (!worker && !redisWorker) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบข้อมูล Worker' },
            });
        }

        // Check if worker has active booking (from Redis or MySQL)
        const hasActiveBooking = redisWorker?.currentBookingId || worker?.current_booking_id;
        
        if (hasActiveBooking) {
            // [Redis] Set to paused - worker will complete current task then go offline
            await redisQueue.setWorkerPaused(sessionId, req.user.id);
            
            // [MySQL] Update for persistence
            if (worker) {
                await worker.update({ status: 'paused' });
            }

            // Emit socket event
            const io = req.app.get('io');
            if (io) {
                io.to(`queue-${sessionId}`).emit('worker-paused', {
                    workerId: worker?.id,
                    userId: req.user.id,
                });
            }

            return res.json({
                success: true,
                message: 'หยุดรับงานใหม่แล้ว กรุณาทำงานปัจจุบันให้เสร็จ',
                data: { status: 'paused' },
            });
        }

        // [Redis] Set to offline
        await redisQueue.setWorkerOffline(sessionId, req.user.id);
        
        // [MySQL] Update for persistence
        if (worker) {
            await worker.update({ status: 'offline' });
        }

        // Emit socket event
        const io = req.app.get('io');
        if (io) {
            io.to(`queue-${sessionId}`).emit('worker-left', {
                workerId: worker?.id,
                userId: req.user.id,
            });
        }

        res.json({
            success: true,
            message: 'ออกจากการรับงานสำเร็จ',
        });
    } catch (error) {
        console.error('Error leaving as worker:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Get online workers
 */
const getWorkers = async (req, res) => {
    try {
        const { sessionId } = req.params;

        const workers = await QueueWorker.findAll({
            where: { queue_session_id: sessionId },
            include: [
                {
                    model: User,
                    as: 'user',
                    attributes: ['id', 'full_name', 'avatar', 'role'],
                },
            ],
            order: [['status', 'ASC'], ['last_active_at', 'DESC']],
        });

        res.json({
            success: true,
            data: workers,
        });
    } catch (error) {
        console.error('Error getting workers:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Get worker's current booking (for reconnection)
 */
const getWorkerCurrentBooking = async (req, res) => {
    try {
        const { sessionId } = req.params;
        const userId = req.user.id;

        logger.debug(`getWorkerCurrentBooking: sessionId=${sessionId}, userId=${userId}`);

        // Find worker record (may or may not exist)
        const worker = await QueueWorker.findOne({
            where: {
                queue_session_id: sessionId,
                user_id: userId,
            },
        });

        logger.debug(`Worker found: ${worker ? worker.id : 'null'}, status: ${worker?.status}`);

        // Find current in-progress booking assigned to this worker
        // This is independent of worker status - booking may still be in_progress
        const currentBooking = await QueueBooking.findOne({
            where: {
                queue_session_id: sessionId,
                assigned_worker_id: userId,
                status: 'in_progress',
            },
            include: [
                { model: Student, as: 'student' },
                { model: Desk, as: 'desk' },
            ],
        });

        logger.debug(`Current booking found: ${currentBooking ? currentBooking.id : 'null'}`);

        // If there's a pending booking but worker is offline (not paused), re-set worker to online
        // Don't change paused workers - they explicitly want to stop after current task
        if (currentBooking && worker && worker.status === 'offline') {
            await worker.update({
                status: 'online',
                current_booking_id: currentBooking.id,
                last_active_at: new Date(),
            });
            logger.debug(`Worker status updated to online`);
        }

        // Enrich booking with zone info
        let enrichedBooking = currentBooking;
        if (currentBooking) {
            const session = await QueueSession.findByPk(sessionId, { attributes: ['classroom_id'] });
            enrichedBooking = await enrichBookingWithZone(currentBooking, session?.classroom_id);
        }

        res.json({
            success: true,
            data: {
                worker: worker,
                currentBooking: enrichedBooking,
            },
        });
    } catch (error) {
        console.error('Error getting worker current booking:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

// ============================================
// Booking Management (Student)
// ============================================

/**
 * Create booking (Student)
 */
const createBooking = async (req, res) => {
    const transaction = await sequelize.transaction();

    try {
        const { pin_code, student_id, desk_number, booking_type, note } = req.body;

        // Find session by PIN
        const session = await QueueSession.findOne({
            where: { pin_code, status: { [Op.in]: ['active', 'paused'] } },
            include: [
                {
                    model: Classroom,
                    as: 'classroom',
                    include: [{ model: Desk, as: 'desks' }],
                },
            ],
        });

        if (!session) {
            await transaction.rollback();
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบการจองคิวที่เปิดอยู่ หรือ PIN ไม่ถูกต้อง' },
            });
        }

        // If session is paused, reject new bookings
        if (session.status === 'paused') {
            await transaction.rollback();
            return res.status(400).json({
                success: false,
                error: { 
                    message: 'ปิดรับการจองคิวชั่วคราว กรุณารอสักครู่',
                    code: 'SESSION_PAUSED',
                },
            });
        }

        // Find student
        const student = await Student.findOne({
            where: { student_id },
        });

        if (!student) {
            await transaction.rollback();
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบรหัสนักศึกษานี้ในระบบ' },
            });
        }

        // Check attendance requirement
        if (session.require_attendance && session.linked_attendance_session_id) {
            const attendanceRecord = await AttendanceRecord.findOne({
                where: {
                    attendance_session_id: session.linked_attendance_session_id,
                    student_id: student.id,
                    status: { [Op.in]: ['present', 'late'] },
                },
            });

            if (!attendanceRecord) {
                await transaction.rollback();
                return res.status(400).json({
                    success: false,
                    error: { message: 'กรุณาเช็คชื่อก่อนจองคิว' },
                });
            }
        }

        // Check if grading and session has linked assignment - validate score completion
        if (booking_type === 'grading' && session.linked_assignment_id) {
            const assignment = await Assignment.findOne({
                where: { id: session.linked_assignment_id },
                include: [{ model: AssignmentSubItem, as: 'subItems' }],
            });

            if (assignment) {
                const hasSubItems = assignment.subItems && assignment.subItems.length > 0;

                if (hasSubItems) {
                    // Assignment has sub-items - check how many are already graded
                    const gradedSubItems = await Score.findAll({
                        where: {
                            assignment_id: assignment.id,
                            student_id: student.id,
                            sub_item_id: { [Op.ne]: null },
                            status: 'graded',
                        },
                    });

                    const gradedSubItemIds = gradedSubItems.map(s => s.sub_item_id);
                    const allSubItemIds = assignment.subItems.map(s => s.id);
                    const allGraded = allSubItemIds.every(id => gradedSubItemIds.includes(id));

                    if (allGraded) {
                        await transaction.rollback();
                        return res.status(400).json({
                            success: false,
                            error: { message: 'คุณได้รับการตรวจครบทุกข้อแล้ว ไม่สามารถจองคิวตรวจงานได้อีก' },
                        });
                    }
                } else {
                    // Single score assignment - check if already graded
                    const existingScore = await Score.findOne({
                        where: {
                            assignment_id: assignment.id,
                            student_id: student.id,
                            sub_item_id: null,
                            status: 'graded',
                        },
                    });

                    if (existingScore) {
                        await transaction.rollback();
                        return res.status(400).json({
                            success: false,
                            error: { message: 'คุณได้รับการตรวจงานแล้ว ไม่สามารถจองคิวตรวจงานได้อีก' },
                        });
                    }
                }
            }
        }

        // Check if student already has an active booking in this session
        const existingActiveBooking = await QueueBooking.findOne({
            where: {
                queue_session_id: session.id,
                student_id: student.id,
                status: { [Op.in]: ['waiting', 'in_progress'] },
            },
            transaction,
        });

        if (existingActiveBooking) {
            await transaction.rollback();
            return res.status(400).json({
                success: false,
                error: { message: 'คุณมีคิวที่รออยู่แล้ว กรุณารอให้ TA ดำเนินการเสร็จก่อน' },
            });
        }

        // Find desk (exclude teacher desks - students cannot book teacher desks)
        const desk = await Desk.findOne({
            where: {
                classroom_id: session.classroom_id,
                number: desk_number,
                is_enabled: true,
                type: { [Op.ne]: 'teacher' }, // Exclude teacher desks from student booking
            },
        });

        if (!desk) {
            await transaction.rollback();
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบโต๊ะหมายเลขนี้ (โต๊ะอาจารย์ไม่สามารถจองได้)' },
            });
        }

        // Check desk status
        let deskStatus = await QueueDeskStatus.findOne({
            where: {
                queue_session_id: session.id,
                desk_id: desk.id,
            },
            transaction,
        });

        // If grading type, check desk completion status based on assignment type
        if (booking_type === 'grading') {
            // First check if desk has active booking (waiting/in_progress)
            if (deskStatus && ['waiting', 'in_progress'].includes(deskStatus.grading_status)) {
                await transaction.rollback();
                return res.status(400).json({
                    success: false,
                    error: { message: 'โต๊ะนี้มีการจองตรวจงานอยู่แล้ว' },
                });
            }

            // Check desk completion based on assignment type
            if (session.linked_assignment_id) {
                const assignment = await Assignment.findOne({
                    where: { id: session.linked_assignment_id },
                    include: [{ model: AssignmentSubItem, as: 'subItems' }],
                });

                if (assignment) {
                    const hasSubItems = assignment.subItems && assignment.subItems.length > 0;

                    // Find students at this desk (from student groups or individual)
                    // We'll check scores for this specific desk using previous bookings
                    const deskBookings = await QueueBooking.findAll({
                        where: {
                            queue_session_id: session.id,
                            desk_id: desk.id,
                            booking_type: 'grading',
                            status: 'completed',
                        },
                        attributes: ['student_id'],
                        group: ['student_id'],
                    });

                    if (deskBookings.length > 0) {
                        const studentIds = deskBookings.map(b => b.student_id);

                        if (hasSubItems) {
                            // Check if all students have all sub-items graded
                            const allSubItemIds = assignment.subItems.map(s => s.id);
                            
                            let deskFullyGraded = true;
                            for (const sid of studentIds) {
                                const gradedSubItems = await Score.findAll({
                                    where: {
                                        assignment_id: assignment.id,
                                        student_id: sid,
                                        sub_item_id: { [Op.ne]: null },
                                        status: 'graded',
                                    },
                                });
                                const gradedIds = gradedSubItems.map(s => s.sub_item_id);
                                const allGraded = allSubItemIds.every(id => gradedIds.includes(id));
                                if (!allGraded) {
                                    deskFullyGraded = false;
                                    break;
                                }
                            }

                            // Only block if desk is fully graded
                            if (deskFullyGraded && deskStatus && deskStatus.grading_status === 'completed') {
                                await transaction.rollback();
                                return res.status(400).json({
                                    success: false,
                                    error: { message: 'โต๊ะนี้ได้รับการตรวจครบทุกข้อแล้ว' },
                                });
                            }
                        } else {
                            // Single score - if completed, desk is done
                            if (deskStatus && deskStatus.grading_status === 'completed') {
                                await transaction.rollback();
                                return res.status(400).json({
                                    success: false,
                                    error: { message: 'โต๊ะนี้ได้รับการตรวจแล้ว' },
                                });
                            }
                        }
                    }
                }
            } else {
                // No linked assignment - use simple desk status
                if (deskStatus && deskStatus.grading_status === 'completed') {
                    await transaction.rollback();
                    return res.status(400).json({
                        success: false,
                        error: { message: 'โต๊ะนี้ได้รับการตรวจแล้ว' },
                    });
                }
            }
        }

        // Get next queue number (global sequence across all booking types)
        const lastBooking = await QueueBooking.findOne({
            where: {
                queue_session_id: session.id,
            },
            order: [['queue_number', 'DESC']],
            transaction,
        });

        const queueNumber = lastBooking ? lastBooking.queue_number + 1 : 1;

        // Create booking
        const booking = await QueueBooking.create(
            {
                queue_session_id: session.id,
                student_id: student.id,
                desk_id: desk.id,
                desk_number,
                booking_type,
                queue_number: queueNumber,
                note,
                status: 'waiting',
            },
            { transaction }
        );

        // Update desk status
        if (!deskStatus) {
            deskStatus = await QueueDeskStatus.create(
                {
                    queue_session_id: session.id,
                    desk_id: desk.id,
                },
                { transaction }
            );
        }

        if (booking_type === 'grading') {
            await deskStatus.update(
                {
                    grading_status: 'waiting',
                    grading_booking_id: booking.id,
                },
                { transaction }
            );
        } else {
            await deskStatus.update(
                {
                    help_status: 'waiting',
                    help_booking_id: booking.id,
                },
                { transaction }
            );
        }

        await transaction.commit();

        logger.debug(`Booking created: id=${booking.id}, session=${session.id}`);

        // [Redis] Add booking to waiting queue for background assignment
        // This replaces the direct tryAssignBooking call
        await redisQueue.addBookingToQueue(session.id, {
            id: booking.id,
            studentId: student.id,
            deskId: desk.id,
            deskNumber: desk_number,
            bookingType: booking_type,
            queueNumber: queueNumber,
            note: note,
            studentInfo: {
                id: student.id,
                studentId: student.student_id,
                fullName: student.full_name,
            },
        });

        // [Redis] Update desk status
        await redisQueue.setDeskStatus(session.id, desk.id, {
            [booking_type === 'grading' ? 'gradingStatus' : 'helpStatus']: 'waiting',
            [booking_type === 'grading' ? 'gradingBookingId' : 'helpBookingId']: booking.id.toString(),
        });

        // Emit socket event for real-time updates
        const io = req.app.get('io');
        logger.debug(`Socket.io instance: ${io ? 'available' : 'NOT AVAILABLE'}`);
        
        if (io) {
            io.to(`queue-${session.id}`).emit('new-booking', {
                booking: {
                    ...booking.toJSON(),
                    student: {
                        id: student.id,
                        student_id: student.student_id,
                        full_name: student.full_name,
                    },
                },
            });
        }

        // [Background Worker] Trigger assignment check (non-blocking)
        // The background worker will handle assignment asynchronously
        triggerAssignmentForSession(session.id).catch(err => {
            logger.error('Error triggering assignment:', err);
        });

        res.status(201).json({
            success: true,
            data: {
                ...booking.toJSON(),
                queue_number: queueNumber,
                session_title: session.title,
            },
        });
    } catch (error) {
        await transaction.rollback();
        console.error('Error creating booking:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Try to assign a booking to an available worker
 */
const tryAssignBooking = async (sessionId, bookingId, io) => {
    try {
        logger.info(`tryAssignBooking: sessionId=${sessionId}, bookingId=${bookingId}`);
        
        const booking = await QueueBooking.findByPk(bookingId);
        if (!booking || booking.status !== 'waiting') {
            logger.info(`Booking not found or not waiting: status=${booking?.status}`);
            return;
        }

        // Find available workers
        const workerCondition = {
            queue_session_id: sessionId,
            status: 'online',
        };

        if (booking.booking_type === 'grading') {
            workerCondition.accept_grading = true;
        } else {
            workerCondition.accept_help = true;
        }

        logger.info(`Looking for workers with condition: ${JSON.stringify(workerCondition)}`);

        const availableWorker = await QueueWorker.findOne({
            where: workerCondition,
            order: [
                // Prioritize worker with least completed tasks
                [
                    booking.booking_type === 'grading'
                        ? 'total_grading_completed'
                        : 'total_help_completed',
                    'ASC',
                ],
                ['last_active_at', 'ASC'],
            ],
        });

        if (availableWorker) {
            logger.info(`Found available worker: user_id=${availableWorker.user_id}, status=${availableWorker.status}`);
            
            // Double-check worker is still online (in case they left between finding and now)
            const freshWorker = await QueueWorker.findByPk(availableWorker.id);
            if (!freshWorker || freshWorker.status !== 'online') {
                logger.info(`Worker ${availableWorker.user_id} is no longer online (status: ${freshWorker?.status}), skipping assignment`);
                return;
            }
            
            // Assign booking to worker
            await booking.update({
                assigned_worker_id: availableWorker.user_id,
                assigned_at: new Date(),
                status: 'in_progress',
                started_at: new Date(),
            });

            await freshWorker.update({
                status: 'busy',
                current_booking_id: booking.id,
            });

            // Update desk status
            const deskStatus = await QueueDeskStatus.findOne({
                where: {
                    queue_session_id: sessionId,
                    desk_id: booking.desk_id,
                },
            });

            if (deskStatus) {
                if (booking.booking_type === 'grading') {
                    await deskStatus.update({ grading_status: 'in_progress' });
                } else {
                    await deskStatus.update({ help_status: 'in_progress' });
                }
            }

            // Emit assignment event
            if (io) {
                const fullBooking = await QueueBooking.findByPk(bookingId, {
                    include: [
                        { model: Student, as: 'student' },
                        { model: Desk, as: 'desk' },
                    ],
                });

                // Enrich with zone info
                const session = await QueueSession.findByPk(sessionId, { attributes: ['classroom_id'] });
                const enrichedBooking = await enrichBookingWithZone(fullBooking, session?.classroom_id);

                io.to(`queue-${sessionId}`).emit('booking-assigned', {
                    booking: enrichedBooking,
                    workerId: availableWorker.user_id,
                });

                // Emit to specific worker (ensure string for room name)
                const workerRoom = `worker-${String(availableWorker.user_id)}`;
                logger.info(`Emitting new-task to room: ${workerRoom}, booking: ${bookingId}`);
                
                // Check how many sockets are in the room
                const roomSockets = io.sockets.adapter.rooms.get(workerRoom);
                logger.info(`Sockets in room ${workerRoom}: ${roomSockets ? roomSockets.size : 0}`);
                
                io.to(workerRoom).emit('new-task', {
                    booking: enrichedBooking,
                });
                logger.info(`new-task event emitted successfully to room ${workerRoom}`);

                // Send FCM push notification to worker
                fcmService.notifyWorkerNewTask(
                    availableWorker.user_id,
                    sessionId,
                    fullBooking
                ).catch(err => logger.error('FCM worker notification error:', err));
            }
        } else {
            logger.info(`No available worker found for session ${sessionId}`);
        }
    } catch (error) {
        logger.error('Error assigning booking:', error);
    }
};

/**
 * Try to assign waiting bookings to a newly joined worker
 * Returns the assigned booking if found
 */
const tryAssignWaitingBookingToWorker = async (sessionId, userId, worker, io) => {
    try {
        logger.debug(`Checking for waiting bookings for new worker ${userId}`);

        // Build booking type condition based on worker preferences
        const bookingTypes = [];
        if (worker.accept_grading) bookingTypes.push('grading');
        if (worker.accept_help) bookingTypes.push('help');

        if (bookingTypes.length === 0) {
            logger.debug(`Worker ${userId} doesn't accept any booking types`);
            return null;
        }

        // Find oldest waiting booking that matches worker preferences
        const waitingBooking = await QueueBooking.findOne({
            where: {
                queue_session_id: sessionId,
                status: 'waiting',
                booking_type: { [Op.in]: bookingTypes },
            },
            order: [['created_at', 'ASC']], // Oldest first (FIFO)
            include: [
                { model: Student, as: 'student' },
                { model: Desk, as: 'desk' },
            ],
        });

        if (!waitingBooking) {
            logger.debug(`No waiting bookings found for session ${sessionId}`);
            return null;
        }

        logger.debug(`Found waiting booking ${waitingBooking.id}, assigning to worker ${userId}`);

        // Assign booking to worker
        await waitingBooking.update({
            assigned_worker_id: userId,
            assigned_at: new Date(),
            status: 'in_progress',
            started_at: new Date(),
        });

        await worker.update({
            status: 'busy',
            current_booking_id: waitingBooking.id,
        });

        // Update desk status
        const deskStatus = await QueueDeskStatus.findOne({
            where: {
                queue_session_id: sessionId,
                desk_id: waitingBooking.desk_id,
            },
        });

        if (deskStatus) {
            if (waitingBooking.booking_type === 'grading') {
                await deskStatus.update({ grading_status: 'in_progress' });
            } else {
                await deskStatus.update({ help_status: 'in_progress' });
            }
        }

        // Emit assignment events to projector/queue room
        io.to(`queue-${sessionId}`).emit('booking-assigned', {
            booking: waitingBooking,
            workerId: userId,
        });

        // Emit to student's booking room
        io.to(`booking-${waitingBooking.id}`).emit('booking-assigned', {
            booking: waitingBooking,
        });

        // Send FCM push notification to student (Your Turn)
        const workerUser = await User.findByPk(userId, {
            attributes: ['id', 'full_name'],
        });
        fcmService.notifyStudentYourTurn(waitingBooking, workerUser || { full_name: 'ผู้ช่วยสอน' })
            .catch(err => logger.error('FCM student notification error:', err));

        // Enrich with zone info before returning
        const sessionForZone = await QueueSession.findByPk(sessionId, { attributes: ['classroom_id'] });
        const enrichedBooking = await enrichBookingWithZone(waitingBooking, sessionForZone?.classroom_id);

        // Return the assigned booking (socket emit to worker will be handled by response)
        return enrichedBooking;

    } catch (error) {
        logger.error('Error assigning waiting booking to worker:', error);
        return null;
    }
};

/**
 * Get booking status (Student view)
 */
const getBookingStatus = async (req, res) => {
    try {
        const { bookingId } = req.params;

        const booking = await QueueBooking.findByPk(bookingId, {
            include: [
                {
                    model: QueueSession,
                    as: 'session',
                    attributes: ['id', 'title', 'status', 'linked_assignment_id', 'classroom_id'],
                    include: [
                        {
                            model: Assignment,
                            as: 'linkedAssignment',
                            attributes: ['id', 'name', 'max_score'],
                            include: [
                                {
                                    model: AssignmentSubItem,
                                    as: 'subItems',
                                    attributes: ['id', 'name', 'max_score', 'order_index'],
                                }
                            ]
                        }
                    ]
                },
                {
                    model: Student,
                    as: 'student',
                    attributes: ['id', 'student_id', 'full_name'],
                },
                {
                    model: Desk,
                    as: 'desk',
                    attributes: ['id', 'number', 'x', 'y'],
                },
                {
                    model: User,
                    as: 'assignedWorker',
                    attributes: ['id', 'full_name'],
                },
            ],
        });

        if (!booking) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบข้อมูลการจอง' },
            });
        }

        // Get position in queue (count all waiting bookings created before this one)
        const position = await QueueBooking.count({
            where: {
                queue_session_id: booking.queue_session_id,
                status: 'waiting',
                created_at: { [Op.lt]: booking.created_at },
            },
        });

        // If booking is completed and has linked assignment, get scores
        let scoreDetails = null;
        if (booking.status === 'completed' && booking.session?.linked_assignment_id) {
            logger.info(`getBookingStatus: Fetching scores for completed booking ${bookingId}, assignment ${booking.session.linked_assignment_id}`);
            const assignment = booking.session.linkedAssignment;
            const hasSubItems = assignment?.subItems && assignment.subItems.length > 0;

            if (hasSubItems) {
                // Get sub-item scores
                const subItemScores = await Score.findAll({
                    where: {
                        assignment_id: assignment.id,
                        student_id: booking.student_id,
                        sub_item_id: { [Op.ne]: null },
                    },
                    include: [
                        {
                            model: User,
                            as: 'grader',
                            attributes: ['id', 'full_name'],
                        },
                        {
                            model: AssignmentSubItem,
                            as: 'subItem',
                            attributes: ['id', 'name', 'max_score'],
                        }
                    ],
                });

                scoreDetails = {
                    type: 'sub_items',
                    assignment_name: assignment.name,
                    sub_items: subItemScores.map(s => ({
                        id: s.sub_item_id,
                        name: s.subItem?.name,
                        score: s.score,
                        max_score: s.subItem?.max_score,
                        graded_by: s.grader?.full_name,
                        graded_at: s.graded_at,
                    })),
                    total_score: subItemScores.reduce((sum, s) => sum + (parseFloat(s.score) || 0), 0),
                    total_max_score: assignment.subItems.reduce((sum, s) => sum + (parseFloat(s.max_score) || 0), 0),
                };
            } else {
                // Get single score
                const singleScore = await Score.findOne({
                    where: {
                        assignment_id: assignment.id,
                        student_id: booking.student_id,
                        sub_item_id: null,
                    },
                    include: [
                        {
                            model: User,
                            as: 'grader',
                            attributes: ['id', 'full_name'],
                        }
                    ],
                });

                if (singleScore) {
                    scoreDetails = {
                        type: 'single',
                        assignment_name: assignment.name,
                        score: singleScore.score,
                        max_score: assignment.max_score,
                        graded_by: singleScore.grader?.full_name,
                        graded_at: singleScore.graded_at,
                        comment: singleScore.comment,
                    };
                    logger.info(`getBookingStatus: Found single score: ${singleScore.score}/${assignment.max_score}`);
                } else {
                    logger.info(`getBookingStatus: No single score found for student ${booking.student_id}`);
                }
            }
        }

        logger.info(`getBookingStatus: Returning score_details: ${scoreDetails ? JSON.stringify(scoreDetails) : 'null'}`);

        // Enrich with zone info
        const bookingPlain = booking.toJSON();
        if (bookingPlain.desk && booking.session?.classroom_id) {
            const zone = await findZoneForDesk(bookingPlain.desk, booking.session.classroom_id);
            bookingPlain.zone = zone;
        } else {
            bookingPlain.zone = null;
        }

        res.json({
            success: true,
            data: {
                ...bookingPlain,
                position_in_queue: position + 1,
                score_details: scoreDetails,
            },
        });
    } catch (error) {
        console.error('Error getting booking status:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Cancel booking (Student)
 * นักศึกษาสามารถยกเลิกการจองได้เฉพาะเมื่อ status เป็น 'waiting' เท่านั้น
 * 
 * Fix: Use SELECT FOR UPDATE to prevent race condition with background assignment worker
 * Key: Remove from Redis BEFORE MySQL commit to prevent assignment during cancel
 */
const cancelBooking = async (req, res) => {
    const transaction = await sequelize.transaction();

    try {
        const { bookingId } = req.params;

        // [FIX] Use SELECT FOR UPDATE to lock the row during transaction
        const booking = await QueueBooking.findByPk(bookingId, {
            include: [
                {
                    model: QueueSession,
                    as: 'session',
                    attributes: ['id', 'title', 'status'],
                },
                {
                    model: Desk,
                    as: 'desk',
                    attributes: ['id', 'number'],
                },
            ],
            transaction,
            lock: transaction.LOCK.UPDATE, // Prevent concurrent modifications
        });

        if (!booking) {
            await transaction.rollback();
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบข้อมูลการจอง' },
            });
        }

        // Only allow cancellation if status is 'waiting'
        if (booking.status !== 'waiting') {
            await transaction.rollback();
            return res.status(400).json({
                success: false,
                error: { message: 'ไม่สามารถยกเลิกได้ เนื่องจากถึงคิวแล้วหรือดำเนินการเสร็จสิ้นแล้ว' },
            });
        }

        // [FIX] Remove from Redis BEFORE MySQL commit to prevent assignment worker from picking it up
        // This is crucial to prevent race: remove from queue first, then update MySQL
        await redisQueue.removeBookingFromQueue(booking.queue_session_id, booking.id, booking.booking_type);
        
        // [FIX] Update Redis desk status immediately (before MySQL)
        if (booking.desk_id) {
            await redisQueue.setDeskStatus(booking.queue_session_id, booking.desk_id, {
                [booking.booking_type === 'grading' ? 'gradingStatus' : 'helpStatus']: booking.booking_type === 'grading' ? 'not_started' : 'none',
                [booking.booking_type === 'grading' ? 'gradingBookingId' : 'helpBookingId']: '',
            });
        }
        
        // Update booking status to cancelled
        await booking.update(
            {
                status: 'cancelled',
                completed_at: new Date(),
            },
            { transaction }
        );

        // If booking had an assigned desk, reset the desk status in QueueDeskStatus (MySQL)
        if (booking.desk_id) {
            const deskStatus = await QueueDeskStatus.findOne({
                where: {
                    queue_session_id: booking.queue_session_id,
                    desk_id: booking.desk_id,
                },
                transaction,
            });

            if (deskStatus) {
                if (booking.booking_type === 'grading') {
                    await deskStatus.update({ 
                        grading_status: 'not_started',
                        grading_booking_id: null,
                    }, { transaction });
                } else {
                    await deskStatus.update({ 
                        help_status: 'none',
                        help_booking_id: null,
                    }, { transaction });
                }
            }
        }

        await transaction.commit();

        // Emit socket event for real-time update
        const io = req.app.get('io');
        if (io) {
            // Notify queue room (projector/dashboard)
            io.to(`queue-${booking.queue_session_id}`).emit('booking-cancelled', {
                bookingId: booking.id,
                queueNumber: booking.queue_number,
                bookingType: booking.booking_type,
                deskId: booking.desk_id,
            });

            // [FIX] Also notify the specific booking room so student/TA gets update
            io.to(`booking-${booking.id}`).emit('booking-cancelled', {
                bookingId: booking.id,
                queueNumber: booking.queue_number,
                bookingType: booking.booking_type,
                deskId: booking.desk_id,
            });

            // [FIX] If booking was assigned to a worker, notify them to refresh
            if (booking.assigned_worker_id) {
                io.to(`worker-${booking.assigned_worker_id}`).emit('booking-cancelled', {
                    bookingId: booking.id,
                    queueNumber: booking.queue_number,
                });
            }
        }

        logger.info(`Booking ${bookingId} cancelled by student/admin`);

        res.json({
            success: true,
            message: 'ยกเลิกการจองสำเร็จ',
            data: booking.toJSON(),
        });
    } catch (error) {
        await transaction.rollback();
        console.error('Error cancelling booking:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Complete booking (Worker)
 */
const completeBooking = async (req, res) => {
    const transaction = await sequelize.transaction();

    try {
        const { bookingId } = req.params;
        const { score, sub_item_scores, score_comment, worker_note } = req.body;

        const booking = await QueueBooking.findByPk(bookingId, {
            include: [
                { 
                    model: QueueSession, 
                    as: 'session',
                    include: [
                        {
                            model: Assignment,
                            as: 'linkedAssignment',
                            include: [
                                { model: AssignmentSubItem, as: 'subItems' }
                            ]
                        }
                    ]
                },
                { model: Student, as: 'student' },
            ],
            transaction,
        });

        if (!booking) {
            await transaction.rollback();
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบข้อมูลการจอง' },
            });
        }

        // Verify worker
        if (booking.assigned_worker_id !== req.user.id) {
            await transaction.rollback();
            return res.status(403).json({
                success: false,
                error: { message: 'คุณไม่ใช่ผู้รับงานนี้' },
            });
        }

        // Check if assignment has sub-items
        const linkedAssignment = booking.session?.linkedAssignment;
        const assignmentSubItems = linkedAssignment?.subItems || [];
        const hasSubItems = assignmentSubItems.length > 0;

        // Update booking
        await booking.update(
            {
                status: 'completed',
                completed_at: new Date(),
                score,
                score_comment,
                worker_note,
            },
            { transaction }
        );

        // Determine if all sub-items are scored for this student
        let allSubItemsScored = true;
        if (hasSubItems && booking.booking_type === 'grading') {
            // Get all existing scores for this student on this assignment
            const existingScores = await Score.findAll({
                where: {
                    assignment_id: booking.session.linked_assignment_id,
                    student_id: booking.student_id,
                    sub_item_id: { [Op.ne]: null }, // Only sub-item scores
                },
                transaction,
            });

            // Count how many sub-items will be scored after this submission
            const scoredSubItemIds = new Set(existingScores.map(s => s.sub_item_id));
            
            // Add the new sub-item scores from this submission
            if (sub_item_scores && Array.isArray(sub_item_scores)) {
                sub_item_scores.forEach(s => scoredSubItemIds.add(s.sub_item_id));
            }

            // Check if all sub-items are scored
            allSubItemsScored = scoredSubItemIds.size >= assignmentSubItems.length;
            
            logger.debug(`Sub-items check: ${scoredSubItemIds.size}/${assignmentSubItems.length} scored, allScored: ${allSubItemsScored}`);
        }

        // Update desk status
        const deskStatus = await QueueDeskStatus.findOne({
            where: {
                queue_session_id: booking.queue_session_id,
                desk_id: booking.desk_id,
            },
            transaction,
        });

        if (deskStatus) {
            if (booking.booking_type === 'grading') {
                // For grading: only mark as completed if all sub-items are scored
                // If not all scored, reset to not_started so student can book again
                const newGradingStatus = (hasSubItems && !allSubItemsScored) ? 'not_started' : 'completed';
                
                await deskStatus.update(
                    {
                        grading_status: newGradingStatus,
                        grading_booking_id: null,
                    },
                    { transaction }
                );
                
                logger.debug(`Desk ${booking.desk_number} grading_status set to: ${newGradingStatus}`);
            } else {
                await deskStatus.update(
                    {
                        help_status: 'none',
                        help_booking_id: null,
                    },
                    { transaction }
                );
            }
        }

        // Update worker stats and status
        const worker = await QueueWorker.findOne({
            where: {
                queue_session_id: booking.queue_session_id,
                user_id: req.user.id,
            },
            transaction,
        });

        if (worker) {
            // If worker was paused, set to offline. Otherwise set to online.
            const newStatus = worker.status === 'paused' ? 'offline' : 'online';
            
            const updateData = {
                status: newStatus,
                current_booking_id: null,
                last_active_at: new Date(),
            };

            if (booking.booking_type === 'grading') {
                updateData.total_grading_completed = worker.total_grading_completed + 1;
            } else {
                updateData.total_help_completed = worker.total_help_completed + 1;
            }

            await worker.update(updateData, { transaction });
            
            // [Redis] Update worker state
            // Check current Redis state to handle pause correctly
            const redisWorker = await redisQueue.getWorkerState(booking.queue_session_id, req.user.id);
            const wasPaused = redisWorker?.status === 'paused' || worker.status === 'paused';
            
            if (wasPaused) {
                await redisQueue.setWorkerOffline(booking.queue_session_id, req.user.id);
            } else {
                await redisQueue.setWorkerOnline(booking.queue_session_id, req.user.id);
            }
            
            // [Redis] Increment completion count
            await redisQueue.incrementWorkerCompletion(booking.queue_session_id, req.user.id, booking.booking_type);
        }

        // Save score to Score table if assignment is linked
        if (
            booking.booking_type === 'grading' &&
            booking.session.linked_assignment_id
        ) {
            // Check if we have sub-item scores
            if (sub_item_scores && Array.isArray(sub_item_scores) && sub_item_scores.length > 0) {
                // Save each sub-item score
                for (const subItemScore of sub_item_scores) {
                    const whereClause = {
                        assignment_id: booking.session.linked_assignment_id,
                        student_id: booking.student_id,
                        sub_item_id: subItemScore.sub_item_id,
                    };

                    const [scoreRecord, created] = await Score.findOrCreate({
                        where: whereClause,
                        defaults: {
                            score: subItemScore.score,
                            comment: score_comment,
                            graded_by: req.user.id,
                            graded_at: new Date(),
                            status: 'graded',
                        },
                        transaction,
                    });

                    if (!created) {
                        await scoreRecord.update({
                            score: subItemScore.score,
                            comment: score_comment,
                            graded_by: req.user.id,
                            graded_at: new Date(),
                            status: 'graded',
                        }, { transaction });
                    }
                }
            } else if (score !== undefined && score !== null) {
                // Save single score (no sub-items)
                const whereClause = {
                    assignment_id: booking.session.linked_assignment_id,
                    student_id: booking.student_id,
                    sub_item_id: null,
                };

                const [scoreRecord, created] = await Score.findOrCreate({
                    where: whereClause,
                    defaults: {
                        score,
                        comment: score_comment,
                        graded_by: req.user.id,
                        graded_at: new Date(),
                        status: 'graded',
                    },
                    transaction,
                });

                if (!created) {
                    await scoreRecord.update({
                        score,
                        comment: score_comment,
                        graded_by: req.user.id,
                        graded_at: new Date(),
                        status: 'graded',
                    }, { transaction });
                }
            }
        }

        await transaction.commit();

        // [Redis] Update desk status
        const newDeskStatus = booking.booking_type === 'grading'
            ? ((hasSubItems && !allSubItemsScored) ? 'not_started' : 'completed')
            : 'none';
        await redisQueue.setDeskStatus(booking.queue_session_id, booking.desk_id, {
            [booking.booking_type === 'grading' ? 'gradingStatus' : 'helpStatus']: newDeskStatus,
            [booking.booking_type === 'grading' ? 'gradingBookingId' : 'helpBookingId']: '',
        });

        // Emit socket events
        const io = req.app.get('io');
        if (io) {
            io.to(`queue-${booking.queue_session_id}`).emit('booking-completed', {
                bookingId: booking.id,
                deskNumber: booking.desk_number,
                bookingType: booking.booking_type,
            });

            // Emit to student
            io.to(`booking-${booking.id}`).emit('your-booking-completed', {
                booking: booking.toJSON(),
            });

            // Send FCM push notification to student (Completed)
            fcmService.notifyStudentCompleted(booking, score)
                .catch(err => logger.error('FCM student completed notification error:', err));

            // Notify all waiting students that queue position may have changed
            io.to(`queue-${booking.queue_session_id}`).emit('queue-position-updated', {
                completedBookingType: booking.booking_type,
                completedQueueNumber: booking.queue_number,
            });
        }

        // [Background Worker] Trigger assignment for next booking (non-blocking)
        // This replaces the old tryAssignNextBooking call
        triggerAssignmentForSession(booking.queue_session_id).catch(err => {
            logger.error('Error triggering assignment:', err);
        });

        res.json({
            success: true,
            data: booking,
        });
    } catch (error) {
        await transaction.rollback();
        console.error('Error completing booking:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Try to assign next waiting booking to a worker
 */
const tryAssignNextBooking = async (sessionId, workerId, io) => {
    try {
        logger.info(`tryAssignNextBooking: sessionId=${sessionId}, workerId=${workerId}`);
        
        const worker = await QueueWorker.findOne({
            where: {
                queue_session_id: sessionId,
                user_id: workerId,
                status: 'online',
            },
        });

        logger.info(`Worker lookup result: ${worker ? `found (id=${worker.id}, user_id=${worker.user_id}, status=${worker.status})` : 'not found'}`);

        if (!worker) {
            logger.info('Worker not found or not online, skipping assignment');
            return;
        }

        // Find next waiting booking matching worker preferences
        const whereCondition = {
            queue_session_id: sessionId,
            status: 'waiting',
        };

        if (worker.accept_grading && worker.accept_help) {
            whereCondition.booking_type = { [Op.in]: ['grading', 'help'] };
        } else if (worker.accept_grading) {
            whereCondition.booking_type = 'grading';
        } else if (worker.accept_help) {
            whereCondition.booking_type = 'help';
        } else {
            logger.debug('Worker does not accept any booking type');
            return;
        }

        logger.info(`Looking for next booking with condition: ${JSON.stringify(whereCondition)}`);

        const nextBooking = await QueueBooking.findOne({
            where: whereCondition,
            order: [['created_at', 'ASC']], // FIFO - oldest first regardless of booking type
        });

        logger.info(`Next booking: ${nextBooking ? `id=${nextBooking.id}, type=${nextBooking.booking_type}, queue=${nextBooking.queue_number}` : 'none'}`);

        if (nextBooking) {
            logger.info(`Calling tryAssignBooking for booking ${nextBooking.id}`);
            await tryAssignBooking(sessionId, nextBooking.id, io);
        } else {
            logger.info('No waiting bookings found');
        }
    } catch (error) {
        console.error('Error assigning next booking:', error);
    }
};

/**
 * Skip/Cancel booking (Worker)
 */
const skipBooking = async (req, res) => {
    const transaction = await sequelize.transaction();

    try {
        const { bookingId } = req.params;
        const { reason } = req.body;

        const booking = await QueueBooking.findByPk(bookingId, { transaction });

        if (!booking) {
            await transaction.rollback();
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบข้อมูลการจอง' },
            });
        }

        // Mark as no_show
        await booking.update(
            {
                status: 'no_show',
                worker_note: reason || 'ไม่พบนักศึกษา',
                completed_at: new Date(),
            },
            { transaction }
        );

        // Update desk status
        const deskStatus = await QueueDeskStatus.findOne({
            where: {
                queue_session_id: booking.queue_session_id,
                desk_id: booking.desk_id,
            },
            transaction,
        });

        if (deskStatus) {
            if (booking.booking_type === 'grading') {
                await deskStatus.update(
                    {
                        grading_status: 'not_started',
                        grading_booking_id: null,
                    },
                    { transaction }
                );
            } else {
                await deskStatus.update(
                    {
                        help_status: 'none',
                        help_booking_id: null,
                    },
                    { transaction }
                );
            }
        }

        // Free up worker
        const worker = await QueueWorker.findOne({
            where: {
                queue_session_id: booking.queue_session_id,
                user_id: req.user.id,
            },
            transaction,
        });

        // If worker was paused, set to offline. Otherwise set to online.
        const wasPaused = worker && worker.status === 'paused';
        if (worker) {
            await worker.update(
                {
                    status: wasPaused ? 'offline' : 'online',
                    current_booking_id: null,
                    last_active_at: new Date(),
                },
                { transaction }
            );
            
            // [Redis] Update worker state
            if (wasPaused) {
                await redisQueue.setWorkerOffline(booking.queue_session_id, req.user.id);
            } else {
                await redisQueue.setWorkerOnline(booking.queue_session_id, req.user.id);
            }
        }

        await transaction.commit();

        // [Redis] Update desk status
        await redisQueue.setDeskStatus(booking.queue_session_id, booking.desk_id, {
            [booking.booking_type === 'grading' ? 'gradingStatus' : 'helpStatus']: booking.booking_type === 'grading' ? 'not_started' : 'none',
            [booking.booking_type === 'grading' ? 'gradingBookingId' : 'helpBookingId']: '',
        });

        // Emit socket events
        const io = req.app.get('io');
        if (io) {
            io.to(`queue-${booking.queue_session_id}`).emit('booking-skipped', {
                bookingId: booking.id,
                deskNumber: booking.desk_number,
            });
        }

        // [Background Worker] Trigger assignment for next booking (non-blocking)
        // This replaces the old tryAssignNextBooking call
        triggerAssignmentForSession(booking.queue_session_id).catch(err => {
            logger.error('Error triggering assignment:', err);
        });

        res.json({
            success: true,
            message: 'ข้ามคิวสำเร็จ',
        });
    } catch (error) {
        await transaction.rollback();
        console.error('Error skipping booking:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Get all bookings for a session
 */
const getSessionBookings = async (req, res) => {
    try {
        const { sessionId } = req.params;
        const { status, booking_type } = req.query;

        const where = { queue_session_id: sessionId };
        if (status) where.status = status;
        if (booking_type) where.booking_type = booking_type;

        const bookings = await QueueBooking.findAll({
            where,
            include: [
                {
                    model: Student,
                    as: 'student',
                    attributes: ['id', 'student_id', 'full_name'],
                },
                {
                    model: Desk,
                    as: 'desk',
                    attributes: ['id', 'number', 'type', 'x', 'y'],
                },
                {
                    model: User,
                    as: 'assignedWorker',
                    attributes: ['id', 'full_name'],
                },
            ],
            order: [['created_at', 'ASC']], // FIFO ordering
        });

        // Enrich bookings with zone info
        const session = await QueueSession.findByPk(sessionId, { attributes: ['classroom_id'] });
        let zones = [];
        if (session?.classroom_id) {
            zones = await Zone.findAll({
                where: { classroom_id: session.classroom_id },
                attributes: ['id', 'name', 'x', 'y', 'width', 'height', 'color'],
            });
        }

        const enrichedBookings = bookings.map(b => {
            const plain = b.toJSON();
            if (plain.desk && zones.length > 0) {
                const deskX = plain.desk.x || 0;
                const deskY = plain.desk.y || 0;
                const zone = zones.find(z =>
                    deskX >= z.x && deskX < z.x + z.width &&
                    deskY >= z.y && deskY < z.y + z.height
                );
                plain.zone = zone ? { id: zone.id, name: zone.name, color: zone.color } : null;
            } else {
                plain.zone = null;
            }
            return plain;
        });

        res.json({
            success: true,
            data: enrichedBookings,
        });
    } catch (error) {
        console.error('Error getting session bookings:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Get desk statuses for projector view
 */
const getDeskStatuses = async (req, res) => {
    try {
        const { sessionId } = req.params;

        const session = await QueueSession.findByPk(sessionId, {
            include: [
                {
                    model: Classroom,
                    as: 'classroom',
                    include: [
                        {
                            model: Desk,
                            as: 'desks',
                            where: {
                                [Op.or]: [
                                    { is_enabled: true },
                                    { type: 'teacher' },
                                ],
                            },
                            required: false,
                        },
                    ],
                },
            ],
        });

        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        const deskStatuses = await QueueDeskStatus.findAll({
            where: { queue_session_id: sessionId },
        });

        // Get active bookings for all desks in this session
        const activeBookings = await QueueBooking.findAll({
            where: {
                queue_session_id: sessionId,
                status: { [Op.in]: ['waiting', 'in_progress'] },
                desk_id: { [Op.ne]: null },
            },
            include: [
                {
                    model: Student,
                    as: 'student',
                    attributes: ['id', 'student_id', 'full_name'],
                },
            ],
            order: [['created_at', 'DESC']],
        });

        // Map bookings to desks (one booking per desk)
        const bookingMap = {};
        activeBookings.forEach((booking) => {
            // Keep only the most recent booking for each desk
            if (!bookingMap[booking.desk_id]) {
                bookingMap[booking.desk_id] = {
                    id: booking.id,
                    queue_number: booking.queue_number,
                    booking_type: booking.booking_type,
                    status: booking.status,
                    student_name: booking.student?.full_name || 'ไม่ระบุ',
                    student_code: booking.student?.student_id || '',
                };
            }
        });

        // Map desk statuses to desks
        const statusMap = {};
        deskStatuses.forEach((ds) => {
            statusMap[ds.desk_id] = ds;
        });

        const desksWithStatus = session.classroom.desks.map((desk) => ({
            ...desk.toJSON(),
            status: statusMap[desk.id] || {
                grading_status: 'not_started',
                help_status: 'none',
            },
            booking: bookingMap[desk.id] || null,
        }));

        // Get queue statistics
        const queueStats = await QueueBooking.findAll({
            where: { queue_session_id: sessionId, status: 'waiting' },
            attributes: [
                'booking_type',
                [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
            ],
            group: ['booking_type'],
            raw: true,
        });

        res.json({
            success: true,
            data: {
                session: {
                    id: session.id,
                    title: session.title,
                    pin_code: session.pin_code,
                    status: session.status,
                },
                classroom: {
                    id: session.classroom.id,
                    name: session.classroom.name,
                    building: session.classroom.building,
                },
                desks: desksWithStatus,
                queueStats: {
                    grading_waiting: queueStats.find((s) => s.booking_type === 'grading')?.count || 0,
                    help_waiting: queueStats.find((s) => s.booking_type === 'help')?.count || 0,
                },
            },
        });
    } catch (error) {
        console.error('Error getting desk statuses:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Verify PIN and get session info (for student booking page)
 */
const verifyPIN = async (req, res) => {
    try {
        const { pin_code } = req.body;

        const session = await QueueSession.findOne({
            where: { pin_code, status: { [Op.in]: ['active', 'paused'] } },
            include: [
                {
                    model: Classroom,
                    as: 'classroom',
                    attributes: ['id', 'name', 'building'],
                },
                {
                    model: Course,
                    as: 'course',
                    attributes: ['id', 'code', 'name'],
                },
            ],
        });

        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว' },
            });
        }

        // If session is paused, return specific error
        if (session.status === 'paused') {
            return res.status(400).json({
                success: false,
                error: { 
                    message: 'ปิดรับการจองคิวชั่วคราว กรุณารอสักครู่',
                    code: 'SESSION_PAUSED',
                },
            });
        }

        res.json({
            success: true,
            data: {
                session_id: session.id,
                title: session.title,
                course: session.course,
                classroom: session.classroom,
                require_attendance: session.require_attendance,
            },
        });
    } catch (error) {
        console.error('Error verifying PIN:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Validate booking info before creating booking
 * Check student enrollment and desk availability
 */
const validateBookingInfo = async (req, res) => {
    try {
        const { pin_code, student_id, desk_number, booking_type } = req.body;

        const errors = [];
        const warnings = [];

        // Find session by PIN
        const session = await QueueSession.findOne({
            where: { pin_code, status: { [Op.in]: ['active', 'paused'] } },
            include: [
                {
                    model: Course,
                    as: 'course',
                    attributes: ['id', 'code', 'name'],
                },
                {
                    model: Classroom,
                    as: 'classroom',
                    include: [{ model: Desk, as: 'desks' }],
                },
            ],
        });

        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว' },
            });
        }

        // If session is paused, reject validation
        if (session.status === 'paused') {
            return res.status(400).json({
                success: false,
                error: { 
                    message: 'ปิดรับการจองคิวชั่วคราว กรุณารอสักครู่',
                    code: 'SESSION_PAUSED',
                },
            });
        }

        let studentInfo = null;
        let deskInfo = null;

        // Validate student
        if (student_id) {
            const student = await Student.findOne({
                where: { student_id },
            });

            if (!student) {
                errors.push({
                    field: 'student_id',
                    message: 'ไม่พบรหัสนักศึกษานี้ในระบบ',
                });
            } else {
                // Check if student is enrolled in the course (via any section)
                const enrollment = await CourseSectionStudent.findOne({
                    include: [{
                        model: CourseSection,
                        as: 'section',
                        where: { course_id: session.course_id },
                        required: true,
                    }],
                    where: {
                        student_id: student.id,
                    },
                });

                if (!enrollment) {
                    errors.push({
                        field: 'student_id',
                        message: `รหัสนักศึกษา ${student_id} ไม่ได้ลงทะเบียนในรายวิชานี้`,
                    });
                } else {
                    studentInfo = {
                        id: student.id,
                        student_id: student.student_id,
                        full_name: student.full_name,
                    };

                    // Check attendance requirement
                    if (session.require_attendance && session.linked_attendance_session_id) {
                        const attendanceRecord = await AttendanceRecord.findOne({
                            where: {
                                attendance_session_id: session.linked_attendance_session_id,
                                student_id: student.id,
                                status: { [Op.in]: ['present', 'late'] },
                            },
                        });

                        if (!attendanceRecord) {
                            errors.push({
                                field: 'student_id',
                                message: 'นักศึกษายังไม่ได้เช็คชื่อในรอบการเรียนนี้',
                            });
                        }
                    }

                    // Check if student already has active booking in this session
                    const existingBooking = await QueueBooking.findOne({
                        where: {
                            queue_session_id: session.id,
                            student_id: student.id,
                            status: { [Op.in]: ['waiting', 'in_progress'] },
                        },
                    });

                    if (existingBooking) {
                        warnings.push({
                            field: 'student_id',
                            message: `นักศึกษามีการจองคิวอยู่แล้ว (คิวที่ ${existingBooking.queue_number})`,
                            existing_booking: {
                                id: existingBooking.id,
                                queue_number: existingBooking.queue_number,
                                booking_type: existingBooking.booking_type,
                                status: existingBooking.status,
                            },
                        });
                    }

                    // Check if student already has score for grading type
                    if (booking_type === 'grading' && session.linked_assignment_id) {
                        const assignment = await Assignment.findOne({
                            where: { id: session.linked_assignment_id },
                            include: [{ model: AssignmentSubItem, as: 'subItems' }],
                        });

                        if (assignment) {
                            const hasSubItems = assignment.subItems && assignment.subItems.length > 0;

                            if (hasSubItems) {
                                // Check how many sub-items are already graded
                                const gradedSubItems = await Score.findAll({
                                    where: {
                                        assignment_id: assignment.id,
                                        student_id: student.id,
                                        sub_item_id: { [Op.ne]: null },
                                        status: 'graded',
                                    },
                                });

                                const gradedSubItemIds = gradedSubItems.map(s => s.sub_item_id);
                                const allSubItemIds = assignment.subItems.map(s => s.id);
                                const allGraded = allSubItemIds.every(id => gradedSubItemIds.includes(id));

                                if (allGraded) {
                                    errors.push({
                                        field: 'student_id',
                                        message: 'นักศึกษาได้รับการตรวจครบทุกข้อแล้ว ไม่สามารถจองคิวตรวจงานได้อีก',
                                    });
                                } else if (gradedSubItems.length > 0) {
                                    // Some items graded - show info
                                    const remainingCount = allSubItemIds.length - gradedSubItemIds.length;
                                    warnings.push({
                                        field: 'student_id',
                                        message: `นักศึกษาได้รับการตรวจไปแล้ว ${gradedSubItems.length}/${allSubItemIds.length} ข้อ (เหลืออีก ${remainingCount} ข้อ)`,
                                    });
                                }
                            } else {
                                // Single score - check if already graded
                                const existingScore = await Score.findOne({
                                    where: {
                                        assignment_id: assignment.id,
                                        student_id: student.id,
                                        sub_item_id: null,
                                        status: 'graded',
                                    },
                                });

                                if (existingScore) {
                                    errors.push({
                                        field: 'student_id',
                                        message: `นักศึกษาได้รับการตรวจงานแล้ว (${existingScore.score} คะแนน) ไม่สามารถจองคิวตรวจงานได้อีก`,
                                    });
                                }
                            }
                        }
                    }
                }
            }
        }

        // Validate desk
        if (desk_number) {
            const desk = await Desk.findOne({
                where: {
                    classroom_id: session.classroom_id,
                    number: desk_number,
                    is_enabled: true,
                },
            });

            if (!desk) {
                // Check if desk exists but disabled
                const disabledDesk = await Desk.findOne({
                    where: {
                        classroom_id: session.classroom_id,
                        number: desk_number,
                    },
                });

                if (disabledDesk) {
                    errors.push({
                        field: 'desk_number',
                        message: `โต๊ะหมายเลข ${desk_number} ถูกปิดใช้งาน`,
                    });
                } else {
                    errors.push({
                        field: 'desk_number',
                        message: `ไม่พบโต๊ะหมายเลข ${desk_number} ในห้องนี้`,
                    });
                }
            } else {
                deskInfo = {
                    id: desk.id,
                    number: desk.number,
                    type: desk.type,
                };

                // Check desk status
                const deskStatus = await QueueDeskStatus.findOne({
                    where: {
                        queue_session_id: session.id,
                        desk_id: desk.id,
                    },
                });

                if (deskStatus) {
                    if (booking_type === 'grading') {
                        if (deskStatus.grading_status === 'completed') {
                            errors.push({
                                field: 'desk_number',
                                message: `โต๊ะหมายเลข ${desk_number} ได้รับการตรวจงานแล้ว`,
                            });
                        } else if (['waiting', 'in_progress'].includes(deskStatus.grading_status)) {
                            errors.push({
                                field: 'desk_number',
                                message: `โต๊ะหมายเลข ${desk_number} มีการจองตรวจงานอยู่แล้ว`,
                            });
                        }
                    }
                }
            }
        }

        const isValid = errors.length === 0;

        res.json({
            success: true,
            data: {
                valid: isValid,
                errors,
                warnings,
                student: studentInfo,
                desk: deskInfo,
            },
        });
    } catch (error) {
        console.error('Error validating booking info:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Check if student has existing active booking in session
 * Used to restore booking state after page refresh
 */
const checkExistingBooking = async (req, res) => {
    try {
        const { pin_code, student_id } = req.body;

        // Find session by PIN
        const session = await QueueSession.findOne({
            where: { pin_code, status: 'active' },
        });

        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'PIN ไม่ถูกต้อง หรือไม่มีการเปิดรับจองคิว' },
            });
        }

        // Find student
        const student = await Student.findOne({
            where: { student_id },
        });

        if (!student) {
            return res.json({
                success: true,
                data: { has_booking: false },
            });
        }

        // Find active booking
        const booking = await QueueBooking.findOne({
            where: {
                queue_session_id: session.id,
                student_id: student.id,
                status: { [Op.in]: ['waiting', 'in_progress'] },
            },
            include: [
                {
                    model: QueueSession,
                    as: 'session',
                    attributes: ['id', 'title', 'status'],
                },
            ],
        });

        if (!booking) {
            return res.json({
                success: true,
                data: { has_booking: false },
            });
        }

        // Get position in queue (count all waiting bookings created before this one)
        const waitingAhead = await QueueBooking.count({
            where: {
                queue_session_id: session.id,
                status: 'waiting',
                created_at: { [Op.lt]: booking.created_at },
            },
        });

        res.json({
            success: true,
            data: {
                has_booking: true,
                booking: {
                    id: booking.id,
                    queue_number: booking.queue_number,
                    booking_type: booking.booking_type,
                    desk_number: booking.desk_number,
                    status: booking.status,
                    position_in_queue: waitingAhead,
                },
            },
        });
    } catch (error) {
        console.error('Error checking existing booking:', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

/**
 * Update queue session status (Public - for projector view)
 * Only allows pause/resume transitions (active ↔ paused)
 * Does not require authentication — safe because it only toggles pause state.
 */
const updateQueueSessionStatusPublic = async (req, res) => {
    try {
        const { sessionId } = req.params;
        const { status } = req.body;

        // Only allow pause/resume from projector
        if (!['active', 'paused'].includes(status)) {
            return res.status(400).json({
                success: false,
                error: { message: 'ใช้ได้เฉพาะ active หรือ paused เท่านั้น' },
            });
        }

        const session = await QueueSession.findByPk(sessionId);
        if (!session) {
            return res.status(404).json({
                success: false,
                error: { message: 'ไม่พบ Queue Session' },
            });
        }

        // Validate: only active↔paused allowed
        const validTransitions = {
            active: ['paused'],
            paused: ['active'],
        };

        if (!validTransitions[session.status] || !validTransitions[session.status].includes(status)) {
            return res.status(400).json({
                success: false,
                error: {
                    message: `ไม่สามารถเปลี่ยนสถานะจาก ${session.status} เป็น ${status}`,
                },
            });
        }

        await session.update({ status });
        invalidateSessionCache();

        logCourseActivity({
            courseId: session.course_id,
            actorUserId: req.user?.id || null,
            action: `queue_session_${status}`,
            category: 'queue',
            targetType: 'queue_session',
            targetId: sessionId,
            targetName: session.title,
            detail: { status, source: 'projector' },
        });

        // Emit socket event for real-time updates
        const io = req.app.get('io');
        if (io) {
            io.to(`queue-${sessionId}`).emit('session-status-changed', {
                sessionId,
                status,
            });
        }

        res.json({
            success: true,
            data: session,
        });
    } catch (error) {
        console.error('Error updating queue session status (projector):', error);
        res.status(500).json({
            success: false,
            error: { message: error.message },
        });
    }
};

module.exports = {
    // Session management
    getQueueSessions,
    getQueueSession,
    createQueueSession,
    updateQueueSession,
    updateQueueSessionStatus,
    deleteQueueSession,
    regeneratePIN,

    // Worker management
    joinAsWorker,
    leaveAsWorker,
    getWorkers,
    getWorkerCurrentBooking,

    // Booking management
    createBooking,
    getBookingStatus,
    cancelBooking,
    completeBooking,
    skipBooking,
    getSessionBookings,

    // Projector view
    getDeskStatuses,
    updateQueueSessionStatusPublic,

    // Public
    verifyPIN,
    validateBookingInfo,
    checkExistingBooking,
};
