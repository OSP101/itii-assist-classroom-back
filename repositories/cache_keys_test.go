package repositories

import (
	"strings"
	"testing"
	"time"
)

func TestCourseOverviewCacheKeyIsScoped(t *testing.T) {
	key := courseOverviewCacheKey("abc123")

	if !strings.HasPrefix(key, cacheKeyCourseOverview) {
		t.Fatalf("key %q must carry the overview prefix so prefix invalidation reaches it", key)
	}
	if key == courseOverviewCacheKey("other") {
		t.Fatal("two different courses must not share a key")
	}
}

// Every field of ClassroomListParams has to affect the key. A field left out of
// the hash silently serves one query's results for another — the failure this
// guards against is ascending and descending lists being interchangeable.
func TestClassroomListCacheKeyCoversEveryField(t *testing.T) {
	base := ClassroomListParams{
		Page: 1, Limit: 20, Search: "lab", Building: "SC", ShowDeleted: "false",
		SortBy: "name", SortOrder: "ASC",
	}
	baseKey := classroomListCacheKey(base)

	variants := map[string]ClassroomListParams{
		"page":        {Page: 2, Limit: 20, Search: "lab", Building: "SC", ShowDeleted: "false", SortBy: "name", SortOrder: "ASC"},
		"limit":       {Page: 1, Limit: 50, Search: "lab", Building: "SC", ShowDeleted: "false", SortBy: "name", SortOrder: "ASC"},
		"search":      {Page: 1, Limit: 20, Search: "room", Building: "SC", ShowDeleted: "false", SortBy: "name", SortOrder: "ASC"},
		"building":    {Page: 1, Limit: 20, Search: "lab", Building: "EN", ShowDeleted: "false", SortBy: "name", SortOrder: "ASC"},
		"showDeleted": {Page: 1, Limit: 20, Search: "lab", Building: "SC", ShowDeleted: "true", SortBy: "name", SortOrder: "ASC"},
		"sortBy":      {Page: 1, Limit: 20, Search: "lab", Building: "SC", ShowDeleted: "false", SortBy: "building", SortOrder: "ASC"},
		"sortOrder":   {Page: 1, Limit: 20, Search: "lab", Building: "SC", ShowDeleted: "false", SortBy: "name", SortOrder: "DESC"},
	}

	for field, params := range variants {
		t.Run(field, func(t *testing.T) {
			if classroomListCacheKey(params) == baseKey {
				t.Fatalf("changing %s did not change the cache key", field)
			}
		})
	}
}

func TestClassroomListCacheKeyIsStable(t *testing.T) {
	params := ClassroomListParams{Page: 1, Limit: 20, Search: "lab"}

	if classroomListCacheKey(params) != classroomListCacheKey(params) {
		t.Fatal("identical params must produce identical keys or nothing ever hits")
	}
}

// Search text is user input and can contain the ':' used as a key separator.
// Hashing is what stops one query's key from colliding with another's.
func TestClassroomListCacheKeySeparatorInjection(t *testing.T) {
	a := classroomListCacheKey(ClassroomListParams{Search: "a|b", Building: "c"})
	b := classroomListCacheKey(ClassroomListParams{Search: "a", Building: "b|c"})

	if a == b {
		t.Fatal("params differing only in where the separator falls must not collide")
	}
}

// Every field of CourseListParams, plus userID and role, has to affect the
// key — GetMyCourses's result differs by all of them (role changes the join,
// userID scopes the membership, the rest filter/paginate/sort).
func TestMyCoursesCacheKeyCoversEveryField(t *testing.T) {
	baseParams := CourseListParams{
		Page: 1, Limit: 12, Search: "cp", Year: 2569, Semester: 1,
		Status: "active", SortBy: "year", SortOrder: "DESC",
	}
	baseKey := myCoursesCacheKey(1, "instructor", baseParams)

	if myCoursesCacheKey(2, "instructor", baseParams) == baseKey {
		t.Fatal("changing userID did not change the cache key")
	}
	if myCoursesCacheKey(1, "ta", baseParams) == baseKey {
		t.Fatal("changing role did not change the cache key")
	}

	variants := map[string]CourseListParams{
		"page":      {Page: 2, Limit: 12, Search: "cp", Year: 2569, Semester: 1, Status: "active", SortBy: "year", SortOrder: "DESC"},
		"limit":     {Page: 1, Limit: 10, Search: "cp", Year: 2569, Semester: 1, Status: "active", SortBy: "year", SortOrder: "DESC"},
		"search":    {Page: 1, Limit: 12, Search: "tc", Year: 2569, Semester: 1, Status: "active", SortBy: "year", SortOrder: "DESC"},
		"year":      {Page: 1, Limit: 12, Search: "cp", Year: 2568, Semester: 1, Status: "active", SortBy: "year", SortOrder: "DESC"},
		"semester":  {Page: 1, Limit: 12, Search: "cp", Year: 2569, Semester: 2, Status: "active", SortBy: "year", SortOrder: "DESC"},
		"status":    {Page: 1, Limit: 12, Search: "cp", Year: 2569, Semester: 1, Status: "inactive", SortBy: "year", SortOrder: "DESC"},
		"sortBy":    {Page: 1, Limit: 12, Search: "cp", Year: 2569, Semester: 1, Status: "active", SortBy: "code", SortOrder: "DESC"},
		"sortOrder": {Page: 1, Limit: 12, Search: "cp", Year: 2569, Semester: 1, Status: "active", SortBy: "year", SortOrder: "ASC"},
	}

	for field, params := range variants {
		t.Run(field, func(t *testing.T) {
			if myCoursesCacheKey(1, "instructor", params) == baseKey {
				t.Fatalf("changing %s did not change the cache key", field)
			}
		})
	}
}

func TestMyCoursesCacheKeyIsStable(t *testing.T) {
	params := CourseListParams{Page: 1, Limit: 12, Status: "active"}

	if myCoursesCacheKey(7, "student", params) != myCoursesCacheKey(7, "student", params) {
		t.Fatal("identical inputs must produce identical keys or nothing ever hits")
	}
}

func TestCacheTTLFromEnv(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"unset uses default", "", 60 * time.Second},
		{"override", "120", 120 * time.Second},
		{"zero disables the cache", "0", 0},
		{"negative falls back", "-5", 60 * time.Second},
		{"garbage falls back", "soon", 60 * time.Second},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("CACHE_TEST_TTL", testCase.value)
			if got := cacheTTLFromEnv("CACHE_TEST_TTL", 60); got != testCase.want {
				t.Fatalf("value %q: expected %s, got %s", testCase.value, testCase.want, got)
			}
		})
	}
}

// Invalidation runs on write paths that must not fail because Redis is absent.
func TestInvalidationIsSafeWithoutRedis(t *testing.T) {
	InvalidateCourseOverviewCache("course-1")
	InvalidateCourseOverviewCache("")
	InvalidateClassroomListCache()
	InvalidateCourseOverviewCacheByAssignment(0)
	InvalidateCourseOverviewCacheBySection(0)
	InvalidateMyCoursesCache()
}
