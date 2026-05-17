package repositories

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"strings"
	"time"

	"gorm.io/datatypes"
)

const (
	AttendanceDisplayPairingTTL         = 2 * time.Minute
	AttendanceDisplayVerificationTTL    = 90 * time.Second
	AttendanceDisplayGrantMaxTTL        = 8 * time.Hour
	AttendanceDisplayGrantScopeLiveRead = "display:attendance-live:read"
)

var (
	ErrAttendanceDisplayUnauthorized = errors.New("display authorization failed")
	ErrAttendanceDisplayExpired      = errors.New("display session expired")
	ErrAttendanceDisplayInvalidCode  = errors.New("invalid verification code")
)

type AttendanceDisplayBootstrap struct {
	PairingID       string    `json:"pairing_id"`
	PairingToken    string    `json:"pairing_token"`
	BootstrapSecret string    `json:"bootstrap_secret"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type AttendanceDisplayPairingView struct {
	ID                  string                 `json:"id"`
	Status              string                 `json:"status"`
	CourseID            string                 `json:"course_id,omitempty"`
	AttendanceSessionID *uint                  `json:"attendance_session_id,omitempty"`
	ApprovedByUserID    *uint                  `json:"approved_by_user_id,omitempty"`
	ExpiresAt           time.Time              `json:"expires_at"`
	Session             *AttendanceSessionInfo `json:"session,omitempty"`
	DeviceHint          string                 `json:"device_hint,omitempty"`
}

type AttendanceDisplayClaimResult struct {
	Pairing          AttendanceDisplayPairingView `json:"pairing"`
	VerificationCode string                       `json:"verification_code"`
}

type AttendanceDisplayCurrent struct {
	GrantID             string                 `json:"grant_id"`
	Status              string                 `json:"status"`
	CourseID            string                 `json:"course_id"`
	AttendanceSessionID uint                   `json:"attendance_session_id"`
	Scope               string                 `json:"scope"`
	ExpiresAt           time.Time              `json:"expires_at"`
	Session             *AttendanceSessionInfo `json:"session,omitempty"`
}

func randomHex(bytesLen int) (string, error) {
	buffer := make([]byte, bytesLen)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func randomID(prefix string) (string, error) {
	token, err := randomHex(12)
	if err != nil {
		return "", err
	}
	return prefix + token, nil
}

func hashDisplaySecret(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func marshalDisplayMetadata(value map[string]interface{}) datatypes.JSON {
	if len(value) == 0 {
		return datatypes.JSON([]byte("{}"))
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(encoded)
}

func auditAttendanceDisplay(eventType string, deviceID string, pairingID string, grantID string, actorUserID *uint, ip string, userAgent string, metadata map[string]interface{}) {
	entry := models.AttendanceDisplayAuditLog{
		DisplayDeviceID: deviceID,
		PairingID:       pairingID,
		GrantID:         grantID,
		EventType:       eventType,
		ActorUserID:     actorUserID,
		IPHash:          hashDisplaySecret(ip),
		UserAgent:       strings.TrimSpace(userAgent),
		Metadata:        marshalDisplayMetadata(metadata),
		CreatedAt:       time.Now(),
	}
	_ = config.DB.Create(&entry).Error
}

func computeGrantExpiry(sessionEnd time.Time) time.Time {
	now := time.Now()
	maxExpiry := now.Add(AttendanceDisplayGrantMaxTTL)
	if !sessionEnd.IsZero() {
		withBuffer := sessionEnd.Add(5 * time.Minute)
		// Only use session-based expiry when it is still in the future.
		// If the session ended more than 5 min ago, fall through to maxExpiry.
		if withBuffer.After(now) && withBuffer.Before(maxExpiry) {
			return withBuffer
		}
	}
	return maxExpiry
}

func loadDisplayPairingByToken(token string) (*models.AttendanceDisplayPairing, error) {
	var pairing models.AttendanceDisplayPairing
	if err := config.DB.Where("pairing_token_hash = ?", hashDisplaySecret(token)).First(&pairing).Error; err != nil {
		return nil, err
	}
	return &pairing, nil
}

func loadDisplayDeviceByBootstrapSecret(secret string) (*models.AttendanceDisplayDevice, error) {
	var device models.AttendanceDisplayDevice
	if err := config.DB.Where("bootstrap_secret_hash = ?", hashDisplaySecret(secret)).First(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

// GetPairingStatusByBootstrap returns the current status of the pairing that belongs
// to the device identified by bootstrapSecret. It verifies ownership before returning
// anything so an unauthenticated caller can only poll their own pairing.
func GetPairingStatusByBootstrap(pairingID string, bootstrapSecret string) (string, error) {
	device, err := loadDisplayDeviceByBootstrapSecret(bootstrapSecret)
	if err != nil {
		return "", ErrAttendanceDisplayUnauthorized
	}
	var pairing models.AttendanceDisplayPairing
	if err := config.DB.Select("id", "status", "expires_at").
		Where("id = ? AND display_device_id = ?", strings.TrimSpace(pairingID), device.ID).
		First(&pairing).Error; err != nil {
		return "", ErrAttendanceDisplayUnauthorized
	}
	if time.Now().After(pairing.ExpiresAt) && pairing.Status == "pending" {
		_ = config.DB.Model(&pairing).Update("status", "expired").Error
		return "expired", nil
	}
	return pairing.Status, nil
}

func loadDisplayGrantBySessionSecret(secret string) (*models.AttendanceDisplayGrant, error) {
	var grant models.AttendanceDisplayGrant
	if err := config.DB.Where("session_secret_hash = ?", hashDisplaySecret(secret)).First(&grant).Error; err != nil {
		return nil, err
	}
	return &grant, nil
}

func BootstrapAttendanceDisplay(userAgent string, ip string) (*AttendanceDisplayBootstrap, error) {
	deviceID, err := randomID("addev_")
	if err != nil {
		return nil, err
	}
	bootstrapSecret, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	pairingID, err := randomID("adpair_")
	if err != nil {
		return nil, err
	}
	pairingToken, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	device := models.AttendanceDisplayDevice{
		ID:                  deviceID,
		BootstrapSecretHash: hashDisplaySecret(bootstrapSecret),
		Status:              "bootstrapped",
		LastIPHash:          hashDisplaySecret(ip),
		LastUserAgent:       strings.TrimSpace(userAgent),
		CreatedAt:           now,
		LastSeenAt:          &now,
		UpdatedAt:           now,
	}
	pairing := models.AttendanceDisplayPairing{
		ID:               pairingID,
		DisplayDeviceID:  deviceID,
		PairingTokenHash: hashDisplaySecret(pairingToken),
		Status:           "pending",
		ExpiresAt:        now.Add(AttendanceDisplayPairingTTL),
		AttemptCount:     0,
		MaxAttempts:      5,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	tx := config.DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	if err := tx.Create(&device).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Create(&pairing).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	auditAttendanceDisplay("bootstrap", deviceID, pairingID, "", nil, ip, userAgent, map[string]interface{}{"status": pairing.Status})

	return &AttendanceDisplayBootstrap{
		PairingID:       pairingID,
		PairingToken:    pairingToken,
		BootstrapSecret: bootstrapSecret,
		ExpiresAt:       pairing.ExpiresAt,
	}, nil
}

func GetAttendanceDisplayPairing(token string) (*AttendanceDisplayPairingView, error) {
	pairing, err := loadDisplayPairingByToken(token)
	if err != nil {
		return nil, err
	}
	if time.Now().After(pairing.ExpiresAt) && pairing.Status != "confirmed" {
		_ = config.DB.Model(pairing).Update("status", "expired").Error
		return nil, ErrAttendanceDisplayExpired
	}

	var session *AttendanceSessionInfo
	if pairing.AttendanceSessionID != nil && *pairing.AttendanceSessionID > 0 {
		session, _ = GetSessionInfo(*pairing.AttendanceSessionID)
	}

	var device models.AttendanceDisplayDevice
	if err := config.DB.Select("id", "last_user_agent").First(&device, "id = ?", pairing.DisplayDeviceID).Error; err != nil {
		device = models.AttendanceDisplayDevice{}
	}

	return &AttendanceDisplayPairingView{
		ID:                  pairing.ID,
		Status:              pairing.Status,
		CourseID:            pairing.CourseID,
		AttendanceSessionID: pairing.AttendanceSessionID,
		ApprovedByUserID:    pairing.ApprovedByUserID,
		ExpiresAt:           pairing.ExpiresAt,
		Session:             session,
		DeviceHint:          device.LastUserAgent,
	}, nil
}

func ClaimAttendanceDisplayPairing(token string, sessionID uint, userID uint, userRole string, userAgent string, ip string) (*AttendanceDisplayClaimResult, error) {
	pairing, err := loadDisplayPairingByToken(token)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if now.After(pairing.ExpiresAt) {
		_ = config.DB.Model(pairing).Update("status", "expired").Error
		return nil, ErrAttendanceDisplayExpired
	}
	if pairing.Status != "pending" && pairing.Status != "claimed" {
		return nil, fmt.Errorf("pairing is not claimable")
	}

	var session models.AttendanceSession
	if err := config.DB.First(&session, sessionID).Error; err != nil {
		return nil, err
	}
	if ComputeSessionStatus(session) == "closed" {
		return nil, fmt.Errorf("attendance session is already closed")
	}

	hasAccess, err := UserHasCourseAccess(session.CourseID, userID, "instructor", "ta")
	if err != nil {
		return nil, err
	}
	if !hasAccess {
		return nil, ErrAttendanceDisplayUnauthorized
	}
	hasPermission, err := HasCoursePermission(session.CourseID, userID, userRole, PermissionUpdateAttendanceSessions)
	if err != nil {
		return nil, err
	}
	if !hasPermission {
		return nil, ErrAttendanceDisplayUnauthorized
	}

	verificationCode := GeneratePIN()
	pairing.CourseID = session.CourseID
	pairing.AttendanceSessionID = &session.ID
	pairing.ApprovedByUserID = &userID
	pairing.Status = "claimed"
	pairing.VerificationCodeHash = hashDisplaySecret(verificationCode)
	pairing.AttemptCount = 0
	pairing.MaxAttempts = 5
	pairing.ExpiresAt = now.Add(AttendanceDisplayVerificationTTL)
	pairing.ClaimedAt = &now
	if err := config.DB.Save(pairing).Error; err != nil {
		return nil, err
	}

	auditAttendanceDisplay("claim", pairing.DisplayDeviceID, pairing.ID, "", &userID, ip, userAgent, map[string]interface{}{"attendance_session_id": session.ID, "course_id": session.CourseID})

	view, err := GetAttendanceDisplayPairing(token)
	if err != nil {
		return nil, err
	}
	return &AttendanceDisplayClaimResult{Pairing: *view, VerificationCode: verificationCode}, nil
}

func ConfirmAttendanceDisplayPairing(pairingID string, verificationCode string, bootstrapSecret string, userAgent string, ip string) (string, *AttendanceDisplayCurrent, error) {
	device, err := loadDisplayDeviceByBootstrapSecret(bootstrapSecret)
	if err != nil {
		return "", nil, err
	}

	var pairing models.AttendanceDisplayPairing
	if err := config.DB.Where("id = ? AND display_device_id = ?", strings.TrimSpace(pairingID), device.ID).First(&pairing).Error; err != nil {
		return "", nil, err
	}
	now := time.Now()
	if pairing.Status != "claimed" || now.After(pairing.ExpiresAt) {
		_ = config.DB.Model(&pairing).Update("status", "expired").Error
		return "", nil, ErrAttendanceDisplayExpired
	}
	if pairing.AttendanceSessionID == nil || pairing.ApprovedByUserID == nil {
		return "", nil, ErrAttendanceDisplayUnauthorized
	}
	if hashDisplaySecret(verificationCode) != pairing.VerificationCodeHash {
		attemptCount := pairing.AttemptCount + 1
		updates := map[string]interface{}{"attempt_count": attemptCount}
		if attemptCount >= pairing.MaxAttempts {
			updates["status"] = "expired"
		}
		_ = config.DB.Model(&pairing).Updates(updates).Error
		auditAttendanceDisplay("confirm_failed", device.ID, pairing.ID, "", pairing.ApprovedByUserID, ip, userAgent, map[string]interface{}{"attempt_count": attemptCount})
		return "", nil, ErrAttendanceDisplayInvalidCode
	}

	var session models.AttendanceSession
	if err := config.DB.First(&session, *pairing.AttendanceSessionID).Error; err != nil {
		return "", nil, err
	}

	sessionSecret, err := randomHex(32)
	if err != nil {
		return "", nil, err
	}
	grantID, err := randomID("adgrant_")
	if err != nil {
		return "", nil, err
	}
	grant := models.AttendanceDisplayGrant{
		ID:                  grantID,
		DisplayDeviceID:     device.ID,
		AttendanceSessionID: *pairing.AttendanceSessionID,
		CourseID:            pairing.CourseID,
		GrantedByUserID:     *pairing.ApprovedByUserID,
		Scope:               AttendanceDisplayGrantScopeLiveRead,
		SessionSecretHash:   hashDisplaySecret(sessionSecret),
		Status:              "active",
		ExpiresAt:           computeGrantExpiry(session.EndTime),
		LastSeenAt:          &now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	tx := config.DB.Begin()
	if tx.Error != nil {
		return "", nil, tx.Error
	}
	if err := tx.Model(&models.AttendanceDisplayGrant{}).Where("display_device_id = ? AND status = ?", device.ID, "active").Updates(map[string]interface{}{"status": "revoked", "revoked_at": now, "revoke_reason": "superseded_by_new_pairing"}).Error; err != nil {
		tx.Rollback()
		return "", nil, err
	}
	if err := tx.Create(&grant).Error; err != nil {
		tx.Rollback()
		return "", nil, err
	}
	pairing.Status = "confirmed"
	pairing.ConfirmedAt = &now
	if err := tx.Save(&pairing).Error; err != nil {
		tx.Rollback()
		return "", nil, err
	}
	if err := tx.Model(device).Updates(map[string]interface{}{"status": "paired", "last_seen_at": now, "last_ip_hash": hashDisplaySecret(ip), "last_user_agent": strings.TrimSpace(userAgent)}).Error; err != nil {
		tx.Rollback()
		return "", nil, err
	}
	if err := tx.Commit().Error; err != nil {
		return "", nil, err
	}

	auditAttendanceDisplay("confirm_success", device.ID, pairing.ID, grant.ID, pairing.ApprovedByUserID, ip, userAgent, map[string]interface{}{"attendance_session_id": grant.AttendanceSessionID})

	current, err := GetAttendanceDisplayCurrent(sessionSecret)
	if err != nil {
		return "", nil, err
	}
	return sessionSecret, current, nil
}

func ResolveAttendanceDisplayGrant(sessionSecret string) (*models.AttendanceDisplayGrant, error) {
	grant, err := loadDisplayGrantBySessionSecret(sessionSecret)
	if err != nil {
		return nil, err
	}
	if grant.Status != "active" || time.Now().After(grant.ExpiresAt) {
		if grant.Status == "active" {
			_ = config.DB.Model(grant).Updates(map[string]interface{}{"status": "expired", "revoked_at": time.Now(), "revoke_reason": "expired"}).Error
		}
		return nil, ErrAttendanceDisplayExpired
	}
	return grant, nil
}

func GetAttendanceDisplayCurrent(sessionSecret string) (*AttendanceDisplayCurrent, error) {
	grant, err := ResolveAttendanceDisplayGrant(sessionSecret)
	if err != nil {
		return nil, err
	}
	session, err := GetSessionInfo(grant.AttendanceSessionID)
	if err != nil {
		return nil, err
	}
	return &AttendanceDisplayCurrent{
		GrantID:             grant.ID,
		Status:              grant.Status,
		CourseID:            grant.CourseID,
		AttendanceSessionID: grant.AttendanceSessionID,
		Scope:               grant.Scope,
		ExpiresAt:           grant.ExpiresAt,
		Session:             session,
	}, nil
}

func GetAttendanceDisplayRecords(sessionSecret string) ([]AttendanceRecordWithStudent, error) {
	grant, err := ResolveAttendanceDisplayGrant(sessionSecret)
	if err != nil {
		return nil, err
	}
	detail, err := GetAttendanceSession(grant.AttendanceSessionID)
	if err != nil {
		return nil, err
	}
	return detail.Records, nil
}

func TouchAttendanceDisplayGrant(sessionSecret string, userAgent string, ip string) (*AttendanceDisplayCurrent, error) {
	grant, err := loadDisplayGrantBySessionSecret(sessionSecret)
	if err != nil {
		return nil, err
	}
	if grant.Status != "active" || time.Now().After(grant.ExpiresAt) {
		return nil, ErrAttendanceDisplayExpired
	}
	now := time.Now()
	if err := config.DB.Model(grant).Update("last_seen_at", now).Error; err != nil {
		return nil, err
	}
	if userAgent != "" || ip != "" {
		auditAttendanceDisplay("heartbeat", grant.DisplayDeviceID, "", grant.ID, &grant.GrantedByUserID, ip, userAgent, nil)
	}
	return GetAttendanceDisplayCurrent(sessionSecret)
}

func RevokeAttendanceDisplayGrant(grantID string, userID uint, userRole string, userAgent string, ip string) error {
	var grant models.AttendanceDisplayGrant
	if err := config.DB.Where("id = ?", strings.TrimSpace(grantID)).First(&grant).Error; err != nil {
		return err
	}
	hasAccess, err := UserHasCourseAccess(grant.CourseID, userID, "instructor", "ta")
	if err != nil {
		return err
	}
	if !hasAccess {
		return ErrAttendanceDisplayUnauthorized
	}
	hasPermission, err := HasCoursePermission(grant.CourseID, userID, userRole, PermissionUpdateAttendanceSessions)
	if err != nil {
		return err
	}
	if !hasPermission {
		return ErrAttendanceDisplayUnauthorized
	}
	now := time.Now()
	if err := config.DB.Model(&grant).Updates(map[string]interface{}{"status": "revoked", "revoked_at": now, "revoke_reason": "manual_revoke"}).Error; err != nil {
		return err
	}
	auditAttendanceDisplay("revoke", grant.DisplayDeviceID, "", grant.ID, &userID, ip, userAgent, map[string]interface{}{"role": userRole})
	return nil
}
