// Command loadtest-seed prepares everything scripts/loadtest/checkin.js needs
// for a 1000-student check-in burst test (plan.md ระยะ 0.4):
//
//   - a test course + section with N students, created through the real
//     admin HTTP API (same calls itii-assist-classroom-back/tools/stress_attendance_queue.ps1
//     already makes, so it exercises the same import/bulk-add code paths)
//   - two attendance sessions: one static PIN, one auto-rotating PIN
//   - one Bearer access token per student, minted directly against the DB
//     (utils.GenerateStudentTokenPair + a persisted refresh_tokens row) —
//     there is no password login for students, only OAuth, so this is the
//     only way to get 1000 valid sessions without 1000 real Google/KKU
//     logins
//   - one fake Google id_token per student in the format tools/tokeninfo-stub
//     understands, for the "anonymous / Google token" load profile
//
// This binary links itii-assist's config/models/repositories/utils packages
// directly, so it must run with the same DB_*/REDIS_*/JWT_* env as the
// backend (e.g. `--env-file .env.backend` style, or on the VM/docker network
// itself). It is a load-testing tool only — never import it from cmd/api.
//
// Output: a JSON file consumed by k6 (see scripts/loadtest/checkin.js):
//
//	{
//	  "static_session_id": 123, "static_pin": "482913",
//	  "rotating_session_id": 124,
//	  "students": [
//	    {"student_id": 1, "access_token": "...", "google_id_token": "..."},
//	    ...
//	  ]
//	}
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"itii-assist/utils"

	"github.com/joho/godotenv"
)

type seedConfig struct {
	baseURL      string
	username     string
	password     string
	studentCount int
	courseID     string
	sectionID    uint
	envFile      string
	outFile      string
}

func main() {
	cfg := parseFlags()

	if cfg.envFile != "" {
		if err := godotenv.Load(cfg.envFile); err != nil {
			log.Printf("⚠️  could not load %s: %v (continuing with existing env)", cfg.envFile, err)
		}
	}

	config.ConnectDB()
	config.ConnectRedis()

	client := &http.Client{Timeout: 30 * time.Second}

	token, err := login(client, cfg.baseURL, cfg.username, cfg.password)
	if err != nil {
		log.Fatalf("login failed: %v", err)
	}

	courseID, sectionID := cfg.courseID, cfg.sectionID
	if courseID == "" {
		courseID, sectionID, err = firstCourseAndSection(client, cfg.baseURL, token)
		if err != nil {
			log.Fatalf("resolve course/section: %v", err)
		}
	}

	studentCodes := make([]string, 0, cfg.studentCount)
	for i := 1; i <= cfg.studentCount; i++ {
		studentCodes = append(studentCodes, fmt.Sprintf("%011d", 78900000000+i))
	}

	log.Printf("importing %d students...", cfg.studentCount)
	if err := importStudents(client, cfg.baseURL, token, studentCodes); err != nil {
		log.Fatalf("import students: %v", err)
	}

	log.Println("resolving student DB ids...")
	studentIDs, err := searchStudentIDs(client, cfg.baseURL, token, studentCodes)
	if err != nil {
		log.Fatalf("search students: %v", err)
	}

	log.Println("adding students to section...")
	if err := bulkAddStudents(client, cfg.baseURL, token, courseID, sectionID, studentIDs); err != nil {
		log.Fatalf("bulk add students: %v", err)
	}

	log.Println("creating attendance sessions...")
	staticSessionID, staticPin, err := createAttendanceSession(client, cfg.baseURL, token, courseID, sectionID, false)
	if err != nil {
		log.Fatalf("create static-pin session: %v", err)
	}
	rotatingSessionID, _, err := createAttendanceSession(client, cfg.baseURL, token, courseID, sectionID, true)
	if err != nil {
		log.Fatalf("create rotating-pin session: %v", err)
	}

	googleClientID := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	if googleClientID == "" {
		log.Println("⚠️  GOOGLE_CLIENT_ID is empty — the google-token k6 profile will fail audience checks")
	}

	log.Println("minting bearer tokens + stub google tokens for every student...")
	students := make([]studentCreds, 0, len(studentIDs))
	for idx, id := range studentIDs {
		access, err := mintStudentBearerToken(id)
		if err != nil {
			log.Fatalf("mint token for student %d: %v", id, err)
		}
		email := fmt.Sprintf("loadtest+%s@example.com", studentCodes[idx])
		students = append(students, studentCreds{
			StudentID:     id,
			AccessToken:   access,
			GoogleIDToken: encodeStubGoogleToken(email, fmt.Sprintf("stub-sub-%d", id), googleClientID),
		})
	}

	out := seedOutput{
		StaticSessionID:   staticSessionID,
		StaticPin:         staticPin,
		RotatingSessionID: rotatingSessionID,
		Students:          students,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Fatalf("marshal output: %v", err)
	}
	if err := os.WriteFile(cfg.outFile, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", cfg.outFile, err)
	}
	log.Printf("✅ wrote %s (%d students, static session %d, rotating session %d)", cfg.outFile, len(students), staticSessionID, rotatingSessionID)
}

func parseFlags() seedConfig {
	cfg := seedConfig{}
	flag.StringVar(&cfg.baseURL, "url", envOr("LOADTEST_BASE_URL", "http://127.0.0.1:8000"), "backend base URL")
	flag.StringVar(&cfg.username, "username", os.Getenv("LOADTEST_ADMIN_USER"), "admin/instructor username")
	flag.StringVar(&cfg.password, "password", os.Getenv("LOADTEST_ADMIN_PASS"), "admin/instructor password")
	flag.IntVar(&cfg.studentCount, "students", 1000, "number of test students")
	flag.StringVar(&cfg.courseID, "course-id", "", "existing course id (blank = use first course)")
	sectionID := flag.Uint("section-id", 0, "existing section id (blank = use first section of the resolved course)")
	flag.StringVar(&cfg.envFile, "env-file", ".env.backend", "env file with DB_*/REDIS_*/JWT_* (same as the backend)")
	flag.StringVar(&cfg.outFile, "out", "loadtest-seed.json", "output JSON path for k6")
	flag.Parse()
	cfg.sectionID = *sectionID

	if cfg.username == "" || cfg.password == "" {
		log.Fatal("-username/-password (or LOADTEST_ADMIN_USER/LOADTEST_ADMIN_PASS) are required")
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// --- HTTP setup calls (mirrors tools/stress_attendance_queue.ps1) ---

func doJSON(client *http.Client, method, url, token string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s -> %d: %s", method, url, resp.StatusCode, string(respBody))
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

func login(client *http.Client, baseURL, username, password string) (string, error) {
	var resp struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	err := doJSON(client, http.MethodPost, baseURL+"/api/auth/login", "", map[string]string{
		"username": username, "password": password,
	}, &resp)
	return resp.Data.AccessToken, err
}

func firstCourseAndSection(client *http.Client, baseURL, token string) (string, uint, error) {
	var resp struct {
		Data struct {
			Courses []struct {
				ID       string `json:"id"`
				Sections []struct {
					ID uint `json:"id"`
				} `json:"sections"`
			} `json:"courses"`
		} `json:"data"`
	}
	if err := doJSON(client, http.MethodGet, baseURL+"/api/courses?page=1&limit=1", token, nil, &resp); err != nil {
		return "", 0, err
	}
	if len(resp.Data.Courses) == 0 || len(resp.Data.Courses[0].Sections) == 0 {
		return "", 0, fmt.Errorf("no course/section found; pass -course-id and -section-id explicitly")
	}
	return resp.Data.Courses[0].ID, resp.Data.Courses[0].Sections[0].ID, nil
}

func importStudents(client *http.Client, baseURL, token string, codes []string) error {
	type row struct {
		StudentID string `json:"student_id"`
		FullName  string `json:"full_name"`
		Email     string `json:"email"`
	}
	for start := 0; start < len(codes); start += 200 {
		end := start + 200
		if end > len(codes) {
			end = len(codes)
		}
		batch := make([]row, 0, end-start)
		for _, code := range codes[start:end] {
			batch = append(batch, row{
				StudentID: code,
				FullName:  "Load Test Student " + code,
				Email:     fmt.Sprintf("loadtest+%s@example.com", code),
			})
		}
		if err := doJSON(client, http.MethodPost, baseURL+"/api/students/import", token, map[string]any{"students": batch}, nil); err != nil {
			return err
		}
	}
	return nil
}

func searchStudentIDs(client *http.Client, baseURL, token string, codes []string) ([]uint, error) {
	idMap := make(map[string]uint, len(codes))
	for start := 0; start < len(codes); start += 200 {
		end := start + 200
		if end > len(codes) {
			end = len(codes)
		}
		// Matches handlers.SearchStudentsByIDsCompatHandler's actual response
		// shape: data is an object with found/not_found, and each found entry
		// wraps the student one level deeper under "student" — not a flat
		// array of {id, student_id} (caught live: the earlier shape assumed
		// here didn't match the real handler and failed to unmarshal).
		var resp struct {
			Data struct {
				Found []struct {
					Student struct {
						ID        uint   `json:"id"`
						StudentID string `json:"student_id"`
					} `json:"student"`
				} `json:"found"`
				NotFound []string `json:"not_found"`
			} `json:"data"`
		}
		if err := doJSON(client, http.MethodPost, baseURL+"/api/students/search-by-ids", token, map[string]any{"studentIds": codes[start:end]}, &resp); err != nil {
			return nil, err
		}
		if len(resp.Data.NotFound) > 0 {
			return nil, fmt.Errorf("search-by-ids: %d student(s) not found, e.g. %s", len(resp.Data.NotFound), resp.Data.NotFound[0])
		}
		for _, f := range resp.Data.Found {
			idMap[f.Student.StudentID] = f.Student.ID
		}
	}
	ids := make([]uint, 0, len(codes))
	for _, code := range codes {
		id, ok := idMap[code]
		if !ok {
			return nil, fmt.Errorf("student %s missing from search-by-ids response", code)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func bulkAddStudents(client *http.Client, baseURL, token, courseID string, sectionID uint, ids []uint) error {
	for start := 0; start < len(ids); start += 200 {
		end := start + 200
		if end > len(ids) {
			end = len(ids)
		}
		url := fmt.Sprintf("%s/api/courses/%s/sections/%d/students/bulk", baseURL, courseID, sectionID)
		if err := doJSON(client, http.MethodPost, url, token, map[string]any{"student_ids": ids[start:end]}, nil); err != nil {
			return err
		}
	}
	return nil
}

func createAttendanceSession(client *http.Client, baseURL, token, courseID string, sectionID uint, autoRotate bool) (uint, string, error) {
	now := time.Now()
	pin := fmt.Sprintf("%06d", rand.Intn(900000)+100000)
	body := map[string]any{
		"course_id":              courseID,
		"section_ids":            []uint{sectionID},
		"title":                  "loadtest-" + strconv.FormatInt(now.UnixMilli(), 10),
		"pin_code":               pin,
		"auto_rotate_pin":        autoRotate,
		"session_type":           "lecture",
		"check_location":         false,
		"radius_meters":          50,
		"start_time":             now.Add(-5 * time.Minute).Format(time.RFC3339),
		"end_time":               now.Add(90 * time.Minute).Format(time.RFC3339),
		"late_threshold_minutes": 15,
	}
	var resp struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := doJSON(client, http.MethodPost, baseURL+"/api/attendance", token, body, &resp); err != nil {
		return 0, "", err
	}
	return resp.Data.ID, pin, nil
}

// --- direct-DB token minting (no OAuth round trip needed) ---

func mintStudentBearerToken(studentID uint) (string, error) {
	access, refresh, jti, err := utils.GenerateStudentTokenPair(studentID)
	if err != nil {
		return "", err
	}
	_ = refresh // not needed by k6; the refresh flow is out of scope for the hot path load test
	record := &models.RefreshToken{
		JTI:       jti,
		UserID:    studentID,
		Kind:      "s",
		Revoked:   false,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := repositories.CreateRefreshToken(record); err != nil {
		return "", err
	}
	return access, nil
}

func encodeStubGoogleToken(email, sub, aud string) string {
	// Keep in sync with tools/tokeninfo-stub's EncodeStubToken — duplicated
	// rather than imported so this tool has no dependency on that binary.
	payload := map[string]string{"email": email, "sub": sub, "aud": aud}
	raw, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(raw)
}

type studentCreds struct {
	StudentID     uint   `json:"student_id"`
	AccessToken   string `json:"access_token"`
	GoogleIDToken string `json:"google_id_token"`
}

type seedOutput struct {
	StaticSessionID   uint           `json:"static_session_id"`
	StaticPin         string         `json:"static_pin"`
	RotatingSessionID uint           `json:"rotating_session_id"`
	Students          []studentCreds `json:"students"`
}
