package repositories

import (
	"fmt"
	"itii-assist/config"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Retention for the append-only log tables. None of these had a purge before,
// so they grew for the lifetime of the deployment. The cost is not disk: a big
// high-churn table pushes autovacuum further and further behind (the default
// scale factor waits for dead tuples to reach 20% of an ever-larger table),
// bloat accumulates, and every query touching the table gets slower — the
// "it's fine after a restart, then slow again a few weeks later" pattern.
//
// Deletes run in bounded batches so a purge never takes a long lock or writes
// one enormous WAL transaction, and so the very first run against years of
// accumulated rows can be spread over several days instead of stalling the
// database for hours.

// courseAccessLogCategory mirrors services.CourseAccessCategory. It is repeated
// here rather than imported because services depends on repositories, and the
// value is bound as a parameter, never interpolated.
const courseAccessLogCategory = "access"

const (
	retentionBatchSize  = 5000
	retentionMaxBatches = 200 // ceiling per table per run: 1M rows
	retentionBatchPause = 200 * time.Millisecond
	retentionMinAgeDays = 7 // refuse to purge anything newer than this
)

type RetentionPolicy struct {
	// Table is interpolated into SQL, so it must never come from user input.
	// Every value lives in retentionPolicies() below.
	Table string
	// Name identifies the policy when one table has more than one window (the
	// activity log expires read events sooner than changes). Empty means Table.
	Name string
	// EnvVar overrides AgeDays at runtime, e.g. RETENTION_SYSTEM_LOGS_DAYS=30.
	EnvVar  string
	AgeDays int
	// Where is an extra predicate ANDed onto the age check. Same rule as Table:
	// literals only, and any value it compares against must be a `?` bound
	// through WhereArgs rather than written into the string.
	Where string
	// WhereArgs are bound to the placeholders in Where, in order.
	WhereArgs []any
}

// Key is the policy's identity for logging and for the uniqueness check in the
// tests. Two policies may share a table only if they have distinct names.
func (p RetentionPolicy) Key() string {
	if strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return p.Table
}

func retentionPolicies() []RetentionPolicy {
	return []RetentionPolicy{
		{Table: "system_logs", EnvVar: "RETENTION_SYSTEM_LOGS_DAYS", AgeDays: 90},
		// Read events (who opened what) are far more numerous than changes and
		// lose their value quickly, so they expire well before the change
		// history below. Listed first so these cheap rows go before the general
		// 180-day pass spends its batch ceiling on them.
		{
			Table:     "course_activity_logs",
			Name:      "course_activity_logs_access",
			EnvVar:    "RETENTION_COURSE_ACCESS_LOGS_DAYS",
			AgeDays:   90,
			Where:     "category = ?",
			WhereArgs: []any{courseAccessLogCategory},
		},
		{Table: "course_activity_logs", EnvVar: "RETENTION_COURSE_ACTIVITY_LOGS_DAYS", AgeDays: 180},
		{Table: "attendance_pin_histories", EnvVar: "RETENTION_ATTENDANCE_PIN_HISTORIES_DAYS", AgeDays: 30},
		{Table: "notification_logs", EnvVar: "RETENTION_NOTIFICATION_LOGS_DAYS", AgeDays: 60},
		{Table: "attendance_display_audit_logs", EnvVar: "RETENTION_DISPLAY_AUDIT_LOGS_DAYS", AgeDays: 90},
		// Unread notifications are kept regardless of age: they are still part
		// of the user's inbox, not history.
		{Table: "user_notifications", EnvVar: "RETENTION_USER_NOTIFICATIONS_DAYS", AgeDays: 90, Where: "is_read = true"},
	}
}

// resolveAgeDays applies the env override, refusing values that would delete
// recent data. A typo in an env var should not silently wipe this week's logs.
func (p RetentionPolicy) resolveAgeDays() int {
	raw := strings.TrimSpace(os.Getenv(p.EnvVar))
	if raw == "" {
		return p.AgeDays
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("⚠️  Invalid %s=%q, using default %d days", p.EnvVar, raw, p.AgeDays)
		return p.AgeDays
	}
	if parsed < retentionMinAgeDays {
		log.Printf("⚠️  %s=%d is below the %d-day floor, using %d days", p.EnvVar, parsed, retentionMinAgeDays, retentionMinAgeDays)
		return retentionMinAgeDays
	}
	return parsed
}

type RetentionResult struct {
	Table   string
	Deleted int64
	// Capped is true when the per-run batch ceiling was hit, meaning rows older
	// than the cutoff still remain and the next run will continue from there.
	Capped bool
}

// PurgeExpiredLogs deletes rows past their retention window, one table at a
// time, in batches. Failures are logged and the remaining tables still run —
// one broken table must not block the rest of the cleanup.
func PurgeExpiredLogs() []RetentionResult {
	if config.DB == nil {
		return nil
	}

	results := make([]RetentionResult, 0, len(retentionPolicies()))
	for _, policy := range retentionPolicies() {
		result, err := purgeTable(policy)
		if err != nil {
			log.Printf("⚠️  Retention purge failed for %s: %v", policy.Key(), err)
			continue
		}
		results = append(results, result)
	}
	return results
}

func purgeTable(policy RetentionPolicy) (RetentionResult, error) {
	cutoff := time.Now().AddDate(0, 0, -policy.resolveAgeDays())
	result := RetentionResult{Table: policy.Key()}

	statement := buildPurgeStatement(policy)
	args := append([]any{cutoff}, policy.WhereArgs...)

	for batch := 0; batch < retentionMaxBatches; batch++ {
		execution := config.DB.Exec(statement, args...)
		if execution.Error != nil {
			return result, execution.Error
		}

		result.Deleted += execution.RowsAffected
		if execution.RowsAffected < retentionBatchSize {
			return result, nil
		}

		// Give autovacuum and any concurrent request traffic room between
		// batches instead of holding the database down for the whole purge.
		time.Sleep(retentionBatchPause)
	}

	result.Capped = true
	return result, nil
}

// buildPurgeStatement renders the batched delete for one policy. Table and
// Where are interpolated rather than bound, so they must stay literals owned by
// retentionPolicies(); the cutoff timestamp and any WhereArgs are bound.
//
// ctid is PostgreSQL-specific and is used on purpose: it lets the subquery pick
// an arbitrary batch of matching rows without sorting, and without depending on
// an index that not all of these tables have on created_at.
func buildPurgeStatement(policy RetentionPolicy) string {
	predicate := "created_at < ?"
	if policy.Where != "" {
		predicate += " AND " + policy.Where
	}

	return fmt.Sprintf(
		`DELETE FROM %s WHERE ctid IN (SELECT ctid FROM %s WHERE %s LIMIT %d)`,
		policy.Table, policy.Table, predicate, retentionBatchSize,
	)
}
