/**
 * Course Activity Log Controller
 * บันทึกและดึงข้อมูลกิจกรรมภายในรายวิชา + สถิติ TA
 */

const { CourseActivityLog, User, Score, Assignment, AssignmentSubItem, Student, QueueBooking, QueueSession, QueueWorker, Course, CourseTA, sequelize } = require('../models');
const { Op } = require('sequelize');
const asyncHandler = require('../utils/asyncHandler');
const ApiError = require('../utils/ApiError');

// ============================================
// Activity Log Endpoints
// ============================================

/**
 * Get activity logs for a course
 * @route GET /api/courses/:courseId/activity-logs
 */
const getActivityLogs = asyncHandler(async (req, res) => {
  const { courseId } = req.params;
  const {
    page = 1,
    limit = 30,
    category = '',
    action = '',
    actorId = '',
    startDate = '',
    endDate = '',
    search = '',
  } = req.query;

  const where = { course_id: courseId };

  if (category) where.category = category;
  if (action) where.action = action;
  if (actorId) where.actor_user_id = actorId;
  if (startDate || endDate) {
    where.created_at = {};
    if (startDate) where.created_at[Op.gte] = new Date(startDate);
    if (endDate) where.created_at[Op.lte] = new Date(endDate + 'T23:59:59');
  }
  if (search) {
    where[Op.or] = [
      { target_name: { [Op.like]: `%${search}%` } },
      { action: { [Op.like]: `%${search}%` } },
    ];
  }

  const offset = (parseInt(page) - 1) * parseInt(limit);

  const { count, rows } = await CourseActivityLog.findAndCountAll({
    where,
    include: [
      {
        model: User,
        as: 'actor',
        attributes: ['id', 'full_name', 'email', 'role', 'avatar'],
      },
    ],
    order: [['created_at', 'DESC']],
    limit: parseInt(limit),
    offset,
  });

  res.json({
    success: true,
    data: {
      logs: rows,
      pagination: {
        total: count,
        page: parseInt(page),
        limit: parseInt(limit),
        totalPages: Math.ceil(count / parseInt(limit)),
      },
    },
  });
});

/**
 * Get activity log statistics/summary for a course
 * @route GET /api/courses/:courseId/activity-logs/stats
 */
const getActivityStats = asyncHandler(async (req, res) => {
  const { courseId } = req.params;
  const { days = 30 } = req.query;

  const sinceDate = new Date();
  sinceDate.setDate(sinceDate.getDate() - parseInt(days));

  const [categoryStats, actionStats, actorAggregates, timeline, totalLogs] = await Promise.all([
    // By category
    CourseActivityLog.findAll({
      where: { course_id: courseId, created_at: { [Op.gte]: sinceDate } },
      attributes: ['category', [sequelize.fn('COUNT', sequelize.col('id')), 'count']],
      group: ['category'],
      raw: true,
    }),
    // By action
    CourseActivityLog.findAll({
      where: { course_id: courseId, created_at: { [Op.gte]: sinceDate } },
      attributes: ['action', [sequelize.fn('COUNT', sequelize.col('id')), 'count']],
      group: ['action'],
      order: [[sequelize.fn('COUNT', sequelize.col('id')), 'DESC']],
      limit: 10,
      raw: true,
    }),
    // By actor (aggregate only, no join)
    CourseActivityLog.findAll({
      where: { course_id: courseId, created_at: { [Op.gte]: sinceDate } },
      attributes: ['actor_user_id', [sequelize.fn('COUNT', sequelize.col('id')), 'count']],
      group: ['actor_user_id'],
      order: [[sequelize.fn('COUNT', sequelize.col('id')), 'DESC']],
      raw: true,
    }),
    // Daily timeline
    CourseActivityLog.findAll({
      where: { course_id: courseId, created_at: { [Op.gte]: sinceDate } },
      attributes: [
        [sequelize.fn('DATE', sequelize.col('created_at')), 'date'],
        [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
      ],
      group: [sequelize.fn('DATE', sequelize.col('created_at'))],
      order: [[sequelize.fn('DATE', sequelize.col('created_at')), 'ASC']],
      raw: true,
    }),
    // Total count
    CourseActivityLog.count({
      where: { course_id: courseId },
    }),
  ]);

  // Fetch actor user details separately to avoid GROUP BY + JOIN conflict
  const actorUserIds = actorAggregates.map(a => a.actor_user_id);
  const actorUsers = actorUserIds.length > 0
    ? await User.findAll({
        where: { id: { [Op.in]: actorUserIds } },
        attributes: ['id', 'full_name', 'role', 'avatar'],
        raw: true,
      })
    : [];
  const userMap = Object.fromEntries(actorUsers.map(u => [u.id, u]));

  res.json({
    success: true,
    data: {
      total: totalLogs,
      period: parseInt(days),
      categoryStats,
      actionStats,
      actorStats: actorAggregates.map(a => ({
        userId: a.actor_user_id,
        fullName: userMap[a.actor_user_id]?.full_name || 'Unknown',
        role: userMap[a.actor_user_id]?.role || 'unknown',
        avatar: userMap[a.actor_user_id]?.avatar || null,
        count: parseInt(a.count),
      })),
      timeline,
    },
  });
});

/**
 * Get filter options (categories, actors) for a course
 * @route GET /api/courses/:courseId/activity-logs/filters
 */
const getActivityFilters = asyncHandler(async (req, res) => {
  const { courseId } = req.params;

  const [categories, actions, actors] = await Promise.all([
    CourseActivityLog.findAll({
      where: { course_id: courseId },
      attributes: [[sequelize.fn('DISTINCT', sequelize.col('category')), 'category']],
      raw: true,
    }),
    CourseActivityLog.findAll({
      where: { course_id: courseId },
      attributes: [
        [sequelize.fn('DISTINCT', sequelize.col('action')), 'action'],
        'category',
      ],
      group: ['action', 'category'],
      raw: true,
    }),
    CourseActivityLog.findAll({
      where: { course_id: courseId },
      attributes: [[sequelize.fn('DISTINCT', sequelize.col('actor_user_id')), 'actor_user_id']],
      raw: true,
    }),
  ]);

  // Fetch actor details separately to avoid DISTINCT + JOIN conflict
  const actorUserIds = actors.map(a => a.actor_user_id);
  const actorUsers = actorUserIds.length > 0
    ? await User.findAll({
        where: { id: { [Op.in]: actorUserIds } },
        attributes: ['id', 'full_name', 'role', 'avatar'],
        raw: true,
      })
    : [];
  const userMap = Object.fromEntries(actorUsers.map(u => [u.id, u]));

  res.json({
    success: true,
    data: {
      categories: categories.map(c => c.category),
      actions: actions.map(a => ({ action: a.action, category: a.category })),
      actors: actorUserIds.map(uid => ({
        id: uid,
        fullName: userMap[uid]?.full_name || 'Unknown',
        role: userMap[uid]?.role || 'unknown',
        avatar: userMap[uid]?.avatar || null,
      })),
    },
  });
});

// ============================================
// TA Statistics Endpoints
// ============================================

/**
 * Get TA grading statistics overview for a course
 * @route GET /api/courses/:courseId/ta-stats
 */
const getTAStats = asyncHandler(async (req, res) => {
  const { courseId } = req.params;

  // Get course with assignments
  const course = await Course.findByPk(courseId);
  if (!course) throw new ApiError(404, 'ไม่พบรายวิชา');

  // Get all TAs for this course
  const tas = await CourseTA.findAll({
    where: { course_id: courseId },
    include: [{ model: User, as: 'taUser', attributes: ['id', 'full_name', 'email', 'avatar'] }],
  });

  // Get all assignments for this course
  const assignments = await Assignment.findAll({
    where: { course_id: courseId },
    attributes: ['id', 'name', 'max_score', 'assignment_type'],
    include: [{
      model: AssignmentSubItem,
      as: 'subItems',
      attributes: ['id', 'name', 'max_score'],
    }],
    order: [['created_at', 'ASC']],
  });

  const assignmentIds = assignments.map(a => a.id);

  // Get all scores graded by TAs for these assignments
  const taUserIds = tas.map(t => t.user_id);

  const scores = await Score.findAll({
    where: {
      assignment_id: { [Op.in]: assignmentIds },
      graded_by: { [Op.in]: taUserIds },
      status: 'graded',
    },
    attributes: ['id', 'assignment_id', 'student_id', 'sub_item_id', 'score', 'graded_by', 'graded_at', 'comment'],
    include: [
      { model: User, as: 'grader', attributes: ['id', 'full_name'] },
      { model: Student, as: 'student', attributes: ['id', 'student_id', 'full_name'] },
    ],
    order: [['graded_at', 'DESC']],
  });

  // Get queue bookings completed by TAs (for queue grading stats)
  const queueSessions = await QueueSession.findAll({
    where: { course_id: courseId },
    attributes: ['id'],
  });
  const sessionIds = queueSessions.map(s => s.id);

  let queueStats = [];
  if (sessionIds.length > 0) {
    queueStats = await QueueBooking.findAll({
      where: {
        queue_session_id: { [Op.in]: sessionIds },
        assigned_worker_id: { [Op.in]: taUserIds },
        status: 'completed',
      },
      attributes: [
        'assigned_worker_id',
        [sequelize.fn('COUNT', sequelize.col('QueueBooking.id')), 'total_completed'],
        [sequelize.fn('AVG', sequelize.col('score')), 'avg_score'],
        [sequelize.fn('MIN', sequelize.col('score')), 'min_score'],
        [sequelize.fn('MAX', sequelize.col('score')), 'max_score'],
      ],
      group: ['assigned_worker_id'],
      raw: true,
    });
  }

  // ============================================
  // PERFORMANCE: Pre-index scores in a single O(S) pass.
  //
  // Before: nested Array.filter() inside TA × Assignment loops → O(T × A × S)
  // After:  HashMap lookups → O(S) build + O(1) per lookup → O(S + T×A) total
  //
  // Three indexes:
  //   scoresByTA         Map<graded_by, Score[]>       — filter by TA
  //   scoresByAssignment Map<assignment_id, Score[]>   — overall stats
  //   scoresByTAAssignment Map<"taId|assignmentId", {main: Score[], sub: Score[]}>
  //                       — per-TA per-assignment, pre-split main vs sub-item
  // ============================================
  const scoresByTA = new Map();           // graded_by → Score[]
  const scoresByAssignment = new Map();   // assignment_id → Score[] (main only, no sub_item)
  const scoresByTAAssignment = new Map(); // "taId|assignmentId" → { main: Score[], sub: Score[] }

  for (const s of scores) {
    const taId = s.graded_by;
    const aId = s.assignment_id;
    const compositeKey = `${taId}|${aId}`;

    // Index 1: by TA
    if (!scoresByTA.has(taId)) scoresByTA.set(taId, []);
    scoresByTA.get(taId).push(s);

    // Index 2: by assignment (main scores only, for overall stats)
    if (!s.sub_item_id) {
      if (!scoresByAssignment.has(aId)) scoresByAssignment.set(aId, []);
      scoresByAssignment.get(aId).push(s);
    }

    // Index 3: by TA×Assignment, pre-split main vs sub-item
    if (!scoresByTAAssignment.has(compositeKey)) {
      scoresByTAAssignment.set(compositeKey, { main: [], sub: [] });
    }
    const bucket = scoresByTAAssignment.get(compositeKey);
    if (s.sub_item_id) {
      bucket.sub.push(s);
    } else {
      bucket.main.push(s);
    }
  }

  // Queue stats indexed by worker id — O(Q) where Q = queue rows (small)
  const queueStatMap = new Map();
  for (const q of queueStats) {
    queueStatMap.set(q.assigned_worker_id, q);
  }

  // ============================================
  // Build per-TA stats — O(T × A) with O(1) lookups
  // ============================================
  const taStats = tas.map(ta => {
    const taId = ta.user_id;
    const taScoreList = scoresByTA.get(taId) || [];
    const queueStat = queueStatMap.get(taId);

    // Per-assignment breakdown — O(A) per TA, no inner filtering
    const perAssignment = assignments.map(assignment => {
      const compositeKey = `${taId}|${assignment.id}`;
      const bucket = scoresByTAAssignment.get(compositeKey) || { main: [], sub: [] };
      const mainScores = bucket.main;
      const subScores = bucket.sub;

      // Aggregate main scores in a single pass — O(mainScores.length)
      let sum = 0, min = Infinity, max = -Infinity;
      for (const sc of mainScores) {
        const v = parseFloat(sc.score || 0);
        sum += v;
        if (v < min) min = v;
        if (v > max) max = v;
      }

      return {
        assignmentId: assignment.id,
        assignmentName: assignment.name,
        maxScore: assignment.max_score,
        totalGraded: mainScores.length + subScores.length,
        mainScores: mainScores.length,
        subItemScoresCount: subScores.length,
        avgScore: mainScores.length > 0
          ? parseFloat((sum / mainScores.length).toFixed(2))
          : null,
        minScore: mainScores.length > 0 ? min : null,
        maxScore_given: mainScores.length > 0 ? max : null,
        scoreDistribution: buildScoreDistribution(mainScores, assignment.max_score),
      };
    }).filter(a => a.totalGraded > 0);

    return {
      userId: taId,
      fullName: ta.taUser?.full_name || 'Unknown',
      email: ta.taUser?.email || '',
      avatar: ta.taUser?.avatar || null,
      totalScoresGraded: taScoreList.length,
      assignmentsGraded: perAssignment.length,
      perAssignment,
      queueStats: queueStat ? {
        totalCompleted: parseInt(queueStat.total_completed),
        avgScore: queueStat.avg_score ? parseFloat(parseFloat(queueStat.avg_score).toFixed(2)) : null,
        minScore: queueStat.min_score !== null ? parseFloat(queueStat.min_score) : null,
        maxScore: queueStat.max_score !== null ? parseFloat(queueStat.max_score) : null,
      } : null,
    };
  });

  // ============================================
  // Overall assignment stats — O(A), using pre-indexed scoresByAssignment
  // ============================================
  const overallAssignmentStats = assignments.map(assignment => {
    const aScores = scoresByAssignment.get(assignment.id) || [];
    let sum = 0;
    for (const sc of aScores) sum += parseFloat(sc.score || 0);
    return {
      assignmentId: assignment.id,
      assignmentName: assignment.name,
      maxScore: assignment.max_score,
      totalGraded: aScores.length,
      avgScore: aScores.length > 0
        ? parseFloat((sum / aScores.length).toFixed(2))
        : null,
    };
  });

  // O(A) map for O(1) lookup during KPI calculation
  const overallStatsMap = new Map();
  for (const os of overallAssignmentStats) overallStatsMap.set(os.assignmentId, os);

  // ============================================
  // Performance Score Calculation (0–100)
  // ============================================
  const totalTAs = tas.length;
  const totalGradedAll = scores.length;
  const assignmentsWithScores = overallAssignmentStats.filter(a => a.totalGraded > 0);
  const totalQueueCompleted = queueStats.reduce((s, q) => s + parseInt(q.total_completed || 0), 0);
  const avgQueuePerTA = totalTAs > 0 ? totalQueueCompleted / totalTAs : 0;

  // Precompute overall std dev per assignment — O(S) total across all assignments
  const overallStdDevMap = new Map();
  for (const assignment of overallAssignmentStats) {
    const aScores = scoresByAssignment.get(assignment.assignmentId) || [];
    if (aScores.length > 1 && assignment.avgScore !== null) {
      let varianceSum = 0;
      for (const sc of aScores) {
        const diff = parseFloat(sc.score || 0) - assignment.avgScore;
        varianceSum += diff * diff;
      }
      overallStdDevMap.set(assignment.assignmentId, Math.sqrt(varianceSum / aScores.length));
    } else {
      overallStdDevMap.set(assignment.assignmentId, null);
    }
  }

  // ============================================
  // KPI computation per TA — O(T × A_graded) with O(1) lookups
  // No more scores.filter() inside these loops
  // ============================================
  const taStatsWithScore = taStats.map(ta => {
    const expectedShare = totalTAs > 0 ? totalGradedAll / totalTAs : 0;

    // KPI 1: Workload Contribution (30%)
    const workloadRatio = expectedShare > 0 ? ta.totalScoresGraded / expectedShare : 0;
    const kpiWorkload = Math.min(workloadRatio * 100, 100);

    // KPI 2: Assignment Coverage (15%)
    const kpiCoverage = assignmentsWithScores.length > 0
      ? (ta.assignmentsGraded / assignmentsWithScores.length) * 100
      : 0;

    // KPI 3: Grading Consistency (25%) — uses overallStatsMap O(1) lookup
    let consistencySum = 0;
    let consistencyCount = 0;
    for (const pa of ta.perAssignment) {
      const overall = overallStatsMap.get(pa.assignmentId);
      if (!overall || overall.avgScore === null || pa.avgScore === null || overall.maxScore <= 0) continue;
      const deviation = Math.abs(pa.avgScore - overall.avgScore) / overall.maxScore;
      consistencySum += Math.max(0, 1 - deviation);
      consistencyCount++;
    }
    const kpiConsistency = consistencyCount > 0 ? (consistencySum / consistencyCount) * 100 : 50;

    // KPI 4: Score Spread (10%) — uses scoresByTAAssignment O(1) lookup + overallStdDevMap
    let spreadSum = 0;
    let spreadCount = 0;
    for (const pa of ta.perAssignment) {
      const compositeKey = `${ta.userId}|${pa.assignmentId}`;
      const mainScores = scoresByTAAssignment.get(compositeKey)?.main || [];
      if (mainScores.length < 3 || pa.avgScore === null) continue;
      const overallStd = overallStdDevMap.get(pa.assignmentId);
      if (!overallStd || overallStd === 0) continue;

      let varianceSum = 0;
      for (const sc of mainScores) {
        const diff = parseFloat(sc.score || 0) - pa.avgScore;
        varianceSum += diff * diff;
      }
      const taStd = Math.sqrt(varianceSum / mainScores.length);
      const ratio = taStd / overallStd;
      spreadSum += Math.max(0, 100 - Math.abs(ratio - 1) * 100);
      spreadCount++;
    }
    const kpiSpread = spreadCount > 0 ? spreadSum / spreadCount : 50;

    // KPI 5: Queue Responsiveness (15%)
    const taQueueCompleted = ta.queueStats?.totalCompleted || 0;
    let kpiQueue;
    if (avgQueuePerTA <= 0 && taQueueCompleted <= 0) {
      kpiQueue = 50; // neutral if no queue data at all
    } else if (avgQueuePerTA <= 0) {
      kpiQueue = 100;
    } else {
      kpiQueue = Math.min((taQueueCompleted / avgQueuePerTA) * 100, 100);
    }

    // KPI 6: Anomaly Penalty (5%) — uses overallStatsMap O(1) lookup
    let anomalyCount = 0;
    const anomalyFlags = [];
    for (const pa of ta.perAssignment) {
      const overall = overallStatsMap.get(pa.assignmentId);
      if (overall && overall.avgScore !== null && pa.avgScore !== null) {
        const diff = Math.abs(pa.avgScore - overall.avgScore);
        if (diff > overall.maxScore * 0.3 && pa.totalGraded >= 3) {
          anomalyCount++;
          anomalyFlags.push({
            kind: 'score_deviation',
            severity: diff > overall.maxScore * 0.5 ? 'danger' : 'warning',
            message: `ค่าเฉลี่ยงาน "${pa.assignmentName}" (${pa.avgScore}) ต่างจากค่าเฉลี่ยรวม (${overall.avgScore}) มากกว่า 30%`,
            assignmentId: pa.assignmentId,
            assignmentName: pa.assignmentName,
          });
        }
      }
      if (pa.scoreDistribution.length > 0 && pa.totalGraded >= 5) {
        const maxBucket = Math.max(...pa.scoreDistribution.map(d => d.count));
        if (maxBucket / pa.totalGraded > 0.8) {
          anomalyCount++;
          anomalyFlags.push({
            kind: 'score_clustering',
            severity: 'warning',
            message: `งาน "${pa.assignmentName}" มีคะแนนซ้ำกันมากผิดปกติ (${maxBucket}/${pa.totalGraded} อยู่ในช่วงเดียวกัน)`,
            assignmentId: pa.assignmentId,
            assignmentName: pa.assignmentName,
          });
        }
      }
    }
    // Low coverage / low volume flags
    if (assignmentsWithScores.length > 0 && ta.assignmentsGraded / assignmentsWithScores.length < 0.3) {
      anomalyFlags.push({
        kind: 'low_coverage',
        severity: 'warning',
        message: `ตรวจงานเพียง ${ta.assignmentsGraded} จาก ${assignmentsWithScores.length} งาน (< 30%)`,
      });
    }
    if (expectedShare > 0 && ta.totalScoresGraded / expectedShare < 0.3) {
      anomalyFlags.push({
        kind: 'low_volume',
        severity: 'warning',
        message: `ตรวจงานเพียง ${ta.totalScoresGraded} รายการ จากส่วนแบ่งที่คาดหวัง ${Math.round(expectedShare)} รายการ`,
      });
    }
    const kpiAnomaly = Math.max(0, 100 - anomalyCount * 25);

    // Weighted final score
    const performanceScore = parseFloat((
      kpiWorkload * 0.30 +
      kpiCoverage * 0.15 +
      kpiConsistency * 0.25 +
      kpiSpread * 0.10 +
      kpiQueue * 0.15 +
      kpiAnomaly * 0.05
    ).toFixed(1));

    // Confidence level based on sample size
    const confidenceLevel = ta.totalScoresGraded >= 20 ? 'high'
      : ta.totalScoresGraded >= 10 ? 'medium' : 'low';

    return {
      ...ta,
      performanceScore,
      confidenceLevel,
      confidence: {
        level: confidenceLevel,
        sampleSize: ta.totalScoresGraded,
        minRecommended: 20,
      },
      kpiBreakdown: {
        workload:    { score: parseFloat(kpiWorkload.toFixed(1)),    weight: 0.30, label: 'ปริมาณงาน',       description: 'สัดส่วนงานที่ตรวจเทียบกับค่าเฉลี่ยต่อ TA' },
        coverage:    { score: parseFloat(kpiCoverage.toFixed(1)),    weight: 0.15, label: 'ความครอบคลุม',    description: 'จำนวนงานที่ตรวจเทียบกับงานทั้งหมด' },
        consistency: { score: parseFloat(kpiConsistency.toFixed(1)), weight: 0.25, label: 'ความสม่ำเสมอ',    description: 'ค่าเฉลี่ยคะแนนใกล้เคียงค่าเฉลี่ยรวมแค่ไหน' },
        spread:      { score: parseFloat(kpiSpread.toFixed(1)),      weight: 0.10, label: 'การกระจายคะแนน', description: 'การกระจายคะแนนใกล้เคียงภาพรวมแค่ไหน' },
        queue:       { score: parseFloat(kpiQueue.toFixed(1)),       weight: 0.15, label: 'คิวตรวจงาน',      description: 'จำนวนคิวที่สำเร็จเทียบกับค่าเฉลี่ยต่อ TA' },
        anomaly:     { score: parseFloat(kpiAnomaly.toFixed(1)),     weight: 0.05, label: 'ตรวจพบสิ่งผิดปกติ', description: 'หักคะแนนเมื่อพบพฤติกรรมผิดปกติ' },
      },
      anomalies: anomalyFlags,
    };
  });

  res.json({
    success: true,
    data: {
      taStats: taStatsWithScore,
      assignments: overallAssignmentStats,
      summary: {
        totalTAs: tas.length,
        totalAssignments: assignments.length,
        totalScoresGraded: scores.length,
      },
    },
  });
});

/**
 * Get detailed TA grading history (per-score records)
 * @route GET /api/courses/:courseId/ta-stats/:userId
 */
const getTADetail = asyncHandler(async (req, res) => {
  const { courseId, userId } = req.params;
  const { assignmentId = '', page = 1, limit = 50 } = req.query;

  // Verify TA belongs to course
  const ta = await CourseTA.findOne({
    where: { course_id: courseId, user_id: userId },
    include: [{ model: User, as: 'taUser', attributes: ['id', 'full_name', 'email'] }],
  });

  // Also allow looking up instructors as graders
  if (!ta) {
    const user = await User.findByPk(userId, { attributes: ['id', 'full_name', 'email', 'role'] });
    if (!user) throw new ApiError(404, 'ไม่พบผู้ใช้งาน');
  }

  const assignments = await Assignment.findAll({
    where: { course_id: courseId },
    attributes: ['id', 'name', 'max_score'],
  });
  const assignmentIds = assignmentId 
    ? [parseInt(assignmentId)]
    : assignments.map(a => a.id);

  const scoreWhere = {
    assignment_id: { [Op.in]: assignmentIds },
    graded_by: parseInt(userId),
    status: 'graded',
  };

  const offset = (parseInt(page) - 1) * parseInt(limit);

  const { count, rows: detailScores } = await Score.findAndCountAll({
    where: scoreWhere,
    include: [
      { model: Assignment, as: 'assignment', attributes: ['id', 'name', 'max_score'] },
      { model: AssignmentSubItem, as: 'subItem', attributes: ['id', 'name', 'max_score'] },
      { model: Student, as: 'student', attributes: ['id', 'student_id', 'full_name'] },
    ],
    order: [['graded_at', 'DESC']],
    limit: parseInt(limit),
    offset,
  });

  // Score timeline (daily grading counts)
  const timeline = await Score.findAll({
    where: {
      assignment_id: { [Op.in]: assignmentIds },
      graded_by: parseInt(userId),
      status: 'graded',
    },
    attributes: [
      [sequelize.fn('DATE', sequelize.col('graded_at')), 'date'],
      [sequelize.fn('COUNT', sequelize.col('Score.id')), 'count'],
      [sequelize.fn('AVG', sequelize.col('score')), 'avg_score'],
    ],
    group: [sequelize.fn('DATE', sequelize.col('graded_at'))],
    order: [[sequelize.fn('DATE', sequelize.col('graded_at')), 'ASC']],
    raw: true,
  });

  res.json({
    success: true,
    data: {
      user: ta?.taUser || await User.findByPk(userId, { attributes: ['id', 'full_name', 'email'] }),
      scores: detailScores,
      timeline,
      pagination: {
        total: count,
        page: parseInt(page),
        limit: parseInt(limit),
        totalPages: Math.ceil(count / parseInt(limit)),
      },
    },
  });
});

// ============================================
// Helpers
// ============================================

/**
 * Build score distribution into 5 buckets — SINGLE-PASS bucket sort O(N).
 *
 * Before: 5 × Array.filter() → O(5N) with 5 full scans
 * After:  1 pass placing each score into the correct bucket → O(N)
 *
 * @param {Array} scores - Array of score objects
 * @param {number} maxScore - Maximum possible score for the assignment
 * @returns {Array<{range: string, count: number}>}
 */
function buildScoreDistribution(scores, maxScore) {
  if (scores.length === 0) return [];
  const max = parseFloat(maxScore) || 100;
  const bucketSize = max / 5;

  // Pre-compute bucket boundaries and initialize counts
  const buckets = [];
  for (let i = 0; i < 5; i++) {
    buckets.push({
      range: `${Math.round(i * bucketSize)}-${Math.round((i + 1) * bucketSize)}`,
      count: 0,
      low: i * bucketSize,
      high: (i + 1) * bucketSize,
    });
  }

  // Single pass: assign each score to the correct bucket via index calculation
  for (const s of scores) {
    const v = parseFloat(s.score || 0);
    // Compute bucket index directly — O(1) per score
    let idx = Math.floor(v / bucketSize);
    // Clamp to last bucket for edge case (v === max)
    if (idx >= 5) idx = 4;
    if (idx < 0) idx = 0;
    buckets[idx].count++;
  }

  // Strip internal fields before returning
  return buckets.map(b => ({ range: b.range, count: b.count }));
}

module.exports = {
  getActivityLogs,
  getActivityStats,
  getActivityFilters,
  getTAStats,
  getTADetail,
};
