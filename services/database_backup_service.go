package services

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/repositories"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	BackupStepUpActionRunNow   = "system_settings.backups.run_now"
	BackupStepUpActionRestore  = "system_settings.backups.restore"
	BackupStepUpActionDownload = "system_settings.backups.download"

	dailyBackupStateConfigKey = "system.backup.daily_last_success_date"
	dailyBackupLockKey        = "backup:daily:lock"
	// Longer than the 30-minute timeout inside RunDatabaseBackupNow, so the
	// lock cannot expire while a backup is still running, but short enough that
	// a process killed mid-backup does not block tomorrow's attempt.
	dailyBackupLockTTL      = 35 * time.Minute
	backupStatusConfigKey   = "system.backup.status"
	backupStorageProviderR2 = "cloudflare_r2"
	backupRetentionSlots    = 7
)

type BackupOperationStatus struct {
	Running       bool       `json:"running"`
	LastTrigger   string     `json:"last_trigger,omitempty"`
	LastStatus    string     `json:"last_status,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastBackupID  *uint      `json:"last_backup_id,omitempty"`
	LastBackupAt  *time.Time `json:"last_backup_at,omitempty"`
	LastRestoreAt *time.Time `json:"last_restore_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type backupManifest struct {
	BackupID    uint      `json:"backup_id"`
	BackupName  string    `json:"backup_name"`
	StoragePath string    `json:"storage_path"`
	StorageSlot int       `json:"storage_slot"`
	Checksum    string    `json:"checksum_sha256"`
	FileSize    int64     `json:"file_size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
	Encrypted   bool      `json:"encrypted"`
	Provider    string    `json:"provider"`
	Schema      int       `json:"schema_version"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type r2Config struct {
	Endpoint  string
	Secure    bool
	Bucket    string
	AccessKey string
	SecretKey string
	Prefix    string
}

type dbConnConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

var backupOpState = struct {
	sync.Mutex
	running bool
}{}

func StartDailyDatabaseBackupWorker() {
	if _, err := loadR2Config(); err != nil {
		log.Printf("⚠️  Daily backup worker disabled: %v", err)
		return
	}

	location := time.UTC
	if tzName := strings.TrimSpace(os.Getenv("BACKUP_TIMEZONE")); tzName != "" {
		if loc, err := time.LoadLocation(tzName); err == nil {
			location = loc
		}
	} else if loc, err := time.LoadLocation("Asia/Bangkok"); err == nil {
		location = loc
	}

	go func() {
		tryRunDailyBackup(location)

		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			tryRunDailyBackup(location)
		}
	}()
}

func tryRunDailyBackup(location *time.Location) {
	now := time.Now().In(location)
	hour := envInt("BACKUP_DAILY_HOUR", 2)
	minute := envInt("BACKUP_DAILY_MINUTE", 30)
	if hour < 0 || hour > 23 {
		hour = 2
	}
	if minute < 0 || minute > 59 {
		minute = 30
	}

	scheduled := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, location)
	if now.Before(scheduled) {
		return
	}

	today := now.Format("2006-01-02")
	lastRunDate, err := repositories.GetAppConfigValue(dailyBackupStateConfigKey)
	if err == nil && strings.TrimSpace(lastRunDate) == today {
		return
	}

	// The date marker above is only written *after* a backup finishes, and a
	// backup takes minutes. Under blue-green both backend slots run this loop,
	// so without a lock the standby ticks mid-backup, still sees no marker for
	// today, and starts a second concurrent pg_dump against the live database.
	//
	// beginBackupOperation() cannot cover this — it is an in-process mutex and
	// the two slots are separate processes.
	if !acquireDailyBackupLock() {
		log.Println("ℹ️  Daily backup already running on another instance, skipping this tick")
		return
	}
	defer releaseDailyBackupLock()

	// Re-check under the lock: the other instance may have finished between the
	// marker read above and the lock being acquired here.
	if lastRunDate, err := repositories.GetAppConfigValue(dailyBackupStateConfigKey); err == nil &&
		strings.TrimSpace(lastRunDate) == today {
		return
	}

	record, backupErr := RunDatabaseBackupNow(nil, "scheduled_daily_backup")
	if backupErr != nil {
		_ = setBackupStatus(BackupOperationStatus{
			Running:     false,
			LastTrigger: "scheduled",
			LastStatus:  "failed",
			LastError:   backupErr.Error(),
			UpdatedAt:   time.Now(),
		})
		log.Printf("⚠️  Daily backup failed: %v", backupErr)
		return
	}

	if setErr := repositories.SetAppConfigValue(dailyBackupStateConfigKey, today); setErr != nil {
		log.Printf("⚠️  Daily backup succeeded but failed to persist state: %v", setErr)
	}
	log.Printf("✅ Daily backup succeeded: id=%d slot=%d", record.ID, record.StorageSlot)
}

func RunDatabaseBackupNow(actorUserID *uint, reason string) (models.DatabaseBackupRecord, error) {
	if !beginBackupOperation() {
		return models.DatabaseBackupRecord{}, errors.New("backup operation is already running")
	}
	defer endBackupOperation()

	trigger := "manual"
	if actorUserID == nil {
		trigger = "scheduled"
	}
	_ = setBackupStatus(BackupOperationStatus{
		Running:     true,
		LastTrigger: trigger,
		LastStatus:  "running",
		UpdatedAt:   time.Now(),
	})

	r2Cfg, err := loadR2Config()
	if err != nil {
		_ = setBackupStatus(BackupOperationStatus{
			Running:     false,
			LastTrigger: trigger,
			LastStatus:  "failed",
			LastError:   err.Error(),
			UpdatedAt:   time.Now(),
		})
		return models.DatabaseBackupRecord{}, err
	}
	dbCfg, err := loadDBConnConfig()
	if err != nil {
		_ = setBackupStatus(BackupOperationStatus{
			Running:     false,
			LastTrigger: trigger,
			LastStatus:  "failed",
			LastError:   err.Error(),
			UpdatedAt:   time.Now(),
		})
		return models.DatabaseBackupRecord{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "itii-db-backup-*")
	if err != nil {
		return models.DatabaseBackupRecord{}, err
	}
	defer os.RemoveAll(tmpDir)

	dumpPath := filepath.Join(tmpDir, "database.dump")
	if err := runPgDump(ctx, dbCfg, dumpPath); err != nil {
		return models.DatabaseBackupRecord{}, err
	}

	dumpBytes, err := os.ReadFile(dumpPath)
	if err != nil {
		return models.DatabaseBackupRecord{}, err
	}
	checksum := computeChecksumSHA256(dumpBytes)

	payload := dumpBytes
	encrypted := false
	if key, keyErr := loadBackupEncryptionKey(); keyErr == nil {
		encryptedPayload, encErr := encryptPayload(key, dumpBytes)
		if encErr != nil {
			return models.DatabaseBackupRecord{}, encErr
		}
		payload = encryptedPayload
		encrypted = true
	} else if keyErr != nil && !errors.Is(keyErr, os.ErrNotExist) {
		return models.DatabaseBackupRecord{}, keyErr
	}

	now := time.Now()
	slot := computeBackupSlot(now)
	fileSuffix := ".dump"
	if encrypted {
		fileSuffix = ".dump.enc"
	}
	backupName := fmt.Sprintf("db-backup-%s-slot-%d%s", now.Format("20060102-150405"), slot, fileSuffix)
	storagePath := buildBackupObjectKey(r2Cfg.Prefix, backupName)

	client, err := createR2Client(r2Cfg)
	if err != nil {
		return models.DatabaseBackupRecord{}, err
	}

	if err := uploadObject(ctx, client, r2Cfg, storagePath, payload); err != nil {
		return models.DatabaseBackupRecord{}, err
	}

	record, err := repositories.CreateDatabaseBackupRecord(models.DatabaseBackupRecord{
		BackupName:      backupName,
		StoragePath:     storagePath,
		StorageProvider: backupStorageProviderR2,
		StorageSlot:     slot,
		ChecksumSHA256:  checksum,
		FileSizeBytes:   int64(len(payload)),
		CreatedBy:       actorUserID,
	})
	if err != nil {
		return models.DatabaseBackupRecord{}, err
	}

	manifestRaw, _ := json.Marshal(backupManifest{
		BackupID:    record.ID,
		BackupName:  record.BackupName,
		StoragePath: record.StoragePath,
		StorageSlot: record.StorageSlot,
		Checksum:    record.ChecksumSHA256,
		FileSize:    record.FileSizeBytes,
		CreatedAt:   record.CreatedAt,
		Encrypted:   encrypted,
		Provider:    backupStorageProviderR2,
		Schema:      1,
		UpdatedAt:   time.Now(),
	})
	_ = uploadObject(ctx, client, r2Cfg, buildLatestManifestKey(r2Cfg.Prefix), manifestRaw)

	nowStatus := time.Now()
	_ = setBackupStatus(BackupOperationStatus{
		Running:      false,
		LastTrigger:  trigger,
		LastStatus:   "success",
		LastBackupID: &record.ID,
		LastBackupAt: &nowStatus,
		UpdatedAt:    nowStatus,
	})
	_ = reason
	return record, nil
}

func RestoreDatabaseFromBackup(record models.DatabaseBackupRecord) error {
	if !beginBackupOperation() {
		return errors.New("backup operation is already running")
	}
	defer endBackupOperation()
	_ = setBackupStatus(BackupOperationStatus{
		Running:     true,
		LastTrigger: "restore",
		LastStatus:  "running",
		UpdatedAt:   time.Now(),
	})

	r2Cfg, err := loadR2Config()
	if err != nil {
		_ = setBackupStatus(BackupOperationStatus{
			Running:     false,
			LastTrigger: "restore",
			LastStatus:  "failed",
			LastError:   err.Error(),
			UpdatedAt:   time.Now(),
		})
		return err
	}
	dbCfg, err := loadDBConnConfig()
	if err != nil {
		_ = setBackupStatus(BackupOperationStatus{
			Running:     false,
			LastTrigger: "restore",
			LastStatus:  "failed",
			LastError:   err.Error(),
			UpdatedAt:   time.Now(),
		})
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	client, err := createR2Client(r2Cfg)
	if err != nil {
		return err
	}

	payload, err := downloadObject(ctx, client, r2Cfg, record.StoragePath)
	if err != nil {
		return err
	}

	dumpPayload := payload
	if strings.HasSuffix(strings.ToLower(record.StoragePath), ".enc") {
		key, keyErr := loadBackupEncryptionKey()
		if keyErr != nil {
			return fmt.Errorf("missing BACKUP_ENCRYPTION_KEY for encrypted backup restore")
		}
		decrypted, decErr := decryptPayload(key, payload)
		if decErr != nil {
			return decErr
		}
		dumpPayload = decrypted
	}

	if expectedChecksum := strings.TrimSpace(record.ChecksumSHA256); expectedChecksum != "" {
		dumpChecksum := computeChecksumSHA256(dumpPayload)
		if !strings.EqualFold(dumpChecksum, expectedChecksum) {
			// Backward compatibility: some legacy records may store checksum of the raw object payload.
			rawChecksum := computeChecksumSHA256(payload)
			if !strings.EqualFold(rawChecksum, expectedChecksum) {
				return errors.New("backup checksum mismatch")
			}
			log.Printf("⚠️  Restore checksum matched raw payload checksum (legacy mode): backup_id=%d", record.ID)
		}
	}

	tmpDir, err := os.MkdirTemp("", "itii-db-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	dumpPath := filepath.Join(tmpDir, "restore.dump")
	if err := os.WriteFile(dumpPath, dumpPayload, 0o600); err != nil {
		return err
	}

	if err := runPgRestore(ctx, dbCfg, dumpPath); err != nil {
		_ = setBackupStatus(BackupOperationStatus{
			Running:     false,
			LastTrigger: "restore",
			LastStatus:  "failed",
			LastError:   err.Error(),
			UpdatedAt:   time.Now(),
		})
		return err
	}

	nowStatus := time.Now()
	_ = setBackupStatus(BackupOperationStatus{
		Running:       false,
		LastTrigger:   "restore",
		LastStatus:    "success",
		LastBackupID:  &record.ID,
		LastRestoreAt: &nowStatus,
		UpdatedAt:     nowStatus,
	})
	return nil
}

func BuildBackupDownloadURL(storagePath string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(storagePath) == "" {
		return "", errors.New("storage path is required")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	r2Cfg, err := loadR2Config()
	if err != nil {
		return "", err
	}
	client, err := createR2Client(r2Cfg)
	if err != nil {
		return "", err
	}
	u, err := client.PresignedGetObject(context.Background(), r2Cfg.Bucket, storagePath, ttl, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func GetBackupOperationStatus() (BackupOperationStatus, error) {
	status, err := loadBackupStatus()
	if err != nil {
		return BackupOperationStatus{}, err
	}
	status.Running = isBackupOperationRunning()
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now()
	}
	return status, nil
}

func CheckR2Health() (string, string) {
	r2Cfg, err := loadR2Config()
	if err != nil {
		return "down", err.Error()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	client, err := createR2Client(r2Cfg)
	if err != nil {
		return "down", err.Error()
	}

	if _, err := client.BucketExists(ctx, r2Cfg.Bucket); err != nil {
		return "down", err.Error()
	}

	return "up", "R2 bucket reachable"
}

func IsBackupOperationRunning() bool {
	return isBackupOperationRunning()
}

func beginBackupOperation() bool {
	backupOpState.Lock()
	defer backupOpState.Unlock()
	if backupOpState.running {
		return false
	}
	backupOpState.running = true
	return true
}

func isBackupOperationRunning() bool {
	backupOpState.Lock()
	defer backupOpState.Unlock()
	return backupOpState.running
}

func endBackupOperation() {
	backupOpState.Lock()
	backupOpState.running = false
	backupOpState.Unlock()
}

func loadR2Config() (r2Config, error) {
	cfg := r2Config{
		Endpoint:  strings.TrimSpace(os.Getenv("R2_ENDPOINT")),
		Bucket:    strings.TrimSpace(os.Getenv("R2_BUCKET")),
		AccessKey: strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID")),
		SecretKey: strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY")),
		Prefix:    strings.Trim(strings.TrimSpace(os.Getenv("R2_BACKUP_PREFIX")), "/"),
	}
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return r2Config{}, errors.New("missing R2 configuration (R2_ENDPOINT, R2_BUCKET, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY)")
	}

	cfg.Secure = true
	if secureRaw := strings.TrimSpace(os.Getenv("R2_SECURE")); secureRaw != "" {
		parsed, err := strconv.ParseBool(secureRaw)
		if err == nil {
			cfg.Secure = parsed
		}
	}

	if strings.HasPrefix(cfg.Endpoint, "http://") || strings.HasPrefix(cfg.Endpoint, "https://") {
		parsedURL, err := url.Parse(cfg.Endpoint)
		if err != nil || parsedURL.Host == "" {
			return r2Config{}, errors.New("invalid R2_ENDPOINT")
		}
		cfg.Endpoint = parsedURL.Host
		cfg.Secure = strings.EqualFold(parsedURL.Scheme, "https")
	}
	return cfg, nil
}

func loadDBConnConfig() (dbConnConfig, error) {
	cfg := dbConnConfig{
		Host:     strings.TrimSpace(os.Getenv("DB_HOST")),
		Port:     strings.TrimSpace(os.Getenv("DB_PORT")),
		User:     strings.TrimSpace(os.Getenv("DB_USER")),
		Password: os.Getenv("DB_PASSWORD"),
		Name:     strings.TrimSpace(os.Getenv("DB_NAME")),
	}
	if cfg.Host == "" || cfg.Port == "" || cfg.User == "" || cfg.Name == "" {
		return dbConnConfig{}, errors.New("missing database connection env for backup/restore")
	}
	return cfg, nil
}

func createR2Client(cfg r2Config) (*minio.Client, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func uploadObject(ctx context.Context, client *minio.Client, cfg r2Config, objectKey string, payload []byte) error {
	_, err := client.PutObject(
		ctx,
		cfg.Bucket,
		objectKey,
		bytes.NewReader(payload),
		int64(len(payload)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	return err
}

func downloadObject(ctx context.Context, client *minio.Client, cfg r2Config, objectKey string) ([]byte, error) {
	obj, err := client.GetObject(ctx, cfg.Bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func computeBackupSlot(now time.Time) int {
	dayIndex := now.UTC().YearDay() - 1
	if dayIndex < 0 {
		dayIndex = 0
	}
	return dayIndex % backupRetentionSlots
}

func buildBackupObjectKey(prefix string, backupName string) string {
	cleanBackupName := strings.TrimSpace(backupName)
	if cleanBackupName == "" {
		cleanBackupName = fmt.Sprintf("db-backup-%d.dump", time.Now().Unix())
	}
	if prefix == "" {
		return path.Join("backups", "db", cleanBackupName)
	}
	return path.Join(prefix, cleanBackupName)
}

func buildLatestManifestKey(prefix string) string {
	if prefix == "" {
		return path.Join("backups", "db", "latest.json")
	}
	return path.Join(prefix, "latest.json")
}

func computeChecksumSHA256(payload []byte) string {
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func loadBackupEncryptionKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("BACKUP_ENCRYPTION_KEY"))
	if raw == "" {
		return nil, os.ErrNotExist
	}

	if len(raw) == 32 {
		return []byte(raw), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, errors.New("BACKUP_ENCRYPTION_KEY must be 32-byte raw string or base64-encoded 32 bytes")
}

func encryptPayload(key []byte, payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nonce, nonce, payload, nil)
	return sealed, nil
}

func decryptPayload(key []byte, payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(payload) < gcm.NonceSize() {
		return nil, errors.New("encrypted payload is too short")
	}
	nonce, ciphertext := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

func runPgDump(ctx context.Context, dbCfg dbConnConfig, outputPath string) error {
	pgDumpBin := strings.TrimSpace(os.Getenv("BACKUP_PG_DUMP_BIN"))
	if pgDumpBin == "" {
		pgDumpBin = "pg_dump"
	}
	resolvedBin, err := resolveCommandBinary(pgDumpBin)
	if err != nil {
		return fmt.Errorf("pg_dump binary unavailable: %w", err)
	}

	args := []string{
		"-h", dbCfg.Host,
		"-p", dbCfg.Port,
		"-U", dbCfg.User,
		"-d", dbCfg.Name,
		"-Fc",
		"-f", outputPath,
	}

	cmd := exec.CommandContext(ctx, resolvedBin, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+dbCfg.Password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_dump failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runPgRestore(ctx context.Context, dbCfg dbConnConfig, dumpPath string) error {
	pgRestoreBin := strings.TrimSpace(os.Getenv("BACKUP_PG_RESTORE_BIN"))
	if pgRestoreBin == "" {
		pgRestoreBin = "pg_restore"
	}
	resolvedBin, err := resolveCommandBinary(pgRestoreBin)
	if err != nil {
		return fmt.Errorf("pg_restore binary unavailable: %w", err)
	}

	args := []string{
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"-h", dbCfg.Host,
		"-p", dbCfg.Port,
		"-U", dbCfg.User,
		"-d", dbCfg.Name,
		dumpPath,
	}

	cmd := exec.CommandContext(ctx, resolvedBin, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+dbCfg.Password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		rawOutput := strings.TrimSpace(string(output))
		if strings.Contains(rawOutput, `unrecognized configuration parameter "transaction_timeout"`) {
			// pg_restore exits with status 1 when it encounters unrecognised SET parameters
			// (e.g. SET transaction_timeout introduced in pg17 client dumps).
			// The tool logs these as "errors ignored on restore: N", meaning the actual
			// data/schema restore still completed.  We scan the output for any *real* errors
			// that go beyond this known compatibility issue and only fail if we find some.
			if onlyTransactionTimeoutErrors(rawOutput) {
				log.Printf("⚠️  pg_restore: ignoring transaction_timeout compatibility warning — restore data applied successfully")
				return nil
			}
		}
		return fmt.Errorf("pg_restore failed: %v (%s)", err, rawOutput)
	}
	return nil
}

// onlyTransactionTimeoutErrors returns true when every error line in pg_restore
// output is solely about the unrecognised "transaction_timeout" parameter.
// Any other ERROR / FATAL line means the restore had a real problem.
func onlyTransactionTimeoutErrors(output string) bool {
	hasTransactionTimeoutError := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		isError := strings.Contains(lower, "error") || strings.Contains(lower, "fatal")
		if !isError {
			continue
		}
		if strings.Contains(trimmed, "transaction_timeout") ||
			strings.Contains(trimmed, "errors ignored on restore") {
			hasTransactionTimeoutError = true
			continue
		}
		// A real, unrelated error was found — restore cannot be trusted.
		return false
	}
	return hasTransactionTimeoutError
}

func resolveCommandBinary(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return "", errors.New("empty command")
	}

	if _, err := exec.LookPath(configured); err == nil {
		return configured, nil
	}

	base := filepath.Base(configured)
	if base != configured && base != "." && base != string(filepath.Separator) {
		if _, err := exec.LookPath(base); err == nil {
			log.Printf("⚠️  Backup command %q not found, falling back to %q from PATH", configured, base)
			return base, nil
		}
	}

	return "", fmt.Errorf("%q not found in PATH", configured)
}

func envInt(name string, defaultValue int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func loadBackupStatus() (BackupOperationStatus, error) {
	raw, err := repositories.GetAppConfigValue(backupStatusConfigKey)
	if err != nil {
		return BackupOperationStatus{}, nil
	}

	var status BackupOperationStatus
	if unmarshalErr := json.Unmarshal([]byte(raw), &status); unmarshalErr != nil {
		return BackupOperationStatus{}, unmarshalErr
	}
	return status, nil
}

func setBackupStatus(next BackupOperationStatus) error {
	current, _ := loadBackupStatus()

	if next.LastTrigger == "" {
		next.LastTrigger = current.LastTrigger
	}
	if next.LastStatus == "" {
		next.LastStatus = current.LastStatus
	}
	if next.LastError == "" && next.LastStatus != "success" {
		next.LastError = current.LastError
	}
	if next.LastBackupID == nil {
		next.LastBackupID = current.LastBackupID
	}
	if next.LastBackupAt == nil {
		next.LastBackupAt = current.LastBackupAt
	}
	if next.LastRestoreAt == nil {
		next.LastRestoreAt = current.LastRestoreAt
	}
	if next.UpdatedAt.IsZero() {
		next.UpdatedAt = time.Now()
	}

	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return repositories.SetAppConfigValue(backupStatusConfigKey, string(raw))
}

// acquireDailyBackupLock takes a cross-instance lock for the daily backup.
//
// Returns true when this process may proceed. If Redis is unavailable it also
// returns true: losing the automated backup entirely is a worse outcome than
// the duplicate it is guarding against, and a duplicate backup is wasteful
// rather than destructive.
func acquireDailyBackupLock() bool {
	if config.Redis == nil {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acquired, err := config.Redis.SetNX(ctx, dailyBackupLockKey, "1", dailyBackupLockTTL).Result()
	if err != nil {
		log.Printf("⚠️  Could not take the daily backup lock, proceeding anyway: %v", err)
		return true
	}

	return acquired
}

func releaseDailyBackupLock() {
	if config.Redis == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := config.Redis.Del(ctx, dailyBackupLockKey).Err(); err != nil {
		// Not fatal: the TTL releases it regardless, just later than ideal.
		log.Printf("⚠️  Could not release the daily backup lock: %v", err)
	}
}
