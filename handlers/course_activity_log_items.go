package handlers

import (
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// Bulk action item lists
//
// Bulk actions used to log only a count: "bulk_add_students -> added: 30" told
// an instructor that something happened to thirty people but never which
// thirty. These helpers attach the actual list, which the resolver then turns
// into names (see detailKeyRefTypes) so the drill-down reads as a roster rather
// than a row of numbers.
//
// The lists are stored on the one summary row rather than written as a row per
// item: an import of several hundred students would otherwise bury every other
// event in the timeline, and the same information is one click away either way.
// =============================================================================

// maxLoggedBatchItems bounds a single list. A class of a few hundred fits
// comfortably; anything beyond that is a data import whose full membership
// belongs in the roster, not in an audit line.
const maxLoggedBatchItems = 200

// withItemIDs attaches an ID list under key, truncating a very long one and
// recording that it was truncated. A silently shortened list would read as the
// complete set and quietly misstate what happened.
func withItemIDs(detail fiber.Map, key string, ids []uint) fiber.Map {
	if len(ids) == 0 {
		return detail
	}
	if detail == nil {
		detail = fiber.Map{}
	}

	if len(ids) > maxLoggedBatchItems {
		detail[key] = ids[:maxLoggedBatchItems]
		detail[key+"_truncated"] = len(ids) - maxLoggedBatchItems
		return detail
	}

	detail[key] = ids
	return detail
}

// withItemEntries attaches a list of per-item objects (a student and the score
// they were given, say) under the same truncation rule as withItemIDs.
func withItemEntries(detail fiber.Map, key string, entries []fiber.Map) fiber.Map {
	if len(entries) == 0 {
		return detail
	}
	if detail == nil {
		detail = fiber.Map{}
	}

	if len(entries) > maxLoggedBatchItems {
		detail[key] = entries[:maxLoggedBatchItems]
		detail[key+"_truncated"] = len(entries) - maxLoggedBatchItems
		return detail
	}

	detail[key] = entries
	return detail
}

// userIDsOf pulls the IDs out of the user records a bulk membership change
// returned, so the log can name who was actually added rather than how many.
func userIDsOf(users []repositories.UserBasic) []uint {
	ids := make([]uint, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	return ids
}

// teamIDsOf pulls the IDs out of the teams a bulk create or randomise produced.
func teamIDsOf(teams []repositories.TeamWithMembers) []uint {
	ids := make([]uint, 0, len(teams))
	for _, team := range teams {
		ids = append(ids, team.ID)
	}
	return ids
}
