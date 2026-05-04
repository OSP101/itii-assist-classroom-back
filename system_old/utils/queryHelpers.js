/**
 * Query Helper Utilities
 * Optimized database query patterns to avoid N+1 problems
 */

const { sequelize } = require('../config/database');
const { Op } = require('sequelize');

/**
 * Batch count records by foreign key
 * Instead of: await Model.count({ where: { fk_id: id } }) in a loop
 * Use: await batchCount(Model, 'fk_id', ids)
 * 
 * @param {Model} model - Sequelize model
 * @param {string} groupByField - Field to group by
 * @param {Array} ids - Array of IDs to count
 * @returns {Object} - Map of id -> count
 */
const batchCount = async (model, groupByField, ids) => {
    if (!ids || ids.length === 0) return {};
    
    const results = await model.findAll({
        attributes: [
            groupByField,
            [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
        ],
        where: {
            [groupByField]: { [Op.in]: ids },
        },
        group: [groupByField],
        raw: true,
    });
    
    const countMap = {};
    ids.forEach(id => { countMap[id] = 0; });
    results.forEach(row => {
        countMap[row[groupByField]] = parseInt(row.count);
    });
    
    return countMap;
};

/**
 * Batch count with status grouping
 * Returns counts grouped by both foreign key and status
 * 
 * @param {Model} model - Sequelize model
 * @param {string} groupByField - Field to group by
 * @param {Array} ids - Array of IDs
 * @param {string} statusField - Status field name
 * @returns {Object} - Map of id -> { status1: count, status2: count, ... }
 */
const batchCountByStatus = async (model, groupByField, ids, statusField = 'status') => {
    if (!ids || ids.length === 0) return {};
    
    const results = await model.findAll({
        attributes: [
            groupByField,
            statusField,
            [sequelize.fn('COUNT', sequelize.col('id')), 'count'],
        ],
        where: {
            [groupByField]: { [Op.in]: ids },
        },
        group: [groupByField, statusField],
        raw: true,
    });
    
    const countMap = {};
    ids.forEach(id => { 
        countMap[id] = { total: 0 }; 
    });
    
    results.forEach(row => {
        const id = row[groupByField];
        const status = row[statusField];
        const count = parseInt(row.count);
        
        if (!countMap[id]) countMap[id] = { total: 0 };
        countMap[id][status] = count;
        countMap[id].total += count;
    });
    
    return countMap;
};

/**
 * Batch sum with grouping
 * 
 * @param {Model} model - Sequelize model
 * @param {string} groupByField - Field to group by
 * @param {Array} ids - Array of IDs
 * @param {string} sumField - Field to sum
 * @returns {Object} - Map of id -> sum
 */
const batchSum = async (model, groupByField, ids, sumField) => {
    if (!ids || ids.length === 0) return {};
    
    const results = await model.findAll({
        attributes: [
            groupByField,
            [sequelize.fn('SUM', sequelize.col(sumField)), 'total'],
        ],
        where: {
            [groupByField]: { [Op.in]: ids },
        },
        group: [groupByField],
        raw: true,
    });
    
    const sumMap = {};
    ids.forEach(id => { sumMap[id] = 0; });
    results.forEach(row => {
        sumMap[row[groupByField]] = parseFloat(row.total) || 0;
    });
    
    return sumMap;
};

/**
 * Batch fetch latest record per group
 * 
 * @param {Model} model - Sequelize model
 * @param {string} groupByField - Field to group by
 * @param {Array} ids - Array of IDs
 * @param {string} orderField - Field to order by (for finding latest)
 * @param {Array} attributes - Attributes to select
 * @returns {Object} - Map of id -> latest record
 */
const batchFetchLatest = async (model, groupByField, ids, orderField = 'created_at', attributes = null) => {
    if (!ids || ids.length === 0) return {};
    
    // Use raw SQL for better performance with window functions
    const tableName = model.getTableName();
    const attrList = attributes ? attributes.join(', ') : '*';
    
    const [results] = await sequelize.query(`
        SELECT ${attrList}, ${groupByField}
        FROM (
            SELECT ${attrList}, ${groupByField},
                   ROW_NUMBER() OVER (PARTITION BY ${groupByField} ORDER BY ${orderField} DESC) as rn
            FROM ${tableName}
            WHERE ${groupByField} IN (?)
        ) ranked
        WHERE rn = 1
    `, {
        replacements: [ids],
        type: sequelize.QueryTypes.SELECT,
    });
    
    const latestMap = {};
    ids.forEach(id => { latestMap[id] = null; });
    
    if (Array.isArray(results)) {
        results.forEach(row => {
            latestMap[row[groupByField]] = row;
        });
    }
    
    return latestMap;
};

/**
 * Batch check existence
 * 
 * @param {Model} model - Sequelize model
 * @param {string} field - Field to check
 * @param {Array} values - Values to check
 * @returns {Set} - Set of existing values
 */
const batchExists = async (model, field, values) => {
    if (!values || values.length === 0) return new Set();
    
    const results = await model.findAll({
        attributes: [field],
        where: {
            [field]: { [Op.in]: values },
        },
        raw: true,
    });
    
    return new Set(results.map(r => r[field]));
};

/**
 * Optimized aggregation query using raw SQL
 * 
 * @param {string} sql - Raw SQL query
 * @param {Array} replacements - Query parameters
 * @returns {Array} - Query results
 */
const rawQuery = async (sql, replacements = []) => {
    const [results] = await sequelize.query(sql, {
        replacements,
        type: sequelize.QueryTypes.SELECT,
    });
    return results;
};

/**
 * Build efficient IN clause with chunking for large arrays
 * 
 * @param {Model} model - Sequelize model
 * @param {Object} where - Where conditions
 * @param {Array} ids - Large array of IDs
 * @param {string} idField - Field name for IDs
 * @param {number} chunkSize - Chunk size (default: 1000)
 * @returns {Array} - Combined results
 */
const chunkedQuery = async (model, where, ids, idField, options = {}, chunkSize = 1000) => {
    if (!ids || ids.length === 0) return [];
    
    const chunks = [];
    for (let i = 0; i < ids.length; i += chunkSize) {
        chunks.push(ids.slice(i, i + chunkSize));
    }
    
    const results = await Promise.all(
        chunks.map(chunk => 
            model.findAll({
                where: {
                    ...where,
                    [idField]: { [Op.in]: chunk },
                },
                ...options,
            })
        )
    );
    
    return results.flat();
};

/**
 * Get student count per course (optimized)
 * Avoids N+1 by using a single aggregation query
 * 
 * @param {Array} courseIds - Array of course IDs
 * @returns {Object} - Map of course_id -> student count
 */
const getStudentCountsByCourse = async (courseIds) => {
    if (!courseIds || courseIds.length === 0) return {};
    
    const [results] = await sequelize.query(`
        SELECT cs.course_id, COUNT(DISTINCT css.student_id) as student_count
        FROM course_sections cs
        LEFT JOIN course_section_students css ON cs.id = css.course_section_id
        WHERE cs.course_id IN (?)
        GROUP BY cs.course_id
    `, {
        replacements: [courseIds],
    });
    
    const countMap = {};
    courseIds.forEach(id => { countMap[id] = 0; });
    results.forEach(row => {
        countMap[row.course_id] = parseInt(row.student_count);
    });
    
    return countMap;
};

/**
 * Get attendance stats for multiple sessions (optimized)
 * 
 * @param {Array} sessionIds - Array of session IDs
 * @returns {Object} - Map of session_id -> { present, late, leave, absent, total }
 */
const getAttendanceStatsBatch = async (sessionIds) => {
    if (!sessionIds || sessionIds.length === 0) return {};
    
    const [results] = await sequelize.query(`
        SELECT 
            attendance_session_id,
            status,
            COUNT(*) as count
        FROM attendance_records
        WHERE attendance_session_id IN (?)
        GROUP BY attendance_session_id, status
    `, {
        replacements: [sessionIds],
    });
    
    const statsMap = {};
    sessionIds.forEach(id => {
        statsMap[id] = {
            present: 0,
            late: 0,
            leave: 0,
            absent: 0,
            total: 0,
        };
    });
    
    results.forEach(row => {
        const id = row.attendance_session_id;
        const status = row.status;
        const count = parseInt(row.count);
        
        if (statsMap[id]) {
            statsMap[id][status] = count;
            statsMap[id].total += count;
        }
    });
    
    return statsMap;
};

/**
 * Get score statistics for multiple assignments (optimized)
 * 
 * @param {Array} assignmentIds - Array of assignment IDs
 * @returns {Object} - Map of assignment_id -> { count, avgScore, minScore, maxScore }
 */
const getScoreStatsBatch = async (assignmentIds) => {
    if (!assignmentIds || assignmentIds.length === 0) return {};
    
    const [results] = await sequelize.query(`
        SELECT 
            assignment_id,
            COUNT(DISTINCT COALESCE(student_id, group_id)) as scored_count,
            AVG(score) as avg_score,
            MIN(score) as min_score,
            MAX(score) as max_score
        FROM scores
        WHERE assignment_id IN (?)
          AND score IS NOT NULL
          AND sub_item_id IS NULL
        GROUP BY assignment_id
    `, {
        replacements: [assignmentIds],
    });
    
    const statsMap = {};
    assignmentIds.forEach(id => {
        statsMap[id] = {
            scoredCount: 0,
            avgScore: null,
            minScore: null,
            maxScore: null,
        };
    });
    
    results.forEach(row => {
        const id = row.assignment_id;
        statsMap[id] = {
            scoredCount: parseInt(row.scored_count),
            avgScore: row.avg_score ? parseFloat(row.avg_score) : null,
            minScore: row.min_score ? parseFloat(row.min_score) : null,
            maxScore: row.max_score ? parseFloat(row.max_score) : null,
        };
    });
    
    return statsMap;
};

module.exports = {
    batchCount,
    batchCountByStatus,
    batchSum,
    batchFetchLatest,
    batchExists,
    rawQuery,
    chunkedQuery,
    getStudentCountsByCourse,
    getAttendanceStatsBatch,
    getScoreStatsBatch,
};
