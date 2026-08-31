package handlers

import (
	"encoding/json"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

// =============================================================================
// Activity log detail resolver
//
// Logs store raw foreign keys inside their JSON detail (student_id, section_id,
// assignment_id, ...). The UI cannot turn those numbers into anything readable,
// so we hydrate them here: every ID on the current page is collected first, then
// looked up with one batched query per entity type (never per row).
// =============================================================================

// Entity types produced by the resolver. The frontend uses these to decide how
// to label a reference, so keep the strings stable.
const (
	refStudent           = "student"
	refUser              = "user"
	refSection           = "section"
	refAssignment        = "assignment"
	refSubItem           = "sub_item"
	refGroup             = "group"
	refClassroom         = "classroom"
	refExamSetting       = "exam_setting"
	refAttendanceSession = "attendance_session"
	refQueueSession      = "queue_session"
)

// resolvedRef is a display-ready reference to an entity.
type resolvedRef struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
}

// detailKeyRefTypes maps a key inside the detail JSON to the entity its value
// points at. Keys not listed here are passed through untouched.
var detailKeyRefTypes = map[string]string{
	"student_id":                   refStudent,
	"student_ids":                  refStudent,
	"added_student_ids":            refStudent,
	"moved_student_ids":            refStudent,
	"skipped_student_ids":          refStudent,
	"added_user_ids":               refUser,
	"created_team_ids":             refGroup,
	"deleted_team_ids":             refGroup,
	"user_id":                      refUser,
	"member_ids":                   refStudent,
	"instructor_ids":               refUser,
	"section_id":                   refSection,
	"from_section_id":              refSection,
	"to_section_id":                refSection,
	"target_section_id":            refSection,
	"course_section_ids":           refSection,
	"assignment_id":                refAssignment,
	"linked_assignment_id":         refAssignment,
	"origin_assignment_id":         refAssignment,
	"ordered_ids":                  refAssignment,
	"sub_item_id":                  refSubItem,
	"group_id":                     refGroup,
	"team_id":                      refGroup,
	"team_ids":                     refGroup,
	"classroom_id":                 refClassroom,
	"classroom_ids":                refClassroom,
	"exam_setting_id":              refExamSetting,
	"session_ids":                  refAttendanceSession,
	"attendance_session_id":        refAttendanceSession,
	"linked_attendance_session_id": refAttendanceSession,
	"queue_session_id":             refQueueSession,
}

// targetTypeRefTypes maps CourseActivityLog.TargetType to a resolvable entity.
// Target types with no lookup value (course, exam_seat, queue_booking, ...) are
// intentionally absent.
var targetTypeRefTypes = map[string]string{
	"student":            refStudent,
	"user":               refUser,
	"ta":                 refUser,
	"section":            refSection,
	"assignment":         refAssignment,
	"team":               refGroup,
	"exam_setting":       refExamSetting,
	"attendance_session": refAttendanceSession,
	"queue_session":      refQueueSession,
}

// nestedListRule describes a detail key holding a list of objects, where one
// field inside each object is a foreign key. Grading through the queue writes
// sub_item_scores this way: [{"sub_item_id": 6, "score": 10}].
type nestedListRule struct {
	IDKey   string
	RefType string
}

var nestedListRefKeys = map[string]nestedListRule{
	"sub_item_scores": {IDKey: "sub_item_id", RefType: refSubItem},
	"graded_scores":   {IDKey: "student_id", RefType: refStudent},
	"record_updates":  {IDKey: "student_id", RefType: refStudent},
}

// changesDetailKey holds the before/after diff written by withChanges, shaped as
// {field: {"from": ..., "to": ...}}. Its fields are looked up in
// detailKeyRefTypes just like top-level keys, so a section or assignment moving
// from one value to another resolves to names on both sides.
const changesDetailKey = "changes"

// -----------------------------------------------------------------------------
// ID collection
// -----------------------------------------------------------------------------

type refCollector struct {
	ids map[string]map[string]struct{} // refType -> set of raw id strings
}

func newRefCollector() *refCollector {
	return &refCollector{ids: map[string]map[string]struct{}{}}
}

func (rc *refCollector) add(refType string, raw string) {
	raw = strings.TrimSpace(raw)
	if refType == "" || raw == "" || raw == "0" || raw == "<nil>" {
		return
	}
	if _, ok := rc.ids[refType]; !ok {
		rc.ids[refType] = map[string]struct{}{}
	}
	rc.ids[refType][raw] = struct{}{}
}

// addValue accepts the raw JSON value of a detail key, which may be a single ID
// or an array of IDs.
func (rc *refCollector) addValue(refType string, value interface{}) {
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			rc.addValue(refType, item)
		}
	case float64:
		rc.add(refType, strconv.FormatInt(int64(typed), 10))
	case string:
		rc.add(refType, typed)
	case json.Number:
		rc.add(refType, typed.String())
	}
}

// addNestedList collects the foreign key held inside each object of a list.
func (rc *refCollector) addNestedList(rule nestedListRule, value interface{}) {
	list, ok := value.([]interface{})
	if !ok {
		return
	}
	for _, item := range list {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		rc.addValue(rule.RefType, entry[rule.IDKey])
	}
}

// addChanges collects the foreign keys on both sides of a before/after diff.
func (rc *refCollector) addChanges(value interface{}) {
	changes, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	for field, raw := range changes {
		refType, ok := detailKeyRefTypes[field]
		if !ok {
			continue
		}
		pair, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		rc.addValue(refType, pair["from"])
		rc.addValue(refType, pair["to"])
	}
}

func (rc *refCollector) list(refType string) []string {
	set := rc.ids[refType]
	if len(set) == 0 {
		return nil
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	return result
}

// numericList returns the collected IDs of a type parsed as uint, dropping any
// value that is not a positive integer.
func (rc *refCollector) numericList(refType string) []uint {
	raw := rc.list(refType)
	result := make([]uint, 0, len(raw))
	for _, id := range raw {
		parsed, err := strconv.ParseUint(id, 10, 64)
		if err != nil || parsed == 0 {
			continue
		}
		result = append(result, uint(parsed))
	}
	return result
}

// -----------------------------------------------------------------------------
// Batched lookups
// -----------------------------------------------------------------------------

// refIndex holds every resolved entity for one page of logs, keyed by
// refType -> id string -> reference.
type refIndex map[string]map[string]resolvedRef

func (ri refIndex) put(refType, id string, ref resolvedRef) {
	if _, ok := ri[refType]; !ok {
		ri[refType] = map[string]resolvedRef{}
	}
	ri[refType][id] = ref
}

func (ri refIndex) lookup(refType string, value interface{}) (resolvedRef, bool) {
	var id string
	switch typed := value.(type) {
	case float64:
		id = strconv.FormatInt(int64(typed), 10)
	case string:
		id = strings.TrimSpace(typed)
	case json.Number:
		id = typed.String()
	default:
		return resolvedRef{}, false
	}

	byID, ok := ri[refType]
	if !ok {
		return resolvedRef{}, false
	}
	ref, ok := byID[id]
	return ref, ok
}

func uintKey(id uint) string { return strconv.FormatUint(uint64(id), 10) }

// buildRefIndex runs one query per entity type that actually appeared in the
// current page of logs. Missing rows (deleted entities) simply stay unresolved.
func buildRefIndex(rc *refCollector) refIndex {
	index := refIndex{}

	if ids := rc.numericList(refStudent); len(ids) > 0 {
		var rows []models.Student
		if err := config.DB.Select("id", "student_id", "full_name").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refStudent, uintKey(row.ID), resolvedRef{
					Type:  refStudent,
					ID:    uintKey(row.ID),
					Label: row.FullName,
					Sub:   row.StudentID,
				})
			}
		}
	}

	if ids := rc.numericList(refUser); len(ids) > 0 {
		var rows []models.User
		if err := config.DB.Select("id", "full_name", "email").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refUser, uintKey(row.ID), resolvedRef{
					Type:  refUser,
					ID:    uintKey(row.ID),
					Label: row.FullName,
					Sub:   row.Email,
				})
			}
		}
	}

	if ids := rc.numericList(refSection); len(ids) > 0 {
		var rows []models.CourseSection
		if err := config.DB.Select("id", "section_no").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refSection, uintKey(row.ID), resolvedRef{
					Type:  refSection,
					ID:    uintKey(row.ID),
					Label: row.SectionNo,
				})
			}
		}
	}

	if ids := rc.numericList(refAssignment); len(ids) > 0 {
		var rows []models.Assignment
		if err := config.DB.Select("id", "name").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refAssignment, uintKey(row.ID), resolvedRef{
					Type:  refAssignment,
					ID:    uintKey(row.ID),
					Label: row.Name,
				})
			}
		}
	}

	if ids := rc.numericList(refSubItem); len(ids) > 0 {
		var rows []models.AssignmentSubItem
		if err := config.DB.Select("id", "name").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refSubItem, uintKey(row.ID), resolvedRef{
					Type:  refSubItem,
					ID:    uintKey(row.ID),
					Label: row.Name,
				})
			}
		}
	}

	if ids := rc.numericList(refGroup); len(ids) > 0 {
		var rows []models.StudentGroup
		if err := config.DB.Select("id", "name", "group_type").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refGroup, uintKey(row.ID), resolvedRef{
					Type:  refGroup,
					ID:    uintKey(row.ID),
					Label: row.Name,
					Sub:   row.GroupType,
				})
			}
		}
	}

	if ids := rc.numericList(refExamSetting); len(ids) > 0 {
		var rows []models.ExamSetting
		if err := config.DB.Select("id", "exam_type", "component").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refExamSetting, uintKey(row.ID), resolvedRef{
					Type:  refExamSetting,
					ID:    uintKey(row.ID),
					Label: row.ExamType,
					Sub:   row.Component,
				})
			}
		}
	}

	if ids := rc.numericList(refAttendanceSession); len(ids) > 0 {
		var rows []models.AttendanceSession
		if err := config.DB.Select("id", "title").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refAttendanceSession, uintKey(row.ID), resolvedRef{
					Type:  refAttendanceSession,
					ID:    uintKey(row.ID),
					Label: row.Title,
				})
			}
		}
	}

	if ids := rc.list(refClassroom); len(ids) > 0 {
		var rows []models.Classroom
		if err := config.DB.Select("id", "name", "building").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refClassroom, row.ID, resolvedRef{
					Type:  refClassroom,
					ID:    row.ID,
					Label: row.Name,
					Sub:   row.Building,
				})
			}
		}
	}

	if ids := rc.list(refQueueSession); len(ids) > 0 {
		var rows []models.QueueSession
		if err := config.DB.Select("id", "title").Where("id IN ?", ids).Find(&rows).Error; err == nil {
			for _, row := range rows {
				index.put(refQueueSession, row.ID, resolvedRef{
					Type:  refQueueSession,
					ID:    row.ID,
					Label: row.Title,
				})
			}
		}
	}

	return index
}

// -----------------------------------------------------------------------------
// Per-log resolution
// -----------------------------------------------------------------------------

func decodeActivityDetail(raw datatypes.JSON) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return decoded
}

// resolveDetail returns a map mirroring the detail keys that point at entities,
// with each value replaced by its resolved reference (or list of references).
// Keys whose entities could not be found are omitted so the frontend can fall
// back to printing the raw value.
func resolveDetail(detail map[string]interface{}, index refIndex) map[string]interface{} {
	if len(detail) == 0 {
		return nil
	}

	resolved := map[string]interface{}{}
	for key, value := range detail {
		if key == changesDetailKey {
			if changes := resolveChanges(value, index); changes != nil {
				resolved[key] = changes
			}
			continue
		}

		// Lists of objects resolve positionally, so the frontend can pair each
		// entry's own fields (a score, say) with its resolved entity.
		if rule, isNested := nestedListRefKeys[key]; isNested {
			list, isList := value.([]interface{})
			if !isList {
				continue
			}
			refs := make([]interface{}, 0, len(list))
			found := false
			for _, item := range list {
				entry, ok := item.(map[string]interface{})
				if !ok {
					refs = append(refs, nil)
					continue
				}
				if ref, ok := index.lookup(rule.RefType, entry[rule.IDKey]); ok {
					refs = append(refs, ref)
					found = true
					continue
				}
				refs = append(refs, nil)
			}
			if found {
				resolved[key] = refs
			}
			continue
		}

		refType, ok := detailKeyRefTypes[key]
		if !ok {
			continue
		}

		if list, isList := value.([]interface{}); isList {
			refs := make([]resolvedRef, 0, len(list))
			for _, item := range list {
				if ref, found := index.lookup(refType, item); found {
					refs = append(refs, ref)
				}
			}
			if len(refs) > 0 {
				resolved[key] = refs
			}
			continue
		}

		if ref, found := index.lookup(refType, value); found {
			resolved[key] = ref
		}
	}

	if len(resolved) == 0 {
		return nil
	}
	return resolved
}

// resolveChanges hydrates the foreign keys on either side of a before/after
// diff, keeping the {field: {from, to}} shape so the frontend can render the
// pair. Sides that resolve to nothing are omitted, and a field with no
// resolvable side at all is left out entirely so the raw values still show.
func resolveChanges(value interface{}, index refIndex) map[string]interface{} {
	changes, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}

	resolved := map[string]interface{}{}
	for field, raw := range changes {
		refType, ok := detailKeyRefTypes[field]
		if !ok {
			continue
		}
		pair, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		sides := map[string]interface{}{}
		if ref, found := index.lookup(refType, pair["from"]); found {
			sides["from"] = ref
		}
		if ref, found := index.lookup(refType, pair["to"]); found {
			sides["to"] = ref
		}
		if len(sides) > 0 {
			resolved[field] = sides
		}
	}

	if len(resolved) == 0 {
		return nil
	}
	return resolved
}

// -----------------------------------------------------------------------------
// Item builder shared by the list and export handlers
// -----------------------------------------------------------------------------

// buildActivityLogItems turns raw log rows into the API payload: actor profile,
// parsed device info, and resolved entity references for the detail JSON.
func buildActivityLogItems(logs []models.CourseActivityLog) ([]fiber.Map, error) {
	actorIDs := make([]uint, 0, len(logs))
	actorSet := map[uint]struct{}{}
	collector := newRefCollector()
	details := make([]map[string]interface{}, len(logs))

	for i, log := range logs {
		// Actor 0 marks an event no user account performed (a student's blocked
		// check-in, say); there is no profile to fetch for it.
		if _, ok := actorSet[log.ActorUserID]; !ok && log.ActorUserID != 0 {
			actorSet[log.ActorUserID] = struct{}{}
			actorIDs = append(actorIDs, log.ActorUserID)
		}

		detail := decodeActivityDetail(log.Detail)
		details[i] = detail
		for key, value := range detail {
			if key == changesDetailKey {
				collector.addChanges(value)
				continue
			}
			if rule, ok := nestedListRefKeys[key]; ok {
				collector.addNestedList(rule, value)
				continue
			}
			if refType, ok := detailKeyRefTypes[key]; ok {
				collector.addValue(refType, value)
			}
		}

		if refType, ok := targetTypeRefTypes[log.TargetType]; ok {
			collector.add(refType, log.TargetID)
		}
	}

	usersByID, err := fetchUsersByID(actorIDs)
	if err != nil {
		return nil, err
	}

	index := buildRefIndex(collector)

	items := make([]fiber.Map, 0, len(logs))
	for i, log := range logs {
		deviceType, browser, osName := utils.ParseUserAgent(log.UserAgent)

		item := fiber.Map{
			"id":            log.ID,
			"course_id":     log.CourseID,
			"actor_user_id": log.ActorUserID,
			"actor_email":   log.ActorEmail,
			"actor_role":    log.ActorRole,
			"action":        log.Action,
			"category":      log.Category,
			"target_type":   log.TargetType,
			"target_id":     log.TargetID,
			"target_name":   log.TargetName,
			"detail":        log.Detail,
			"ip_address":    log.IPAddress,
			"user_agent":    log.UserAgent,
			"device_type":   deviceType,
			"browser":       browser,
			"os":            osName,
			"created_at":    log.CreatedAt,
		}

		if actor, ok := usersByID[log.ActorUserID]; ok {
			item["actor"] = buildActorPayload(actor)
		}

		if resolved := resolveDetail(details[i], index); resolved != nil {
			item["resolved"] = resolved
		}

		// Many member actions were logged without a target_name; resolve it so
		// the Target column stops showing a dash.
		if refType, ok := targetTypeRefTypes[log.TargetType]; ok {
			if ref, found := index.lookup(refType, log.TargetID); found {
				item["target_ref"] = ref
				if strings.TrimSpace(log.TargetName) == "" {
					item["target_name"] = ref.Label
				}
			}
		}

		items = append(items, item)
	}

	return items, nil
}
