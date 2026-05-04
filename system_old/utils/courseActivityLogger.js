/**
 * Course Activity Logger
 * Helper utility for logging course activities.
 * Fire-and-forget — does NOT block the main request.
 */

const { CourseActivityLog } = require('../models');
const logger = require('./logger');

/**
 * Log a course activity.
 *
 * @param {Object} params
 * @param {string}  params.courseId       - Course ID
 * @param {number}  params.actorUserId    - User performing the action
 * @param {string}  params.action         - e.g. 'create_assignment', 'submit_score'
 * @param {string}  [params.category]     - 'assignment' | 'score' | 'attendance' | 'queue' | 'course' | 'member' | 'general'
 * @param {string}  [params.targetType]   - e.g. 'assignment', 'student', 'section', 'ta'
 * @param {string}  [params.targetId]     - ID of the affected entity
 * @param {string}  [params.targetName]   - Human-readable name
 * @param {Object}  [params.detail]       - Additional JSON data
 */
function logCourseActivity({
  courseId,
  actorUserId,
  action,
  category = 'general',
  targetType = null,
  targetId = null,
  targetName = null,
  detail = null,
}) {
  // Fire-and-forget: do not await
  CourseActivityLog.create({
    course_id: courseId,
    actor_user_id: actorUserId,
    action,
    category,
    target_type: targetType,
    target_id: targetId ? String(targetId) : null,
    target_name: targetName,
    detail,
  }).catch((err) => {
    logger.error('[CourseActivityLog] Failed to log activity:', err.message);
  });
}

module.exports = { logCourseActivity };
