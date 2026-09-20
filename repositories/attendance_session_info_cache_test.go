package repositories

import (
	"fmt"
	"sync"
	"testing"
)

// GetSessionInfo coalesces concurrent callers for the same session through a
// singleflight.Group (plan.md ระยะ 1.1), which hands every waiter the exact
// same underlying *AttendanceSessionInfo. Handlers mutate their copy in place
// (GetSessionInfoHandler blanks PinCode before responding — see
// attendance_session_info_pin_test.go), so GetSessionInfo MUST return a fresh
// copy to each caller. If that copy is ever dropped, concurrent callers start
// sharing one struct and stomp each other's fields — this test drives enough
// concurrent callers to make that failure mode observable: every goroutine
// mutates a field unique to itself and then must still see its own value
// after every other goroutine has also run.
//
// This also covers the NESTED pointer fields (Course/Section), not just the
// top-level struct: an earlier version of this test only mutated the
// top-level Title string, which a plain shallow copy (`info := *shared`)
// already protects — it never would have caught a regression to that shallow
// form, because Course/Section are pointers a shallow copy still shares
// across every waiter (found in code review; fixed by
// (*AttendanceSessionInfo).clone() doing a real per-field deep copy).
func TestGetSessionInfo_ConcurrentCallersDoNotShareTheReturnedStruct(t *testing.T) {
	_, cleanup := setupAttendanceRuntimeTest(t)
	defer cleanup()

	createCourseFixture(t, "CP101", true)
	session := createAttendanceSessionFixture(t, false)

	const workers = 200
	addresses := make([]*AttendanceSessionInfo, workers)
	courseAddresses := make([]*AttendanceCourseBasic, workers)
	results := make([]string, workers)
	errs := make([]error, workers)

	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	done.Add(workers)

	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer done.Done()
			start.Wait() // release every goroutine at once to maximize overlap

			info, err := GetSessionInfo(session.ID)
			if err != nil {
				errs[idx] = err
				return
			}
			if info.Course == nil {
				errs[idx] = fmt.Errorf("info.Course is nil — fixture wiring broken, not what this test is checking")
				return
			}

			mutated := fmt.Sprintf("mutated-by-goroutine-%d", idx)
			info.Title = mutated                // top-level scalar field
			info.Course.Name = mutated           // nested pointer field — the gap a shallow copy misses

			addresses[idx] = info
			courseAddresses[idx] = info.Course
			results[idx] = mutated
		}(i)
	}
	start.Done() // fire the gate
	done.Wait()

	seenAddresses := make(map[*AttendanceSessionInfo]int, workers)
	seenCourseAddresses := make(map[*AttendanceCourseBasic]int, workers)
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: GetSessionInfo failed: %v", i, errs[i])
		}
		if addresses[i] == nil {
			t.Fatalf("goroutine %d: nil info with no error", i)
		}
		// Re-read the fields this goroutine set, AFTER every other goroutine
		// (including later-scheduled ones sharing a singleflight call) has
		// also mutated its own copy. If GetSessionInfo ever shares one
		// pointer (top-level or nested) across callers, a later goroutine's
		// mutation overwrites an earlier one's here and this fails.
		if addresses[i].Title != results[i] {
			t.Errorf("goroutine %d: expected Title %q, got %q — the returned *AttendanceSessionInfo is shared across concurrent GetSessionInfo callers", i, results[i], addresses[i].Title)
		}
		if addresses[i].Course.Name != results[i] {
			t.Errorf("goroutine %d: expected Course.Name %q, got %q — info.Course is shared across concurrent GetSessionInfo callers (shallow-copy regression)", i, results[i], addresses[i].Course.Name)
		}
		if prev, ok := seenAddresses[addresses[i]]; ok {
			t.Errorf("goroutine %d and %d received the SAME *AttendanceSessionInfo pointer from GetSessionInfo — concurrent callers must each get their own copy", prev, i)
		}
		seenAddresses[addresses[i]] = i
		if prev, ok := seenCourseAddresses[courseAddresses[i]]; ok {
			t.Errorf("goroutine %d and %d received the SAME *AttendanceCourseBasic pointer via info.Course — concurrent callers must each get their own copy of nested fields too", prev, i)
		}
		seenCourseAddresses[courseAddresses[i]] = i
	}
}
