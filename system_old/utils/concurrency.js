const logger = require('./logger');
/**
 * Concurrent Request Handler Utility
 * ป้องกันปัญหาจาก concurrent requests ที่อาจทำให้เกิด duplicate records หรือ race conditions
 */

const { sequelize } = require('../config/database');

/**
 * Request Lock Manager
 * ใช้สำหรับป้องกัน duplicate operations ที่เกิดจาก concurrent requests
 */
class RequestLockManager {
    constructor() {
        this.locks = new Map();
        this.lockTimeouts = new Map();
        this.DEFAULT_LOCK_TIMEOUT = 30000; // 30 seconds
    }

    /**
     * สร้าง lock key จาก operation parameters
     */
    createKey(operation, params) {
        return `${operation}:${JSON.stringify(params)}`;
    }

    /**
     * ขอ lock สำหรับ operation
     * @returns {boolean} - true ถ้าได้ lock, false ถ้ามีคนอื่น lock อยู่
     */
    async acquire(key, timeout = this.DEFAULT_LOCK_TIMEOUT) {
        // ถ้ามี lock อยู่แล้ว ให้ reject
        if (this.locks.has(key)) {
            const lockTime = this.locks.get(key);
            // Check if lock is expired
            if (Date.now() - lockTime < timeout) {
                return false;
            }
            // Lock expired, allow new lock
        }

        this.locks.set(key, Date.now());
        
        // Set auto-release timeout
        const existingTimeout = this.lockTimeouts.get(key);
        if (existingTimeout) {
            clearTimeout(existingTimeout);
        }
        
        this.lockTimeouts.set(key, setTimeout(() => {
            this.release(key);
        }, timeout));

        return true;
    }

    /**
     * ปล่อย lock
     */
    release(key) {
        this.locks.delete(key);
        const timeout = this.lockTimeouts.get(key);
        if (timeout) {
            clearTimeout(timeout);
            this.lockTimeouts.delete(key);
        }
    }

    /**
     * Execute with lock
     * @param {string} operation - ชื่อ operation (e.g., 'submit_score')
     * @param {Object} params - Parameters ที่ใช้สร้าง unique key
     * @param {Function} fn - Function ที่ต้องการ execute
     */
    async executeWithLock(operation, params, fn) {
        const key = this.createKey(operation, params);
        
        const acquired = await this.acquire(key);
        if (!acquired) {
            const error = new Error('Operation already in progress');
            error.code = 'DUPLICATE_REQUEST';
            throw error;
        }

        try {
            return await fn();
        } finally {
            this.release(key);
        }
    }
}

const lockManager = new RequestLockManager();

/**
 * Upsert helper - ใช้แทน findOrCreate เพื่อ handle concurrent inserts
 * 
 * @param {Model} model - Sequelize model
 * @param {Object} where - WHERE conditions
 * @param {Object} defaults - Default values for INSERT
 * @param {Object} updates - Values to UPDATE if exists
 * @param {Transaction} transaction - Optional transaction
 */
async function upsertRecord(model, where, defaults, updates = {}, transaction = null) {
    const options = { where, transaction };
    
    // Try to find existing record
    let record = await model.findOne(options);
    
    if (record) {
        // Update existing record
        await record.update(updates, { transaction });
        return { record, created: false };
    }
    
    // Try to create new record
    try {
        record = await model.create({ ...where, ...defaults }, { transaction });
        return { record, created: true };
    } catch (error) {
        // If duplicate key error, fetch the existing record
        if (error.name === 'SequelizeUniqueConstraintError') {
            record = await model.findOne(options);
            if (record) {
                await record.update(updates, { transaction });
                return { record, created: false };
            }
        }
        throw error;
    }
}

/**
 * Execute with retry - สำหรับ operations ที่อาจ fail เพราะ deadlock
 * 
 * @param {Function} fn - Async function to execute
 * @param {number} maxRetries - Maximum retry attempts
 * @param {number} baseDelay - Base delay in ms (will be multiplied by attempt number)
 */
async function executeWithRetry(fn, maxRetries = 3, baseDelay = 100) {
    let lastError;
    
    for (let attempt = 1; attempt <= maxRetries; attempt++) {
        try {
            return await fn();
        } catch (error) {
            lastError = error;
            
            // Check if error is retryable
            const isRetryable = 
                error.name === 'SequelizeDatabaseError' && 
                (error.parent?.code === 'ER_LOCK_DEADLOCK' || 
                 error.parent?.code === 'ER_LOCK_WAIT_TIMEOUT');
            
            if (!isRetryable || attempt === maxRetries) {
                throw error;
            }
            
            // Wait before retry (exponential backoff)
            const delay = baseDelay * Math.pow(2, attempt - 1);
            await new Promise(resolve => setTimeout(resolve, delay));
            
            logger.warn(`[Retry] Attempt ${attempt}/${maxRetries} after ${delay}ms delay`);
        }
    }
    
    throw lastError;
}

/**
 * Execute with transaction - Wrapper for safe transaction handling
 * 
 * @param {Function} fn - Function that receives transaction and performs DB operations
 * @param {Object} options - Transaction options
 */
async function executeWithTransaction(fn, options = {}) {
    const transaction = await sequelize.transaction(options);
    
    try {
        const result = await fn(transaction);
        await transaction.commit();
        return result;
    } catch (error) {
        await transaction.rollback();
        throw error;
    }
}

/**
 * Batch process - ประมวลผล items เป็นกลุ่มเพื่อลด DB load
 * 
 * @param {Array} items - Items to process
 * @param {number} batchSize - Size of each batch
 * @param {Function} processor - Async function to process each batch
 * @param {Object} options - Options (parallel: boolean)
 */
async function batchProcess(items, batchSize, processor, options = {}) {
    const { parallel = false } = options;
    const results = [];
    
    for (let i = 0; i < items.length; i += batchSize) {
        const batch = items.slice(i, i + batchSize);
        
        if (parallel) {
            const batchResults = await Promise.all(batch.map(item => processor(item)));
            results.push(...batchResults);
        } else {
            for (const item of batch) {
                const result = await processor(item);
                results.push(result);
            }
        }
    }
    
    return results;
}

/**
 * Debounce DB operations - รอให้ operations รวมกันก่อน execute
 */
class DebouncedBatcher {
    constructor(flushFn, options = {}) {
        this.flushFn = flushFn;
        this.delay = options.delay || 100;
        this.maxBatchSize = options.maxBatchSize || 100;
        this.items = [];
        this.timeout = null;
        this.promise = null;
        this.resolvers = [];
    }

    add(item) {
        return new Promise((resolve, reject) => {
            this.items.push(item);
            this.resolvers.push({ resolve, reject });
            
            if (this.items.length >= this.maxBatchSize) {
                this.flush();
            } else {
                this.scheduleFlush();
            }
        });
    }

    scheduleFlush() {
        if (this.timeout) {
            clearTimeout(this.timeout);
        }
        
        this.timeout = setTimeout(() => {
            this.flush();
        }, this.delay);
    }

    async flush() {
        if (this.items.length === 0) return;
        
        const items = this.items;
        const resolvers = this.resolvers;
        
        this.items = [];
        this.resolvers = [];
        
        if (this.timeout) {
            clearTimeout(this.timeout);
            this.timeout = null;
        }

        try {
            const results = await this.flushFn(items);
            resolvers.forEach(({ resolve }, index) => {
                resolve(results[index]);
            });
        } catch (error) {
            resolvers.forEach(({ reject }) => {
                reject(error);
            });
        }
    }
}

module.exports = {
    lockManager,
    upsertRecord,
    executeWithRetry,
    executeWithTransaction,
    batchProcess,
    DebouncedBatcher,
};
