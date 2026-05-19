package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"itii-assist/middlewares"
	"itii-assist/models"
	"itii-assist/repositories"

	"github.com/gofiber/fiber/v3"
)

// =============================================================================
// ExamSession handlers
// =============================================================================

// GET /api/courses/:courseId/exam-sessions
func GetExamSessionsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")
	sessions, err := repositories.GetExamSessionsByCourse(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get exam sessions"})
	}
	return c.JSON(fiber.Map{"success": true, "data": sessions})
}

// POST /api/courses/:courseId/exam-sessions
func CreateExamSessionHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")

	var input struct {
		ExamSettingID   uint     `json:"exam_setting_id"`
		ExamDate        string   `json:"exam_date"`
		StartTime       string   `json:"start_time"`
		EndTime         string   `json:"end_time"`
		Notes           string   `json:"notes"`
		ClassroomIDs    []string `json:"classroom_ids"`
		SeatNumberStart *int     `json:"seat_number_start"`
		SeatNumberStep  *int     `json:"seat_number_step"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.ExamSettingID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "exam_setting_id is required"})
	}

	examDate, err := time.Parse("2006-01-02", input.ExamDate)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid exam_date format (YYYY-MM-DD)"})
	}

	seatNumberStart := 1
	if input.SeatNumberStart != nil {
		if *input.SeatNumberStart < 1 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "seat_number_start must be a positive integer"})
		}
		seatNumberStart = *input.SeatNumberStart
	}

	seatNumberStep := 1
	if input.SeatNumberStep != nil {
		if *input.SeatNumberStep < 1 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "seat_number_step must be a positive integer"})
		}
		seatNumberStep = *input.SeatNumberStep
	}

	session := &models.ExamSession{
		CourseID:      courseID,
		ExamSettingID: input.ExamSettingID,
		ExamDate:      examDate,
		StartTime:     input.StartTime,
		EndTime:       input.EndTime,
		Notes:         input.Notes,
		SeatNumberStart: seatNumberStart,
		SeatNumberStep:  seatNumberStep,
	}
	if err := repositories.CreateExamSession(session); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create exam session"})
	}
	if err := repositories.ReplaceExamSessionRooms(session.ID, input.ClassroomIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to save exam rooms"})
	}
	if loaded, err := repositories.GetExamSessionByID(session.ID); err == nil {
		session = loaded
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": session})
}

// PUT /api/courses/:courseId/exam-sessions/:sessionId
func UpdateExamSessionHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}

	var input struct {
		ExamDate        string `json:"exam_date"`
		StartTime       string `json:"start_time"`
		EndTime         string `json:"end_time"`
		Notes           string `json:"notes"`
		SeatNumberStart *int   `json:"seat_number_start"`
		SeatNumberStep  *int   `json:"seat_number_step"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	updates := map[string]interface{}{}
	if input.ExamDate != "" {
		if t, err := time.Parse("2006-01-02", input.ExamDate); err == nil {
			updates["exam_date"] = t
		}
	}
	if input.StartTime != "" {
		updates["start_time"] = input.StartTime
	}
	if input.EndTime != "" {
		updates["end_time"] = input.EndTime
	}
	updates["notes"] = input.Notes
	if input.SeatNumberStart != nil {
		if *input.SeatNumberStart < 1 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "seat_number_start must be a positive integer"})
		}
		updates["seat_number_start"] = *input.SeatNumberStart
	}
	if input.SeatNumberStep != nil {
		if *input.SeatNumberStep < 1 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "seat_number_step must be a positive integer"})
		}
		updates["seat_number_step"] = *input.SeatNumberStep
	}

	if err := repositories.UpdateExamSession(uint(sessionID), updates); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update exam session"})
	}
	return c.JSON(fiber.Map{"success": true})
}

// DELETE /api/courses/:courseId/exam-sessions/:sessionId
func DeleteExamSessionHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	if err := repositories.ClearExamSeats(uint(sessionID)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to clear seats"})
	}
	if err := repositories.ClearExamSessionRooms(uint(sessionID)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to clear exam rooms"})
	}
	if err := repositories.DeleteExamSession(uint(sessionID)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete session"})
	}
	return c.JSON(fiber.Map{"success": true})
}

// PUT /api/courses/:courseId/exam-sessions/:sessionId/classrooms
func UpdateExamSessionClassroomsHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}

	var input struct {
		ClassroomIDs []string `json:"classroom_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	if err := repositories.ReplaceExamSessionRooms(uint(sessionID), input.ClassroomIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update exam rooms"})
	}

	session, err := repositories.GetExamSessionByID(uint(sessionID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}

	return c.JSON(fiber.Map{"success": true, "data": session.Rooms})
}

// =============================================================================
// ExamSeat handlers
// =============================================================================

// GET /api/courses/:courseId/exam-sessions/:sessionId/seats
func GetExamSeatsHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	seats, err := repositories.GetExamSeatsBySession(uint(sessionID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get seats"})
	}
	return c.JSON(fiber.Map{"success": true, "data": seats})
}

// POST /api/courses/:courseId/exam-sessions/:sessionId/seats
func AssignExamSeatHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}

	var input struct {
		StudentID uint   `json:"student_id"`
		DeskID    string `json:"desk_id"`
		SeatNumber int   `json:"seat_number"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.StudentID == 0 || input.DeskID == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "student_id and desk_id are required"})
	}
	if input.SeatNumber < 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "seat_number must be positive"})
	}

	seat := &models.ExamSeat{
		ExamSessionID: uint(sessionID),
		StudentRefID:  input.StudentID,
		DeskID:        input.DeskID,
		SeatNumber:    input.SeatNumber,
	}
	if err := repositories.UpsertExamSeat(seat); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to assign seat"})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": seat})
}

// POST /api/courses/:courseId/exam-sessions/:sessionId/seats/auto-assign
func AutoAssignExamSeatsHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	courseID := c.Params("courseId")

	var input struct {
		ClassroomIDs []string `json:"classroom_ids"`
	}
	if err := c.Bind().JSON(&input); err != nil || len(input.ClassroomIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "classroom_ids is required"})
	}

	// Get enrolled students for this course
	students, err := repositories.GetEnrolledStudents(courseID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get students"})
	}

	// Collect available desks across classrooms (sorted Y then X per classroom, classrooms alphabetical)
	var allDesks []models.Desk
	for _, clsID := range input.ClassroomIDs {
		desks, err := repositories.FindEnabledDesksByClassroomSorted(clsID)
		if err != nil {
			continue
		}
		allDesks = append(allDesks, desks...)
	}

	if len(allDesks) < len(students) {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": fmt.Sprintf("Not enough desks (%d) for students (%d)", len(allDesks), len(students)),
		})
	}

	// Clear existing seats first
	if err := repositories.ClearExamSeats(uint(sessionID)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to clear existing seats"})
	}

	seats := make([]models.ExamSeat, len(students))
	for i, st := range students {
		seats[i] = models.ExamSeat{
			ExamSessionID: uint(sessionID),
			StudentRefID:  st.ID,
			DeskID:        allDesks[i].ID,
			SeatNumber:    i + 1,
		}
	}
	if err := repositories.BulkCreateExamSeats(seats); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to auto-assign seats"})
	}
	if err := repositories.ReplaceExamSessionRooms(uint(sessionID), input.ClassroomIDs); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to sync exam rooms"})
	}
	return c.JSON(fiber.Map{"success": true, "assigned": len(seats)})
}

// PUT /api/courses/:courseId/exam-sessions/:sessionId/seats
func ReplaceExamSeatsHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}

	var input struct {
		Seats []struct {
			StudentID  uint   `json:"student_id"`
			DeskID     string `json:"desk_id"`
			SeatNumber int    `json:"seat_number"`
		} `json:"seats"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}

	seenStudents := make(map[uint]struct{}, len(input.Seats))
	seenDesks := make(map[string]struct{}, len(input.Seats))
	seenSeatNumbers := make(map[int]struct{}, len(input.Seats))
	seats := make([]models.ExamSeat, 0, len(input.Seats))
	for _, item := range input.Seats {
		if item.StudentID == 0 || item.DeskID == "" || item.SeatNumber <= 0 {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "Each seat requires student_id, desk_id, and seat_number"})
		}
		if _, exists := seenStudents[item.StudentID]; exists {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "Duplicate student assignment in request"})
		}
		if _, exists := seenDesks[item.DeskID]; exists {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "Duplicate desk assignment in request"})
		}
		if _, exists := seenSeatNumbers[item.SeatNumber]; exists {
			return c.Status(400).JSON(fiber.Map{"success": false, "message": "Duplicate seat_number in request"})
		}

		seenStudents[item.StudentID] = struct{}{}
		seenDesks[item.DeskID] = struct{}{}
		seenSeatNumbers[item.SeatNumber] = struct{}{}
		seats = append(seats, models.ExamSeat{
			ExamSessionID: uint(sessionID),
			StudentRefID:  item.StudentID,
			DeskID:        item.DeskID,
			SeatNumber:    item.SeatNumber,
		})
	}

	if err := repositories.ReplaceExamSeats(uint(sessionID), seats); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to save exam seats"})
	}

	updatedSeats, err := repositories.GetExamSeatsBySession(uint(sessionID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to load updated seats"})
	}

	return c.JSON(fiber.Map{"success": true, "data": updatedSeats})
}

// DELETE /api/courses/:courseId/exam-sessions/:sessionId/seats/:seatId
func UnassignExamSeatHandler(c fiber.Ctx) error {
	seatID, err := strconv.ParseUint(c.Params("seatId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid seat ID"})
	}
	if err := repositories.DeleteExamSeat(uint(seatID)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to unassign seat"})
	}
	return c.JSON(fiber.Map{"success": true})
}

// DELETE /api/courses/:courseId/exam-sessions/:sessionId/seats
func ClearExamSeatsHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}
	if err := repositories.ClearExamSeats(uint(sessionID)); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to clear seats"})
	}
	return c.JSON(fiber.Map{"success": true})
}

// GET /api/courses/:courseId/exam-sessions/:sessionId/export
func GetExamSeatingExportHandler(c fiber.Ctx) error {
	sessionID, err := strconv.ParseUint(c.Params("sessionId"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid session ID"})
	}

	session, err := repositories.GetExamSessionByID(uint(sessionID))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Session not found"})
	}

	rows, err := repositories.GetExamSeatingExport(uint(sessionID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get seating export"})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"session": session,
			"rows":    rows,
		},
	})
}

// GET /api/courses/:courseId/my-exam-seats  (student auth)
func GetMyExamSeatsHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")

	studentID, ok := middlewares.GetStudentID(c)
	if !ok || studentID == 0 {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Student authentication required"})
	}

	seats, err := repositories.GetMyExamSeats(courseID, studentID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to get exam seats"})
	}
	return c.JSON(fiber.Map{"success": true, "data": seats})
}

// =============================================================================
// DOCX Import handlers
// =============================================================================

// ImportPreviewRow represents one parsed row from the DOCX roster file.
type ImportPreviewRow struct {
	RowNum        int    `json:"row_num"`
	StudentID     string `json:"student_id"`
	FullName      string `json:"full_name"`
	Major         string `json:"major"`
	SeatLabel     string `json:"seat_label"`
	ClassroomName string `json:"classroom_name"`
	DeskNumber    int    `json:"desk_number"`
	StudentFound  bool   `json:"student_found"`
	DeskFound     bool   `json:"desk_found"`
	StudentDBID   uint   `json:"student_db_id,omitempty"`
	DeskDBID      string `json:"desk_db_id,omitempty"`
}

// POST /api/courses/:courseId/exam-sessions/import/preview
func ImportExamSeatsPreviewHandler(c fiber.Ctx) error {
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader == nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "No file uploaded (field name: file)"})
	}

	const maxSize = 10 * 1024 * 1024 // 10 MB
	f, err := fileHeader.Open()
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Cannot read file"})
	}
	defer f.Close()

	content, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Cannot read file content"})
	}
	if len(content) > maxSize {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "File too large (max 10 MB)"})
	}

	rows, err := parseDocxRoster(content)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Failed to parse DOCX: " + err.Error()})
	}

	// Resolve students and desks
	matched, studentNotFound, deskNotFound := 0, 0, 0
	for i := range rows {
		// Student lookup
		if st, err := repositories.FindStudentByStudentID(rows[i].StudentID); err == nil {
			rows[i].StudentFound = true
			rows[i].StudentDBID = st.ID
		} else {
			studentNotFound++
		}

		// Desk lookup
		if rows[i].ClassroomName != "" && rows[i].DeskNumber > 0 {
			cls, err := repositories.FindClassroomByName(rows[i].ClassroomName)
			if err == nil {
				desk, err := repositories.FindDeskByClassroomAndNumber(cls.ID, rows[i].DeskNumber)
				if err == nil {
					rows[i].DeskFound = true
					rows[i].DeskDBID = desk.ID
				} else {
					deskNotFound++
				}
			} else {
				deskNotFound++
			}
		} else {
			deskNotFound++
		}

		if rows[i].StudentFound && rows[i].DeskFound {
			matched++
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"rows":              rows,
			"total":             len(rows),
			"matched":           matched,
			"student_not_found": studentNotFound,
			"desk_not_found":    deskNotFound,
		},
	})
}

// POST /api/courses/:courseId/exam-sessions/import/commit
func ImportExamSeatsCommitHandler(c fiber.Ctx) error {
	courseID := c.Params("courseId")

	var input struct {
		ExamSettingID uint   `json:"exam_setting_id"`
		ExamDate      string `json:"exam_date"`
		StartTime     string `json:"start_time"`
		EndTime       string `json:"end_time"`
		Notes         string `json:"notes"`
		Seats         []struct {
			StudentID  uint   `json:"student_id"`
			DeskID     string `json:"desk_id"`
			SeatNumber int    `json:"seat_number"`
		} `json:"seats"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid input"})
	}
	if input.ExamSettingID == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "exam_setting_id is required"})
	}
	if len(input.Seats) == 0 {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "No seats to import"})
	}

	examDate, err := time.Parse("2006-01-02", input.ExamDate)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": "Invalid exam_date (YYYY-MM-DD)"})
	}

	session := &models.ExamSession{
		CourseID:      courseID,
		ExamSettingID: input.ExamSettingID,
		ExamDate:      examDate,
		StartTime:     input.StartTime,
		EndTime:       input.EndTime,
		Notes:         input.Notes,
	}
	if err := repositories.CreateExamSession(session); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create exam session"})
	}

	seats := make([]models.ExamSeat, 0, len(input.Seats))
	for _, s := range input.Seats {
		if s.StudentID == 0 || s.DeskID == "" {
			continue
		}
		seats = append(seats, models.ExamSeat{
			ExamSessionID: session.ID,
			StudentRefID:  s.StudentID,
			DeskID:        s.DeskID,
			SeatNumber:    s.SeatNumber,
		})
	}
	if err := repositories.BulkCreateExamSeats(seats); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to create seats"})
	}
	if err := repositories.SyncExamSessionRoomsFromSeats(session.ID); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to sync exam rooms"})
	}

	return c.Status(201).JSON(fiber.Map{
		"success":    true,
		"session_id": session.ID,
		"imported":   len(seats),
	})
}

// =============================================================================
// DOCX parser (stdlib only: archive/zip + encoding/xml)
// =============================================================================

// wordDocument and related types for XML decoding
type wordBody struct {
	Tables []wordTable `xml:"body>tbl"`
}
type wordTable struct {
	Rows []wordRow `xml:"tr"`
}
type wordRow struct {
	Cells []wordCell `xml:"tc"`
}
type wordCell struct {
	Paragraphs []wordParagraph `xml:"p"`
}
type wordParagraph struct {
	Runs []wordRun `xml:"r"`
}
type wordRun struct {
	Texts []wordText `xml:"t"`
}
type wordText struct {
	Value string `xml:",chardata"`
}

// studentIDPattern matches KKU student IDs like "683380139-4"
var studentIDPattern = regexp.MustCompile(`^\d{9}-\d$`)

// seatLabelPattern matches "CP9226-28" style labels (room code followed by dash + number)
var seatLabelPattern = regexp.MustCompile(`^([A-Za-z]{2}\d{4})-(\d+)$`)

func parseDocxRoster(content []byte) ([]ImportPreviewRow, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("not a valid DOCX (ZIP) file")
	}

	// Find word/document.xml
	var docFile *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return nil, fmt.Errorf("word/document.xml not found in DOCX")
	}

	rc, err := docFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	xmlBytes, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	// The Word XML uses namespace prefixes; strip them for simpler parsing
	xmlStr := string(xmlBytes)
	// Remove namespace prefixes like w:, w14:, r:, etc.
	xmlStr = regexp.MustCompile(`<(/?)w\d*:`).ReplaceAllString(xmlStr, "<$1")
	xmlStr = regexp.MustCompile(`<(/?)[a-zA-Z]+:`).ReplaceAllString(xmlStr, "<$1")

	var doc wordBody
	if err := xml.Unmarshal([]byte(xmlStr), &doc); err != nil {
		return nil, fmt.Errorf("XML parse error: %v", err)
	}

	var rows []ImportPreviewRow
	rowNum := 0

	for _, tbl := range doc.Tables {
		for _, tr := range tbl.Rows {
			cells := extractCellTexts(tr)
			if len(cells) < 5 {
				continue
			}
			// Identify data rows: cell[1] matches student ID pattern
			rawID := strings.TrimSpace(cells[1])
			if !studentIDPattern.MatchString(rawID) {
				continue
			}

			rowNum++
			rawSeat := strings.TrimSpace(cells[4])
			classroomName, deskNumber := parseSeatLabel(rawSeat)

			rows = append(rows, ImportPreviewRow{
				RowNum:        rowNum,
				StudentID:     rawID,
				FullName:      strings.TrimSpace(cells[2]),
				Major:         strings.TrimSpace(cells[3]),
				SeatLabel:     rawSeat,
				ClassroomName: classroomName,
				DeskNumber:    deskNumber,
			})
		}
	}

	return rows, nil
}

func extractCellTexts(row wordRow) []string {
	texts := make([]string, len(row.Cells))
	for i, cell := range row.Cells {
		var sb strings.Builder
		for _, p := range cell.Paragraphs {
			for _, r := range p.Runs {
				for _, t := range r.Texts {
					sb.WriteString(t.Value)
				}
			}
		}
		texts[i] = sb.String()
	}
	return texts
}

func parseSeatLabel(label string) (classroomName string, deskNumber int) {
	m := seatLabelPattern.FindStringSubmatch(label)
	if m != nil {
		classroomName = m[1]
		deskNumber, _ = strconv.Atoi(m[2])
		return
	}
	// Fallback: plain number only (e.g. "28.")
	clean := strings.TrimRight(label, ".")
	if n, err := strconv.Atoi(strings.TrimSpace(clean)); err == nil {
		deskNumber = n
	}
	return
}
