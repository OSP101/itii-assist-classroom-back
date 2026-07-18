// Command repair-orphan-scores re-attaches scores that were detached when
// UpdateAssignment hard-deleted and re-created an assignment's sub-items under
// new IDs. The scores were never deleted, only unreachable.
//
// It reports by default and only writes when --apply is passed. Read the report
// before applying: generations it cannot place are listed rather than guessed at.
//
//	go run ./cmd/repair-orphan-scores --course-id=<id>
//	go run ./cmd/repair-orphan-scores --course-id=<id> --apply
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"itii-assist/config"
	"itii-assist/repositories"

	"github.com/joho/godotenv"
)

func main() {
	courseID := flag.String("course-id", "", "limit the repair to one course (default: every course)")
	apply := flag.Bool("apply", false, "write the changes; without this the run only reports")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found (using system environment variables)")
	}
	config.ConnectDB()

	plans, err := repositories.PlanOrphanScoreRepair(*courseID)
	if err != nil {
		log.Fatalf("❌ Failed to inspect orphaned scores: %v", err)
	}

	if len(plans) == 0 {
		fmt.Println("✅ No orphaned scores found — nothing to repair.")
		return
	}

	totals := printReport(plans)

	if !*apply {
		fmt.Println("\nDry run — nothing was changed.")
		fmt.Println("Back up the database (pg_dump) before re-running with --apply.")
		return
	}

	if totals.remappableRows == 0 {
		fmt.Println("\nNothing can be re-attached automatically. Not applying.")
		os.Exit(1)
	}

	fmt.Println("\n⏳ Applying…")
	result, err := repositories.ApplyOrphanScoreRepair(plans)
	if err != nil {
		log.Fatalf("❌ Repair failed and was rolled back: %v", err)
	}

	fmt.Printf("✅ Re-attached %d score(s).\n", result.Remapped)
	fmt.Printf("   Skipped %d (the student already had a score on the current sub-item).\n", result.Skipped)
	fmt.Printf("   Superseded %d (an older duplicate for the same cell).\n", result.Superseded)
	fmt.Println("   Every row touched was copied to scores_orphan_repair_backup first.")
}

type reportTotals struct {
	orphanRows      int64
	remappableRows  int
	unmappableCount int
}

func printReport(plans []repositories.AssignmentOrphanPlan) reportTotals {
	var totals reportTotals

	fmt.Printf("Found orphaned scores on %d assignment(s):\n", len(plans))
	for _, plan := range plans {
		fmt.Printf("\n── Assignment #%d  %s  (course %s)\n", plan.AssignmentID, plan.AssignmentName, plan.CourseID)
		fmt.Printf("   Current sub-items: %d", len(plan.LiveSubItems))
		for i, item := range plan.LiveSubItems {
			fmt.Printf("\n     %d. id=%d  %s", i+1, item.ID, item.Name)
		}
		fmt.Printf("\n   Orphaned score rows: %d\n", plan.OrphanRowCount)
		totals.orphanRows += plan.OrphanRowCount

		for _, generation := range plan.Generations {
			switch {
			case generation.Mappable:
				fmt.Printf("   ✅ generation %v — %d ids, matches the sub-item count; will be mapped in order\n",
					generation.SubItemIDs, len(generation.SubItemIDs))
			case generation.LikelyGenerations > 1:
				fmt.Printf("   ⚠️  generation %v — %d ids, looks like %d generations of %d run together; NOT mapped, needs a decision\n",
					generation.SubItemIDs, len(generation.SubItemIDs), generation.LikelyGenerations, len(plan.LiveSubItems))
				totals.unmappableCount++
			default:
				fmt.Printf("   ⚠️  generation %v — %d ids against %d sub-items; positions cannot be inferred, NOT mapped\n",
					generation.SubItemIDs, len(generation.SubItemIDs), len(plan.LiveSubItems))
				totals.unmappableCount++
			}
		}

		for _, remap := range plan.Remaps {
			fmt.Printf("      %d → %d  (%s)\n", remap.FromSubItemID, remap.ToSubItemID, remap.ToName)
		}
		totals.remappableRows += len(plan.Remaps)
	}

	fmt.Printf("\nSummary: %d orphaned row(s) across %d assignment(s); %d sub-item id(s) can be re-attached automatically",
		totals.orphanRows, len(plans), totals.remappableRows)
	if totals.unmappableCount > 0 {
		fmt.Printf("; %d generation(s) need a manual decision", totals.unmappableCount)
	}
	fmt.Println(".")
	return totals
}
