package routes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/services"
	"itii-assist/utils"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func securityFixture(t *testing.T) (*fiber.App, *gorm.DB, map[string]string) {
	t.Helper()
	t.Setenv("JWT_SECRET", "security-regression-access-key-not-for-production")
	t.Setenv("JWT_REFRESH_SECRET", "security-regression-refresh-key-not-for-production")
	t.Setenv("ATTENDANCE_NETWORK_GUARD_ENABLED", "false")
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&models.User{}, &models.UserOAuthAccount{}, &models.Student{}, &models.RefreshToken{}, &models.AppConfig{}, &models.Course{}, &models.CourseInstructor{}, &models.CourseTA{}, &models.CourseMember{}, &models.AttendanceSession{}, &models.QueueSession{}, &models.QueueBooking{}, &models.Classroom{}, &models.Zone{}, &models.Desk{}, &models.QueueDeskStatus{}, &models.LoginChallenge{}, &models.TwoFactorPending{}, &models.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	// Match the existing SQLite fixtures: SQLite otherwise gives Postgres
	// timestamptz columns NUMERIC affinity and cannot scan them into time.Time.
	var tables []struct{ Name, SQL string }
	if err = db.Raw("SELECT name, sql FROM sqlite_master WHERE type = 'table' AND sql LIKE '%timestamptz%'").Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if err = db.Exec("DROP TABLE `" + table.Name + "`").Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Exec(strings.ReplaceAll(table.SQL, "timestamptz", "datetime")).Error; err != nil {
			t.Fatal(err)
		}
	}
	previousDB, previousRedis := config.DB, config.Redis
	config.DB, config.Redis = db, nil
	t.Cleanup(func() { config.DB, config.Redis = previousDB, previousRedis; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	put := func(value interface{}) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	put(&models.User{ID: 1, Username: "ta", PasswordHash: "unused", Role: "ta", IsActive: true})
	put(&models.Student{ID: 11, StudentID: "student-a", Email: "a@example.test", FullName: "A", IsActive: true})
	put(&models.Student{ID: 12, StudentID: "student-b", Email: "b@example.test", FullName: "B", IsActive: true})
	put(&models.Course{ID: "course-a", Code: "A", Name: "A", Year: 2569, Semester: 1, IsActive: true})
	put(&models.Course{ID: "course-b", Code: "B", Name: "B", Year: 2569, Semester: 1, IsActive: true})
	put(&models.CourseTA{CourseID: "course-a", UserID: 1})
	put(&models.AttendanceSession{ID: 100, CourseID: "course-a", Title: "A", Status: "draft", StartTime: time.Now().Add(-time.Minute), EndTime: time.Now().Add(time.Hour)})
	put(&models.AttendanceSession{ID: 101, CourseID: "course-b", Title: "B", Status: "draft", StartTime: time.Now().Add(-time.Minute), EndTime: time.Now().Add(time.Hour)})
	put(&models.Classroom{ID: "room", Name: "Room", Building: "Building", Floor: "1"})
	put(&models.Desk{ID: "desk", ClassroomID: "room", Number: 1, Type: "normal", IsEnabled: true})
	put(&models.QueueSession{ID: "queue-a", CourseID: "course-a", ClassroomID: "room", Title: "Queue", PinCode: "123456", Status: "active"})
	put(&models.QueueBooking{ID: 31, QueueSessionID: "queue-a", StudentID: 11, DeskID: "desk", DeskNumber: 1, BookingType: "help", Status: "waiting"})
	tokens := map[string]string{}
	for _, who := range []struct {
		name, kind, role string
		id               uint
	}{{"ta", "u", "ta", 1}, {"a", "s", "student", 11}, {"b", "s", "student", 12}} {
		var token, jti string
		if who.kind == "s" {
			token, _, jti, err = utils.GenerateStudentTokenPair(who.id)
		} else {
			token, _, jti, err = utils.GenerateTokenPair(who.id, who.role)
		}
		if err != nil {
			t.Fatal(err)
		}
		tokens[who.name] = token
		put(&models.RefreshToken{JTI: jti, UserID: who.id, Kind: who.kind, ExpiresAt: time.Now().Add(time.Hour)})
	}
	app := fiber.New()
	audit := services.NewAuditLogger(db)
	SetupQueueRoutes(app, audit)
	SetupAttendanceRoutes(app, audit)
	SetupAuthRoutes(app, audit)
	return app, db, tokens
}

func securityRequest(t *testing.T, app *fiber.App, method, path, body, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Password hashing under the race detector can exceed Fiber's 1s default.
	response, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, raw
}

func TestSecurityAnonymousQueueAccessIsRejectedBeforeHandlers(t *testing.T) {
	app := fiber.New()
	SetupQueueRoutes(app, services.NewAuditLogger(nil))
	for _, tc := range []struct{ method, path string }{{"POST", "/api/queue/bookings"}, {"POST", "/api/queue/validate"}, {"POST", "/api/queue/check-existing"}, {"POST", "/api/queue/bookings/31/cancel"}, {"GET", "/api/queue/bookings/31/status"}, {"POST", "/api/queue/sessions/queue-a/status"}, {"POST", "/api/queue/sessions/queue-a/cutoff"}, {"POST", "/api/queue/sessions/queue-a/heartbeat"}, {"POST", "/api/courses/course-a/queue/bookings"}} {
		if code, _ := securityRequest(t, app, tc.method, tc.path, `{}`, ""); code != 401 {
			t.Errorf("%s %s: got %d", tc.method, tc.path, code)
		}
	}
}

func TestSecurityStudentCannotSpoofOwnerReadOtherBookingOrControlQueue(t *testing.T) {
	app, db, tokens := securityFixture(t)
	for _, tc := range []struct{ method, path, body, token string }{
		{"GET", "/api/queue/bookings/31/status", ``, tokens["b"]},
		{"POST", "/api/queue/bookings/31/cancel", `{}`, tokens["b"]},
		{"POST", "/api/queue/bookings", `{"pin_code":"123456","student_id":"student-b","desk_number":1,"booking_type":"help"}`, tokens["a"]},
		{"POST", "/api/queue/sessions/queue-a/cutoff", `{"is_cutoff_enabled":false}`, tokens["a"]},
		{"POST", "/api/queue/sessions/queue-a/status", `{"status":"paused"}`, tokens["a"]},
		{"POST", "/api/realtime/room-ticket", `{"event":"join-booking","data":31}`, tokens["b"]},
	} {
		if code, raw := securityRequest(t, app, tc.method, tc.path, tc.body, tc.token); code != 403 {
			t.Errorf("%s: got %d %s", tc.path, code, raw)
		}
	}
	var booking models.QueueBooking
	if err := db.First(&booking, 31).Error; err != nil {
		t.Fatal(err)
	}
	if booking.Status != "waiting" {
		t.Fatal("denied cancellation mutated booking")
	}
	var count int64
	db.Model(&models.QueueBooking{}).Count(&count)
	if count != 1 {
		t.Fatal("denied impersonation created a booking")
	}
	if code, raw := securityRequest(t, app, "GET", "/api/queue/bookings/31/status", ``, tokens["a"]); code != 200 {
		t.Fatalf("owner lost access: %d %s", code, raw)
	}
	if code, raw := securityRequest(t, app, "POST", "/api/realtime/room-ticket", `{"event":"join-booking","data":31}`, tokens["a"]); code != 200 {
		t.Fatalf("owner lost realtime: %d %s", code, raw)
	}
}

func TestSecurityAttendanceStartChecksCourseAndKeepsAllowedPath(t *testing.T) {
	app, db, tokens := securityFixture(t)
	if code, _ := securityRequest(t, app, "POST", "/api/attendance/sessions/start", `{"attendance_session_id":101}`, tokens["ta"]); code != 403 {
		t.Fatalf("cross-course start got %d", code)
	}
	var session models.AttendanceSession
	if err := db.First(&session, 101).Error; err != nil {
		t.Fatal(err)
	}
	if session.Status != "draft" {
		t.Fatal("denied start modified session")
	}
	// Redis is deliberately absent; an authorized caller reaches the service
	// readiness check, while a foreign-course caller is rejected before it.
	if code, raw := securityRequest(t, app, "POST", "/api/attendance/sessions/start", `{"attendance_session_id":100}`, tokens["ta"]); code != 503 {
		t.Fatalf("authorized start path changed: %d %s", code, raw)
	}
}

func TestSecurityDisabledAndRevokedSessionsCannotAccessPrivateData(t *testing.T) {
	app, db, tokens := securityFixture(t)
	if err := db.Model(&models.Student{}).Where("id = ?", 11).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if code, _ := securityRequest(t, app, "GET", "/api/queue/bookings/31/status", ``, tokens["a"]); code != 401 {
		t.Fatalf("disabled account got %d", code)
	}
	if err := db.Model(&models.RefreshToken{}).Where("user_id = ? AND kind = ?", 12, "s").Update("revoked", true).Error; err != nil {
		t.Fatal(err)
	}
	if code, _ := securityRequest(t, app, "GET", "/api/queue/bookings/31/status", ``, tokens["b"]); code != 401 {
		t.Fatalf("revoked session got %d", code)
	}
}

func TestSecurityTwoFactorRequiresFirstFactorAndIsSingleUse(t *testing.T) {
	app, db, _ := securityFixture(t)
	hash, err := utils.HashPassword("Valid-security-test-password-123!")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&models.User{}).Where("id = ?", 1).Updates(map[string]interface{}{"password_hash": hash, "two_factor_enabled": true, "two_factor_method": "email"}).Error; err != nil {
		t.Fatal(err)
	}
	if code, _ := securityRequest(t, app, "POST", "/api/auth/2fa/complete-login", `{"userId":1,"code":"123456"}`, ""); code != 401 {
		t.Fatalf("missing challenge got %d", code)
	}
	code, raw := securityRequest(t, app, "POST", "/api/auth/login", `{"username":"ta","password":"Valid-security-test-password-123!"}`, "")
	if code != 200 {
		t.Fatalf("password login failed: %d %s", code, raw)
	}
	var response struct {
		Data struct {
			Challenge string `json:"challenge"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &response); err != nil || response.Data.Challenge == "" {
		t.Fatalf("missing challenge: %s", raw)
	}
	digest := sha256.Sum256([]byte(response.Data.Challenge))
	expiry := time.Now().Add(time.Minute)
	if err = db.Create(&models.TwoFactorPending{UserID: 1, Method: "email", Secret: "login:" + hex.EncodeToString(digest[:]), EmailCode: "123456", EmailCodeExpiresAt: &expiry, ExpiresAt: expiry}).Error; err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"userId":1,"code":"123456","challenge":%q}`, response.Data.Challenge)
	if code, raw = securityRequest(t, app, "POST", "/api/auth/2fa/verify-login", body, ""); code != 200 {
		t.Fatalf("verification failed: %d %s", code, raw)
	}
	if code, raw = securityRequest(t, app, "POST", "/api/auth/2fa/complete-login", body, ""); code != 200 {
		t.Fatalf("completion failed: %d %s", code, raw)
	}
	if code, _ = securityRequest(t, app, "POST", "/api/auth/2fa/complete-login", body, ""); code != 401 {
		t.Fatalf("challenge replay got %d", code)
	}
}
