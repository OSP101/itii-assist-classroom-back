package observability

import (
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// attendanceAuditCollector replaced 3 independent promauto.NewGaugeFunc
// callbacks (caught in review: each one took its own SnapshotAttendanceMetrics()
// call, meaning 3 separate locks on globalAttendanceMetrics.mu — the same
// mutex the check-in hot path locks on every request — per single /metrics
// scrape instead of 1). This proves the replacement Collector still exposes
// all 3 series correctly, sourced from one snapshot.
func TestAttendanceAuditCollector_EmitsAllThreeMetrics(t *testing.T) {
	// Delta-based rather than asserting absolute values: globalAttendanceMetrics
	// is a package-level singleton, so this stays correct regardless of what
	// ran before it in the same test binary.
	before := SnapshotAttendanceMetrics()

	RecordAttendanceAuditDropped("write")
	RecordAttendanceAuditDropped("write")
	RecordAttendanceAuditDropped("probe")
	RecordAttendanceGuardUnavailable()

	after := SnapshotAttendanceMetrics()
	if after.Audit.WritesDropped != before.Audit.WritesDropped+2 {
		t.Fatalf("expected WritesDropped to increase by 2, before=%d after=%d", before.Audit.WritesDropped, after.Audit.WritesDropped)
	}
	if after.Audit.ProbesDropped != before.Audit.ProbesDropped+1 {
		t.Fatalf("expected ProbesDropped to increase by 1, before=%d after=%d", before.Audit.ProbesDropped, after.Audit.ProbesDropped)
	}
	if after.Audit.GuardUnavailable != before.Audit.GuardUnavailable+1 {
		t.Fatalf("expected GuardUnavailable to increase by 1, before=%d after=%d", before.Audit.GuardUnavailable, after.Audit.GuardUnavailable)
	}

	collector := newAttendanceAuditCollector()
	expected := fmt.Sprintf(`
# HELP faculty_classroom_app_attendance_audit_writes_dropped Cumulative forensic check-in audit records discarded because the write lane was saturated (evidence loss, not just a stat).
# TYPE faculty_classroom_app_attendance_audit_writes_dropped gauge
faculty_classroom_app_attendance_audit_writes_dropped %d
# HELP faculty_classroom_app_attendance_audit_probes_dropped Cumulative device-flip correlation probes discarded because the probe lane was saturated.
# TYPE faculty_classroom_app_attendance_audit_probes_dropped gauge
faculty_classroom_app_attendance_audit_probes_dropped %d
# HELP faculty_classroom_app_attendance_guard_unavailable Cumulative check-ins rejected with 503 because the campus network guard could not resolve the session.
# TYPE faculty_classroom_app_attendance_guard_unavailable gauge
faculty_classroom_app_attendance_guard_unavailable %d
`, after.Audit.WritesDropped, after.Audit.ProbesDropped, after.Audit.GuardUnavailable)

	if err := testutil.CollectAndCompare(collector, strings.NewReader(expected)); err != nil {
		t.Fatalf("collected metrics did not match expected output: %v", err)
	}
}
