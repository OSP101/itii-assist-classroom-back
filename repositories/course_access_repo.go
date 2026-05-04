package repositories

import (
	"itii-assist/config"
	"strings"

	"gorm.io/gorm"
)

type courseAccessStateRow struct {
	CourseExists bool `gorm:"column:course_exists"`
	Allowed      bool `gorm:"column:allowed"`
}

type courseLookupRow struct {
	ID       uint   `gorm:"column:id"`
	CourseID string `gorm:"column:course_id"`
}

func resolveAllowedCourseRoles(allowedCourseRoles []string) (bool, bool) {
	if len(allowedCourseRoles) == 0 {
		return true, true
	}

	allowInstructor := false
	allowTA := false
	for _, role := range allowedCourseRoles {
		switch strings.ToLower(strings.TrimSpace(role)) {
		case "instructor":
			allowInstructor = true
		case "ta":
			allowTA = true
		}
	}

	if !allowInstructor && !allowTA {
		return true, true
	}

	return allowInstructor, allowTA
}

func GetCourseAccessState(courseID string, userID uint, allowedCourseRoles ...string) (bool, bool, error) {
	courseID = strings.TrimSpace(courseID)
	if courseID == "" || userID == 0 {
		return false, false, nil
	}

	allowInstructor, allowTA := resolveAllowedCourseRoles(allowedCourseRoles)
	conditions := make([]string, 0, 5)
	args := []interface{}{courseID}

	if allowInstructor {
		conditions = append(conditions,
			`EXISTS (SELECT 1 FROM courses c WHERE c.id = ? AND c.instructor_id = ?)`,
			`EXISTS (SELECT 1 FROM course_instructors ci WHERE ci.course_id = ? AND ci.user_id = ?)`,
			`EXISTS (SELECT 1 FROM course_members cm WHERE cm.course_id = ? AND cm.user_id = ? AND cm.role = 'instructor' AND cm.status = 'active')`,
		)
		args = append(args, courseID, userID, courseID, userID, courseID, userID)
	}

	if allowTA {
		conditions = append(conditions,
			`EXISTS (SELECT 1 FROM course_tas ct WHERE ct.course_id = ? AND ct.user_id = ?)`,
			`EXISTS (SELECT 1 FROM course_members cm WHERE cm.course_id = ? AND cm.user_id = ? AND cm.role = 'ta' AND cm.status = 'active')`,
		)
		args = append(args, courseID, userID, courseID, userID)
	}

	if len(conditions) == 0 {
		return true, false, nil
	}

	query := `
		SELECT EXISTS (SELECT 1 FROM courses WHERE id = ?) AS course_exists,
		       (` + strings.Join(conditions, ` OR `) + `) AS allowed
	`

	var row courseAccessStateRow
	if err := config.DB.Raw(query, args...).Scan(&row).Error; err != nil {
		return false, false, err
	}

	return row.CourseExists, row.Allowed, nil
}

func UserHasCourseAccess(courseID string, userID uint, allowedCourseRoles ...string) (bool, error) {
	_, allowed, err := GetCourseAccessState(courseID, userID, allowedCourseRoles...)
	return allowed, err
}

func loadCourseIDByRawQuery(query string, args ...interface{}) (string, error) {
	var row struct {
		CourseID string `gorm:"column:course_id"`
	}
	if err := config.DB.Raw(query, args...).Scan(&row).Error; err != nil {
		return "", err
	}
	if strings.TrimSpace(row.CourseID) == "" {
		return "", gorm.ErrRecordNotFound
	}
	return row.CourseID, nil
}

func uniqueCourseLookupIDs(values []uint) []uint {
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func loadCourseIDsByUintLookup(ids []uint, query string, args ...interface{}) ([]string, error) {
	normalizedIDs := uniqueCourseLookupIDs(ids)
	if len(normalizedIDs) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	var rows []courseLookupRow
	if err := config.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	resolvedIDs := make(map[uint]struct{}, len(rows))
	courseSet := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.ID == 0 || strings.TrimSpace(row.CourseID) == "" {
			continue
		}
		resolvedIDs[row.ID] = struct{}{}
		courseSet[row.CourseID] = struct{}{}
	}

	if len(resolvedIDs) != len(normalizedIDs) {
		return nil, gorm.ErrRecordNotFound
	}

	courseIDs := make([]string, 0, len(courseSet))
	for courseID := range courseSet {
		courseIDs = append(courseIDs, courseID)
	}

	return courseIDs, nil
}

func GetCourseIDByAssignmentID(assignmentID uint) (string, error) {
	return loadCourseIDByRawQuery(`SELECT course_id FROM assignments WHERE id = ? LIMIT 1`, assignmentID)
}

func GetCourseIDByAttendanceSessionID(sessionID uint) (string, error) {
	return loadCourseIDByRawQuery(`SELECT course_id FROM attendance_sessions WHERE id = ? LIMIT 1`, sessionID)
}

func GetCourseIDByScoreID(scoreID uint) (string, error) {
	return loadCourseIDByRawQuery(`
		SELECT a.course_id
		FROM scores s
		JOIN assignments a ON a.id = s.assignment_id
		WHERE s.id = ?
		LIMIT 1
	`, scoreID)
}

func GetCourseIDsByScoreIDs(scoreIDs []uint) ([]string, error) {
	normalizedIDs := uniqueCourseLookupIDs(scoreIDs)
	return loadCourseIDsByUintLookup(normalizedIDs, `
		SELECT s.id, a.course_id
		FROM scores s
		JOIN assignments a ON a.id = s.assignment_id
		WHERE s.id IN ?
	`, normalizedIDs)
}

func GetCourseIDByScoreEditRequestID(requestID uint) (string, error) {
	return loadCourseIDByRawQuery(`
		SELECT a.course_id
		FROM score_edit_requests ser
		JOIN scores s ON s.id = ser.score_id
		JOIN assignments a ON a.id = s.assignment_id
		WHERE ser.id = ?
		LIMIT 1
	`, requestID)
}

func GetCourseIDsByScoreEditRequestIDs(requestIDs []uint) ([]string, error) {
	normalizedIDs := uniqueCourseLookupIDs(requestIDs)
	return loadCourseIDsByUintLookup(normalizedIDs, `
		SELECT ser.id, a.course_id
		FROM score_edit_requests ser
		JOIN scores s ON s.id = ser.score_id
		JOIN assignments a ON a.id = s.assignment_id
		WHERE ser.id IN ?
	`, normalizedIDs)
}

func GetCourseIDByBonusScoreID(bonusScoreID uint) (string, error) {
	return loadCourseIDByRawQuery(`SELECT course_id FROM bonus_scores WHERE id = ? LIMIT 1`, bonusScoreID)
}

func GetCourseIDByQueueSessionID(sessionID string) (string, error) {
	return loadCourseIDByRawQuery(`SELECT course_id FROM queue_sessions WHERE id = ? LIMIT 1`, sessionID)
}
