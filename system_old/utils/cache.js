/**
 * Cache Utility for Performance Optimization
 * Simple in-memory cache with TTL support
 */

class CacheManager {
    constructor() {
        this.cache = new Map();
        this.stats = {
            hits: 0,
            misses: 0,
        };
        
        // Cleanup expired entries every 5 minutes
        this.cleanupInterval = setInterval(() => {
            this.cleanup();
        }, 5 * 60 * 1000);
    }

    /**
     * Get value from cache
     * @param {string} key - Cache key
     * @returns {any|null} - Cached value or null if not found/expired
     */
    get(key) {
        const entry = this.cache.get(key);
        
        if (!entry) {
            this.stats.misses++;
            return null;
        }
        
        if (entry.expiry && Date.now() > entry.expiry) {
            this.cache.delete(key);
            this.stats.misses++;
            return null;
        }
        
        this.stats.hits++;
        return entry.value;
    }

    /**
     * Set value in cache
     * @param {string} key - Cache key
     * @param {any} value - Value to cache
     * @param {number} ttlMs - Time to live in milliseconds (default: 60 seconds)
     */
    set(key, value, ttlMs = 60000) {
        this.cache.set(key, {
            value,
            expiry: ttlMs > 0 ? Date.now() + ttlMs : null,
            createdAt: Date.now(),
        });
    }

    /**
     * Delete a key from cache
     * @param {string} key - Cache key
     */
    delete(key) {
        this.cache.delete(key);
    }

    /**
     * Delete all keys matching a pattern
     * @param {string} pattern - Pattern to match (e.g., 'course_*')
     */
    deletePattern(pattern) {
        const regex = new RegExp('^' + pattern.replace(/\*/g, '.*') + '$');
        for (const key of this.cache.keys()) {
            if (regex.test(key)) {
                this.cache.delete(key);
            }
        }
    }

    /**
     * Clear all cache
     */
    clear() {
        this.cache.clear();
    }

    /**
     * Get or set with callback
     * @param {string} key - Cache key
     * @param {Function} fetchFn - Async function to fetch data if not cached
     * @param {number} ttlMs - Time to live in milliseconds
     * @returns {Promise<any>} - Cached or fetched value
     */
    async getOrSet(key, fetchFn, ttlMs = 60000) {
        const cached = this.get(key);
        if (cached !== null) {
            return cached;
        }
        
        const value = await fetchFn();
        this.set(key, value, ttlMs);
        return value;
    }

    /**
     * Remove expired entries
     */
    cleanup() {
        const now = Date.now();
        for (const [key, entry] of this.cache.entries()) {
            if (entry.expiry && now > entry.expiry) {
                this.cache.delete(key);
            }
        }
    }

    /**
     * Get cache statistics
     */
    getStats() {
        return {
            ...this.stats,
            size: this.cache.size,
            hitRate: this.stats.hits + this.stats.misses > 0 
                ? ((this.stats.hits / (this.stats.hits + this.stats.misses)) * 100).toFixed(2) + '%'
                : '0%',
        };
    }

    /**
     * Shutdown cache manager
     */
    shutdown() {
        if (this.cleanupInterval) {
            clearInterval(this.cleanupInterval);
        }
        this.clear();
    }
}

// Singleton instance
const cache = new CacheManager();

// Cache TTL constants (in milliseconds)
const CACHE_TTL = {
    SHORT: 30 * 1000,        // 30 seconds - for frequently changing data
    MEDIUM: 60 * 1000,       // 1 minute - default
    LONG: 5 * 60 * 1000,     // 5 minutes - for stable data
    VERY_LONG: 15 * 60 * 1000, // 15 minutes - for rarely changing data
};

module.exports = {
    cache,
    CacheManager,
    CACHE_TTL,
};
