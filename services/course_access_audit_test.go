package services

import (
	"sync"
	"testing"
	"time"
)

func TestCourseViewDedupeSuppressesRepeatsWithinWindow(t *testing.T) {
	cache := courseViewDedupe{seen: map[string]time.Time{}}
	ttl := 10 * time.Minute
	key := courseViewDedupeKey(7, "CS101", ActionViewScores)
	start := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)

	if !cache.admit(key, start, ttl) {
		t.Fatal("first view must be recorded")
	}
	// A page opening fires several GETs back to back.
	if cache.admit(key, start.Add(200*time.Millisecond), ttl) {
		t.Fatal("burst within the window must be suppressed")
	}
	if cache.admit(key, start.Add(9*time.Minute), ttl) {
		t.Fatal("repeat just inside the window must be suppressed")
	}
	// A visit later in the day is a genuinely separate one.
	if !cache.admit(key, start.Add(11*time.Minute), ttl) {
		t.Fatal("view after the window must be recorded")
	}
}

func TestCourseViewDedupeSeparatesActorCourseAndAction(t *testing.T) {
	cache := courseViewDedupe{seen: map[string]time.Time{}}
	ttl := 10 * time.Minute
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)

	if !cache.admit(courseViewDedupeKey(7, "CS101", ActionViewScores), now, ttl) {
		t.Fatal("baseline view must be recorded")
	}
	for _, key := range []string{
		courseViewDedupeKey(8, "CS101", ActionViewScores),     // another admin
		courseViewDedupeKey(7, "CS102", ActionViewScores),     // another course
		courseViewDedupeKey(7, "CS101", ActionViewAttendance), // another action
	} {
		if !cache.admit(key, now, ttl) {
			t.Fatalf("key %q must not be suppressed by an unrelated view", key)
		}
	}
}

func TestCourseViewDedupeSweepsExpiredKeys(t *testing.T) {
	cache := courseViewDedupe{seen: map[string]time.Time{}}
	ttl := time.Minute
	old := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)

	for i := 0; i < courseViewSweepThreshold; i++ {
		cache.admit(courseViewDedupeKey(uint(i+1), "CS101", ActionViewCourse), old, ttl)
	}
	if len(cache.seen) != courseViewSweepThreshold {
		t.Fatalf("expected %d entries, got %d", courseViewSweepThreshold, len(cache.seen))
	}

	// The next admit crosses the threshold and must drop the expired entries
	// rather than letting the map grow without bound.
	cache.admit(courseViewDedupeKey(999999, "CS101", ActionViewCourse), old.Add(2*time.Minute), ttl)
	if len(cache.seen) != 1 {
		t.Fatalf("expected the sweep to leave only the fresh entry, got %d", len(cache.seen))
	}
}

func TestCourseViewDedupeAdmitsOnlyOnceUnderConcurrency(t *testing.T) {
	cache := courseViewDedupe{seen: map[string]time.Time{}}
	ttl := 10 * time.Minute
	key := courseViewDedupeKey(7, "CS101", ActionViewCourse)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cache.admit(key, now, ttl) {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if admitted != 1 {
		t.Fatalf("concurrent requests must produce exactly one record, got %d", admitted)
	}
}

func TestCourseViewDedupeKeyIsUnambiguous(t *testing.T) {
	// Course IDs are NanoIDs and actions are fixed identifiers, but the key is
	// built by concatenation, so guard against a split that could collide.
	a := courseViewDedupeKey(1, "2|view_course", ActionViewScores)
	b := courseViewDedupeKey(1, "2", "view_course|"+ActionViewScores)
	if a == b {
		t.Fatalf("distinct triples produced the same key: %q", a)
	}
	if want := "1|5:CS101|" + ActionViewCourse; courseViewDedupeKey(1, "CS101", ActionViewCourse) != want {
		t.Fatalf("unexpected key format: %q", courseViewDedupeKey(1, "CS101", ActionViewCourse))
	}
}
