package repositories

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"os"
	"strconv"
	"strings"
	"time"
)

// Cache key layout and TTL policy for the read-through caches.
//
// Every key is namespaced by a prefix so invalidation can wipe a whole family
// with CacheDeletePrefix. Keys are built here, next to the invalidation
// helpers, so a rename cannot leave the two sides disagreeing — a mismatch is
// silent, and shows up only as a user staring at a value they just changed.

const (
	cacheKeyCourseOverview = "cache:course:overview:"
	cacheKeyClassroomList  = "cache:classroom:list:"
)

// TTLs are the backstop, not the primary correctness mechanism — writes
// invalidate explicitly. They exist so that an invalidation path nobody
// remembered to wire up still self-heals within a bounded window.
func courseOverviewTTL() time.Duration {
	return cacheTTLFromEnv("CACHE_COURSE_OVERVIEW_SECONDS", 60)
}

func classroomListTTL() time.Duration {
	// Rooms and desk layouts change a handful of times a semester.
	return cacheTTLFromEnv("CACHE_CLASSROOM_LIST_SECONDS", 300)
}

// cacheTTLFromEnv reads a TTL override. A value of 0 disables that cache
// entirely, which is the intended escape hatch if a cached endpoint is ever
// suspected of serving something stale in production.
func cacheTTLFromEnv(name string, defaultSeconds int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return time.Duration(defaultSeconds) * time.Second
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return time.Duration(defaultSeconds) * time.Second
	}

	return time.Duration(parsed) * time.Second
}

func courseOverviewCacheKey(courseID string) string {
	return cacheKeyCourseOverview + courseID
}

// classroomListCacheKey hashes the parameter set rather than concatenating it.
// Search terms arrive from user input and can contain anything, including the
// ':' used as the key separator — hashing keeps one query's key from colliding
// with another's.
func classroomListCacheKey(params ClassroomListParams) string {
	// Every field of ClassroomListParams must appear here. Leaving one out
	// silently merges two different queries onto one entry — e.g. ascending and
	// descending results served interchangeably.
	//
	// Fields are length-prefixed rather than joined by a separator. Search and
	// Building are user input, so a plain join lets one field's content
	// impersonate a boundary: Search="a|b", Building="c" and Search="a",
	// Building="b|c" would otherwise produce byte-identical input and collide
	// onto a single cache entry.
	var builder strings.Builder
	fmt.Fprintf(&builder, "%d|%d|", params.Page, params.Limit)
	for _, field := range []string{
		params.Search, params.Building, params.ShowDeleted,
		params.SortBy, params.SortOrder,
	} {
		fmt.Fprintf(&builder, "%d:%s", len(field), field)
	}

	sum := sha1.Sum([]byte(builder.String()))
	return cacheKeyClassroomList + hex.EncodeToString(sum[:])
}

// InvalidateCourseOverviewCache drops the cached overview for one course. Call
// after anything that changes what the overview reports: scores, assignments,
// sections, enrolment or TA membership.
func InvalidateCourseOverviewCache(courseID string) {
	if strings.TrimSpace(courseID) == "" {
		return
	}
	config.CacheDelete(courseOverviewCacheKey(courseID))
}

// InvalidateClassroomListCache drops every cached classroom list. The lists are
// keyed by a hash of their filters, so there is no way to target only the pages
// containing a given room — and getting this wrong would leave a deleted room
// visible, so it clears the family.
func InvalidateClassroomListCache() {
	config.CacheDeletePrefix(cacheKeyClassroomList)
}

// InvalidateCourseOverviewCacheByAssignment resolves an assignment to its
// course and drops that course's cached overview.
//
// Scores carry only an assignment_id, so this extra lookup is what connects a
// grading write back to the cache entry it invalidates. It costs one indexed
// primary-key read per write, which is the right trade: the overview shows
// grades, and serving a stale one right after a TA submits would look like the
// score was lost.
func InvalidateCourseOverviewCacheByAssignment(assignmentID uint) {
	if assignmentID == 0 || !config.CacheAvailable() {
		return
	}

	var courseID string
	if err := config.DB.Model(&models.Assignment{}).
		Select("course_id").
		Where("id = ?", assignmentID).
		Scan(&courseID).Error; err != nil {
		// Falling back to a full sweep is deliberate: failing to invalidate is
		// worse than invalidating too much, and this path only runs on writes.
		config.CacheDeletePrefix(cacheKeyCourseOverview)
		return
	}

	InvalidateCourseOverviewCache(courseID)
}

// InvalidateCourseOverviewCacheBySection resolves a course section to its
// course and drops that course's cached overview. Enrolment writes are
// addressed by section id, so this is the bridge back to the cache key.
func InvalidateCourseOverviewCacheBySection(sectionID uint) {
	if sectionID == 0 || !config.CacheAvailable() {
		return
	}

	var courseID string
	if err := config.DB.Model(&models.CourseSection{}).
		Select("course_id").
		Where("id = ?", sectionID).
		Scan(&courseID).Error; err != nil {
		config.CacheDeletePrefix(cacheKeyCourseOverview)
		return
	}

	InvalidateCourseOverviewCache(courseID)
}
