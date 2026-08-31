package repositories

import (
	"strings"
	"testing"
)

func TestResolveAgeDaysUsesDefaultWhenEnvUnset(t *testing.T) {
	policy := RetentionPolicy{Table: "system_logs", EnvVar: "RETENTION_TEST_UNSET_DAYS", AgeDays: 90}
	if got := policy.resolveAgeDays(); got != 90 {
		t.Fatalf("expected default 90, got %d", got)
	}
}

func TestResolveAgeDaysHonoursEnvOverride(t *testing.T) {
	t.Setenv("RETENTION_TEST_DAYS", "30")
	policy := RetentionPolicy{Table: "system_logs", EnvVar: "RETENTION_TEST_DAYS", AgeDays: 90}
	if got := policy.resolveAgeDays(); got != 30 {
		t.Fatalf("expected override 30, got %d", got)
	}
}

// A typo in an env var must not turn into "delete almost everything". Both a
// non-numeric value and an aggressively small one have to be rejected, because
// this code path issues unconditional DELETEs against production log tables.
func TestResolveAgeDaysRejectsUnsafeValues(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"non numeric falls back to default", "ninety", 90},
		{"empty-ish falls back to default", "   ", 90},
		{"zero is clamped to the floor", "0", retentionMinAgeDays},
		{"negative is clamped to the floor", "-5", retentionMinAgeDays},
		{"below floor is clamped", "1", retentionMinAgeDays},
		{"exactly the floor is allowed", "7", retentionMinAgeDays},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("RETENTION_TEST_DAYS", testCase.value)
			policy := RetentionPolicy{Table: "system_logs", EnvVar: "RETENTION_TEST_DAYS", AgeDays: 90}
			if got := policy.resolveAgeDays(); got != testCase.want {
				t.Fatalf("value %q: expected %d, got %d", testCase.value, testCase.want, got)
			}
		})
	}
}

// Table and Where are interpolated straight into SQL, so the policy list is the
// only thing standing between this and an injection. Guard its shape.
func TestRetentionPoliciesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}

	for _, policy := range retentionPolicies() {
		if policy.Table == "" || policy.EnvVar == "" {
			t.Fatalf("policy %+v is missing Table or EnvVar", policy)
		}
		// One table may carry several windows (the activity log expires read
		// events sooner than changes), but each must be distinctly named.
		if seen[policy.Key()] {
			t.Fatalf("duplicate policy %q", policy.Key())
		}
		seen[policy.Key()] = true

		if policy.AgeDays < retentionMinAgeDays {
			t.Fatalf("policy for %q has AgeDays=%d, below the %d-day floor", policy.Table, policy.AgeDays, retentionMinAgeDays)
		}
		for _, forbidden := range []string{";", "--", "'", "/*"} {
			if strings.Contains(policy.Table, forbidden) || strings.Contains(policy.Where, forbidden) {
				t.Fatalf("policy for %q contains %q, which must never reach interpolated SQL", policy.Key(), forbidden)
			}
		}

		// Every placeholder in Where must have a bound argument, and no policy
		// may bind arguments it has no placeholders for.
		if got, want := strings.Count(policy.Where, "?"), len(policy.WhereArgs); got != want {
			t.Fatalf("policy for %q has %d placeholders but %d bound args", policy.Key(), got, want)
		}
	}

	if len(seen) == 0 {
		t.Fatal("no retention policies defined")
	}
}

func TestBuildPurgeStatement(t *testing.T) {
	plain := buildPurgeStatement(RetentionPolicy{Table: "system_logs"})
	want := "DELETE FROM system_logs WHERE ctid IN (SELECT ctid FROM system_logs WHERE created_at < ? LIMIT 5000)"
	if plain != want {
		t.Fatalf("unexpected statement:\n got: %s\nwant: %s", plain, want)
	}

	// The extra predicate must be ANDed inside the subquery, not appended after
	// the LIMIT where it would silently do nothing.
	withWhere := buildPurgeStatement(RetentionPolicy{Table: "user_notifications", Where: "is_read = true"})
	if !strings.Contains(withWhere, "created_at < ? AND is_read = true LIMIT") {
		t.Fatalf("Where clause not placed inside the subquery: %s", withWhere)
	}

	// Exactly one bind parameter: the cutoff. Anything else means a value that
	// should have been bound got interpolated instead.
	if got := strings.Count(withWhere, "?"); got != 1 {
		t.Fatalf("expected exactly 1 bind placeholder, got %d in %s", got, withWhere)
	}
}
