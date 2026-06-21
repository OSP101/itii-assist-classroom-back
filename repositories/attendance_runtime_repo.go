package repositories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/observability"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	attendancePinReservationAttempts = 10
	attendanceStartIdempotencyTTL    = 5 * time.Minute
	attendanceStateTTLBuffer         = 2 * time.Minute
)

var (
	ErrAttendanceRedisUnavailable = errors.New("attendance redis unavailable")
	ErrAttendancePinUnavailable   = errors.New("attendance pin unavailable")
	ErrAttendanceSessionClosed    = errors.New("attendance session closed")
	ErrAttendanceInvalidPIN       = errors.New("attendance invalid pin")
)

type AttendanceRuntimeState struct {
	SessionID          uint       `json:"session_id"`
	Mode               string     `json:"mode"`
	Status             string     `json:"status"`
	CurrentPIN         string     `json:"current_pin"`
	NextPIN            string     `json:"next_pin,omitempty"`
	PreviousPIN        string     `json:"previous_pin,omitempty"`
	PinIssuedAt        *time.Time `json:"pin_issued_at,omitempty"`
	PreviousValidUntil *time.Time `json:"previous_valid_until,omitempty"`
	NextRotationAt     *time.Time `json:"next_rotation_at,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
}

type AttendanceStartResult struct {
	SessionID      uint       `json:"session_id"`
	CurrentPIN     string     `json:"current_pin"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	NextRotationAt *time.Time `json:"next_rotation_at,omitempty"`
	State          AttendanceRuntimeState
}

func attendancePinHash(pin string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(pin)))
	return hex.EncodeToString(sum[:])
}

func attendanceRedisAvailable() bool {
	return config.Redis != nil
}

func attendanceModeForSession(session *models.AttendanceSession) string {
	if (session.AutoRotatePin != nil && *session.AutoRotatePin) && observability.AttendancePinAutoRotateEnabled() {
		return "rotating"
	}
	return "static"
}

func attendancePinKey(pin string) string {
	return fmt.Sprintf("attendance:pin:%s", strings.TrimSpace(pin))
}

func attendanceSessionStateKey(sessionID uint) string {
	return fmt.Sprintf("attendance:session:%d:state", sessionID)
}

func attendanceSessionCurrentKey(sessionID uint) string {
	return fmt.Sprintf("attendance:session:%d:current_pin", sessionID)
}

func attendanceSessionNextKey(sessionID uint) string {
	return fmt.Sprintf("attendance:session:%d:next_pin", sessionID)
}

func attendanceSessionPreviousKey(sessionID uint) string {
	return fmt.Sprintf("attendance:session:%d:previous_pin", sessionID)
}

func attendanceSessionIdempotencyKey(sessionID uint, idempotencyKey string) string {
	return fmt.Sprintf("attendance:session:%d:start:%s", sessionID, strings.TrimSpace(idempotencyKey))
}

func attendanceSessionRedisTTL(state AttendanceRuntimeState) time.Duration {
	expiry := time.Now().Add(attendanceStateTTLBuffer)
	if state.ExpiresAt != nil && state.ExpiresAt.After(expiry) {
		expiry = state.ExpiresAt.Add(attendanceStateTTLBuffer)
	}
	if state.PreviousValidUntil != nil && state.PreviousValidUntil.After(expiry) {
		expiry = state.PreviousValidUntil.Add(attendanceStateTTLBuffer)
	}
	return time.Until(expiry)
}

func reserveAttendancePIN(ctx context.Context, sessionID uint, ttl time.Duration, disallowed ...string) (string, error) {
	if !attendanceRedisAvailable() {
		return "", ErrAttendanceRedisUnavailable
	}

	disallowedSet := make(map[string]struct{}, len(disallowed))
	for _, pin := range disallowed {
		normalized := strings.TrimSpace(pin)
		if normalized != "" {
			disallowedSet[normalized] = struct{}{}
		}
	}

	for attempt := 0; attempt < attendancePinReservationAttempts; attempt++ {
		pin := GeneratePIN()
		if _, exists := disallowedSet[pin]; exists {
			continue
		}
		reserved, err := config.Redis.SetNX(ctx, attendancePinKey(pin), sessionID, ttl).Result()
		if err != nil {
			observability.RecordAttendanceRedisFailure()
			log.Printf("event=redis_error action=reserve_attendance_pin session_id=%d err=%v", sessionID, err)
			return "", err
		}
		if reserved {
			log.Printf("event=attendance_pin_reserved session_id=%d pin_hash=%s", sessionID, attendancePinHash(pin))
			return pin, nil
		}
		observability.RecordAttendancePinCollision()
		log.Printf("event=attendance_pin_collision session_id=%d attempt=%d", sessionID, attempt+1)
	}
	return "", ErrAttendancePinUnavailable
}

func releaseAttendancePINs(ctx context.Context, pins ...string) {
	if !attendanceRedisAvailable() {
		return
	}
	keys := make([]string, 0, len(pins))
	for _, pin := range pins {
		normalized := strings.TrimSpace(pin)
		if normalized != "" {
			keys = append(keys, attendancePinKey(normalized))
		}
	}
	if len(keys) == 0 {
		return
	}
	if err := config.Redis.Del(ctx, keys...).Err(); err != nil {
		log.Printf("event=redis_error action=release_attendance_pins err=%v", err)
	}
}

func writeAttendanceRuntimeState(ctx context.Context, state AttendanceRuntimeState) error {
	if !attendanceRedisAvailable() {
		return ErrAttendanceRedisUnavailable
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	ttl := attendanceSessionRedisTTL(state)
	pipe := config.Redis.TxPipeline()
	pipe.Set(ctx, attendanceSessionStateKey(state.SessionID), payload, ttl)
	pipe.Set(ctx, attendanceSessionCurrentKey(state.SessionID), state.CurrentPIN, ttl)
	if strings.TrimSpace(state.NextPIN) != "" {
		pipe.Set(ctx, attendanceSessionNextKey(state.SessionID), state.NextPIN, ttl)
	} else {
		pipe.Del(ctx, attendanceSessionNextKey(state.SessionID))
	}
	if strings.TrimSpace(state.PreviousPIN) != "" && state.PreviousValidUntil != nil && state.PreviousValidUntil.After(time.Now()) {
		pipe.Set(ctx, attendanceSessionPreviousKey(state.SessionID), state.PreviousPIN, time.Until(*state.PreviousValidUntil)+attendanceStateTTLBuffer)
	} else {
		pipe.Del(ctx, attendanceSessionPreviousKey(state.SessionID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		observability.RecordAttendanceRedisFailure()
		return err
	}
	return nil
}

func readAttendanceRuntimeState(ctx context.Context, sessionID uint) (*AttendanceRuntimeState, error) {
	if !attendanceRedisAvailable() {
		return nil, ErrAttendanceRedisUnavailable
	}
	raw, err := config.Redis.Get(ctx, attendanceSessionStateKey(sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		observability.RecordAttendanceRedisFailure()
		return nil, err
	}

	var state AttendanceRuntimeState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func deleteAttendanceRuntimeState(ctx context.Context, sessionID uint, state *AttendanceRuntimeState) error {
	if !attendanceRedisAvailable() {
		return ErrAttendanceRedisUnavailable
	}
	keys := []string{
		attendanceSessionStateKey(sessionID),
		attendanceSessionCurrentKey(sessionID),
		attendanceSessionNextKey(sessionID),
		attendanceSessionPreviousKey(sessionID),
	}
	if state != nil {
		for _, pin := range []string{state.CurrentPIN, state.NextPIN, state.PreviousPIN} {
			if strings.TrimSpace(pin) != "" {
				keys = append(keys, attendancePinKey(pin))
			}
		}
	}
	return config.Redis.Del(ctx, keys...).Err()
}

func persistAttendancePinHistory(tx *gorm.DB, sessionID uint, pin string, validFrom time.Time, validUntil time.Time, reason string) error {
	if strings.TrimSpace(pin) == "" {
		return nil
	}
	return tx.Create(&models.AttendancePinHistory{
		SessionID:  sessionID,
		PinHash:    attendancePinHash(pin),
		ValidFrom:  validFrom,
		ValidUntil: validUntil,
		Reason:     reason,
		CreatedAt:  time.Now(),
	}).Error
}

func applyAttendanceRuntimeStateToSession(session *models.AttendanceSession, state *AttendanceRuntimeState) {
	if session == nil {
		return
	}
	session.PinCode = ""
	session.PreviousPinCode = ""
	session.PinIssuedAt = nil
	session.PinRotatesAt = nil
	session.PinGraceUntil = nil
	if state == nil {
		return
	}
	session.PinCode = state.CurrentPIN
	session.PreviousPinCode = state.PreviousPIN
	session.PinIssuedAt = state.PinIssuedAt
	session.PinRotatesAt = state.NextRotationAt
	session.PinGraceUntil = state.PreviousValidUntil
	session.PinMode = state.Mode
	session.Status = state.Status
	session.StartedAt = state.StartedAt
	session.ExpiresAt = state.ExpiresAt
	session.ClosedAt = state.ClosedAt
}

func buildAttendanceRuntimeState(ctx context.Context, db *gorm.DB, session *models.AttendanceSession, reason string) (*AttendanceRuntimeState, error) {
	if session == nil {
		return nil, errors.New("attendance session is required")
	}
	if !attendanceRedisAvailable() {
		return nil, ErrAttendanceRedisUnavailable
	}

	now := time.Now()
	mode := attendanceModeForSession(session)
	if session.EndTime.Before(now) {
		session.EndTime = now.Add(time.Minute)
	}
	expiresAt := session.EndTime
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		ttl = time.Minute
		expiresAt = now.Add(ttl)
	}

	currentPIN, err := reserveAttendancePIN(ctx, session.ID, ttl)
	if err != nil {
		return nil, err
	}

	state := &AttendanceRuntimeState{
		SessionID:   session.ID,
		Mode:        mode,
		Status:      "active",
		CurrentPIN:  currentPIN,
		PinIssuedAt: &now,
		StartedAt:   &now,
		ExpiresAt:   &expiresAt,
	}

	if mode == "rotating" {
		rotationAt := now.Add(attendancePinRotationWindow())
		nextPIN, err := reserveAttendancePIN(ctx, session.ID, ttl, currentPIN)
		if err != nil {
			releaseAttendancePINs(ctx, currentPIN)
			return nil, err
		}
		state.NextPIN = nextPIN
		state.NextRotationAt = &rotationAt
	}

	if db == nil {
		db = config.DB
	}
	updates := map[string]interface{}{
		"status":            "active",
		"pin_mode":          mode,
		"pin_hash":          attendancePinHash(state.CurrentPIN),
		"current_pin_hash":  attendancePinHash(state.CurrentPIN),
		"previous_pin_hash": "",
		"pin_code":          "",
		"previous_pin_code": "",
		"pin_issued_at":     now,
		"pin_rotates_at":    state.NextRotationAt,
		"pin_grace_until":   nil,
		"started_at":        now,
		"expires_at":        expiresAt,
		"closed_at":         nil,
		"start_time":        now,
		"end_time":          expiresAt,
		"auto_rotate_pin":   mode == "rotating",
	}
	err = db.Model(&models.AttendanceSession{}).Where("id = ?", session.ID).Updates(updates).Error
	if err == nil {
		err = persistAttendancePinHistory(db, session.ID, state.CurrentPIN, now, expiresAt, reason)
	}
	if err != nil {
		releaseAttendancePINs(ctx, state.CurrentPIN, state.NextPIN)
		observability.RecordAttendanceDBInsertFailure()
		return nil, err
	}

	if err := writeAttendanceRuntimeState(ctx, *state); err != nil {
		releaseAttendancePINs(ctx, state.CurrentPIN, state.NextPIN)
		return nil, err
	}

	session.Status = "active"
	session.PinMode = mode
	session.PinHash = attendancePinHash(state.CurrentPIN)
	session.CurrentPinHash = session.PinHash
	session.PreviousPinHash = ""
	session.PinCode = state.CurrentPIN
	session.PreviousPinCode = ""
	session.PinIssuedAt = state.PinIssuedAt
	session.PinRotatesAt = state.NextRotationAt
	session.PinGraceUntil = nil
	session.StartedAt = state.StartedAt
	session.ExpiresAt = state.ExpiresAt
	session.ClosedAt = nil
	log.Printf("event=attendance_session_created session_id=%d mode=%s", session.ID, mode)
	return state, nil
}

func getAttendanceRuntimeState(ctx context.Context, session *models.AttendanceSession, allowRehydrate bool) (*AttendanceRuntimeState, error) {
	if session == nil {
		return nil, errors.New("attendance session is required")
	}
	state, err := readAttendanceRuntimeState(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	if state != nil {
		return state, nil
	}
	if !allowRehydrate || session.Status != "active" {
		return nil, nil
	}
	log.Printf("event=attendance_session_rehydrating session_id=%d", session.ID)
	return buildAttendanceRuntimeState(ctx, config.DB, session, "manual_regenerate")
}

func StartAttendanceSession(ctx context.Context, sessionID uint, idempotencyKey string) (*AttendanceStartResult, error) {
	if !attendanceRedisAvailable() {
		return nil, ErrAttendanceRedisUnavailable
	}

	if normalizedKey := strings.TrimSpace(idempotencyKey); normalizedKey != "" {
		cached, err := config.Redis.Get(ctx, attendanceSessionIdempotencyKey(sessionID, normalizedKey)).Result()
		if err == nil {
			var result AttendanceStartResult
			if json.Unmarshal([]byte(cached), &result) == nil {
				return &result, nil
			}
		}
	}

	var result *AttendanceStartResult
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var session models.AttendanceSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; err != nil {
			return err
		}

		state, err := readAttendanceRuntimeState(ctx, sessionID)
		if err != nil {
			return err
		}
		if state == nil {
			state, err = buildAttendanceRuntimeState(ctx, tx, &session, "initial")
			if err != nil {
				return err
			}
		}

		applyAttendanceRuntimeStateToSession(&session, state)
		result = &AttendanceStartResult{
			SessionID:      session.ID,
			CurrentPIN:     state.CurrentPIN,
			ExpiresAt:      state.ExpiresAt,
			NextRotationAt: state.NextRotationAt,
			State:          *state,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if result != nil && strings.TrimSpace(idempotencyKey) != "" {
		if payload, marshalErr := json.Marshal(result); marshalErr == nil {
			_ = config.Redis.Set(ctx, attendanceSessionIdempotencyKey(sessionID, idempotencyKey), payload, attendanceStartIdempotencyTTL).Err()
		}
	}
	return result, nil
}

func RotateAttendanceSessionPIN(ctx context.Context, sessionID uint, reason string) (*AttendanceRuntimeState, error) {
	if !attendanceRedisAvailable() {
		return nil, ErrAttendanceRedisUnavailable
	}

	var rotatedState *AttendanceRuntimeState
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var session models.AttendanceSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; err != nil {
			return err
		}
		if session.Status != "active" {
			return ErrAttendanceSessionClosed
		}

		state, err := getAttendanceRuntimeState(ctx, &session, true)
		if err != nil {
			return err
		}
		if state == nil {
			return ErrAttendancePinUnavailable
		}
		if state.Mode != "rotating" {
			rotatedState = state
			return nil
		}

		now := time.Now()
		graceUntil := now.Add(attendancePinGraceWindow())
		newNextPIN, err := reserveAttendancePIN(ctx, session.ID, time.Until(*state.ExpiresAt), state.CurrentPIN, state.NextPIN)
		if err != nil {
			return err
		}

		previousPIN := state.CurrentPIN
		currentPIN := state.NextPIN
		nextPIN := newNextPIN
		nextRotationAt := now.Add(attendancePinRotationWindow())

		releaseAttendancePINs(ctx, state.CurrentPIN)
		if err := config.Redis.Set(ctx, attendancePinKey(previousPIN), session.ID, time.Until(graceUntil)).Err(); err != nil {
			releaseAttendancePINs(ctx, newNextPIN)
			return err
		}

		state.PreviousPIN = previousPIN
		state.PreviousValidUntil = &graceUntil
		state.CurrentPIN = currentPIN
		state.NextPIN = nextPIN
		state.PinIssuedAt = &now
		state.NextRotationAt = &nextRotationAt

		if err := tx.Model(&models.AttendanceSession{}).Where("id = ?", session.ID).Updates(map[string]interface{}{
			"pin_hash":          attendancePinHash(currentPIN),
			"current_pin_hash":  attendancePinHash(currentPIN),
			"previous_pin_hash": attendancePinHash(previousPIN),
			"pin_issued_at":     now,
			"pin_grace_until":   graceUntil,
			"pin_rotates_at":    nextRotationAt,
		}).Error; err != nil {
			releaseAttendancePINs(ctx, newNextPIN)
			return err
		}
		if err := persistAttendancePinHistory(tx, session.ID, currentPIN, now, *state.ExpiresAt, reason); err != nil {
			releaseAttendancePINs(ctx, newNextPIN)
			return err
		}
		if err := writeAttendanceRuntimeState(ctx, *state); err != nil {
			releaseAttendancePINs(ctx, newNextPIN)
			return err
		}

		rotatedState = state
		observability.RecordAttendancePinRotation()
		log.Printf("event=attendance_pin_rotation_completed session_id=%d", session.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rotatedState, nil
}

func CloseAttendanceRuntimeSession(ctx context.Context, sessionID uint) (*AttendanceRuntimeState, error) {
	var finalState *AttendanceRuntimeState
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var session models.AttendanceSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; err != nil {
			return err
		}

		state, err := readAttendanceRuntimeState(ctx, sessionID)
		if err != nil && !errors.Is(err, ErrAttendanceRedisUnavailable) {
			return err
		}

		now := time.Now()
		if err := tx.Model(&models.AttendanceSession{}).Where("id = ?", session.ID).Updates(map[string]interface{}{
			"status":            "closed",
			"closed_at":         now,
			"expires_at":        now,
			"end_time":          now,
			"pin_hash":          "",
			"current_pin_hash":  "",
			"previous_pin_hash": "",
			"pin_code":          "",
			"previous_pin_code": "",
			"pin_issued_at":     nil,
			"pin_rotates_at":    nil,
			"pin_grace_until":   nil,
		}).Error; err != nil {
			return err
		}

		finalState = &AttendanceRuntimeState{
			SessionID: session.ID,
			Mode:      attendanceModeForSession(&session),
			Status:    "closed",
			ClosedAt:  &now,
			ExpiresAt: &now,
		}
		if state != nil {
			finalState.CurrentPIN = state.CurrentPIN
			finalState.NextPIN = state.NextPIN
			finalState.PreviousPIN = state.PreviousPIN
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := deleteAttendanceRuntimeState(ctx, sessionID, finalState); err != nil && !errors.Is(err, ErrAttendanceRedisUnavailable) {
		log.Printf("event=redis_error action=close_attendance_session session_id=%d err=%v", sessionID, err)
	}
	log.Printf("event=attendance_session_closed session_id=%d", sessionID)
	return finalState, nil
}

func LookupAttendanceSessionIDByPIN(ctx context.Context, pin string) (uint, error) {
	normalizedPIN := strings.TrimSpace(pin)
	if normalizedPIN == "" {
		return 0, ErrAttendanceInvalidPIN
	}

	if attendanceRedisAvailable() {
		raw, err := config.Redis.Get(ctx, attendancePinKey(normalizedPIN)).Result()
		if err == nil {
			var sessionID uint
			if _, scanErr := fmt.Sscanf(raw, "%d", &sessionID); scanErr == nil && sessionID > 0 {
				return sessionID, nil
			}
		} else if !errors.Is(err, redis.Nil) {
			observability.RecordAttendanceRedisFailure()
			log.Printf("event=redis_error action=lookup_attendance_pin err=%v", err)
		}
	}

	var session models.AttendanceSession
	now := time.Now()
	err := config.DB.Where("status = 'active' AND (current_pin_hash = ? OR (previous_pin_hash = ? AND pin_grace_until IS NOT NULL AND pin_grace_until > ?))", attendancePinHash(normalizedPIN), attendancePinHash(normalizedPIN), now).First(&session).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrAttendanceInvalidPIN
		}
		return 0, err
	}
	return session.ID, nil
}

func ResolveAttendanceSessionPinState(ctx context.Context, sessionID uint, allowRehydrate bool) (*models.AttendanceSession, *AttendanceRuntimeState, error) {
	var session models.AttendanceSession
	if err := config.DB.First(&session, sessionID).Error; err != nil {
		return nil, nil, err
	}
	state, err := getAttendanceRuntimeState(ctx, &session, allowRehydrate)
	if err != nil {
		return nil, nil, err
	}
	applyAttendanceRuntimeStateToSession(&session, state)
	return &session, state, nil
}

func MaintainAttendanceRuntimeSessions(ctx context.Context, now time.Time) ([]AttendancePinStateChange, error) {
	var sessions []models.AttendanceSession
	if err := config.DB.Where("status = 'active'").Find(&sessions).Error; err != nil {
		return nil, err
	}

	changes := make([]AttendancePinStateChange, 0, len(sessions))
	for _, session := range sessions {
		session := session
		if session.ExpiresAt != nil && !session.ExpiresAt.After(now) {
			state, err := CloseAttendanceRuntimeSession(ctx, session.ID)
			if err != nil {
				return nil, err
			}
			change := AttendancePinStateChange{SessionID: session.ID, CourseID: session.CourseID, Status: "closed", Released: true, StatusChanged: true}
			if state != nil {
				change.PinCode = state.CurrentPIN
			}
			changes = append(changes, change)
			continue
		}

		state, err := getAttendanceRuntimeState(ctx, &session, true)
		if err != nil {
			return nil, err
		}
		if state == nil || state.Mode != "rotating" || state.NextRotationAt == nil || state.NextRotationAt.After(now) {
			continue
		}
		rotatedState, err := RotateAttendanceSessionPIN(ctx, session.ID, "rotation")
		if err != nil {
			return nil, err
		}
		changes = append(changes, AttendancePinStateChange{
			SessionID:    session.ID,
			CourseID:     session.CourseID,
			Status:       "active",
			PinCode:      rotatedState.CurrentPIN,
			PinIssuedAt:  rotatedState.PinIssuedAt,
			PinRotatesAt: rotatedState.NextRotationAt,
			Rotated:      true,
		})
	}
	return changes, nil
}
