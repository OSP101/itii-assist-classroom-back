package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TwoFactorRotationResult struct {
	DryRun        bool `json:"dry_run"`
	Scanned       int  `json:"scanned"`
	NeedsRotation int  `json:"needs_rotation"`
	Rotated       int  `json:"rotated"`
}

// Dry runs use a read-only SQL transaction. Applying is an explicit atomic
// operation after old-key writers have stopped; one invalid row aborts it all.
func RotateTwoFactorEncryptionKey(ctx context.Context, dryRun bool) (TwoFactorRotationResult, error) {
	result := TwoFactorRotationResult{DryRun: dryRun}
	if err := utils.ValidateTwoFactorRotationKey(); err != nil {
		return result, err
	}
	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := func() *gorm.DB {
			q := tx.Session(&gorm.Session{NewDB: true})
			if !dryRun {
				q = q.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			return q
		}
		var users []models.User
		if err := query().Unscoped().Select("id", "two_factor_secret").Where("two_factor_method = ? AND two_factor_secret <> ?", "totp", "").Order("id").Find(&users).Error; err != nil {
			return err
		}
		var pending []models.TwoFactorPending
		if err := query().Select("id", "secret").Where("method = ? AND secret <> ?", "totp", "").Order("id").Find(&pending).Error; err != nil {
			return err
		}
		update := func(model any, id uint, column, old string) error {
			result.Scanned++
			replacement, changed, err := utils.RewrapTwoFactorSecret(old)
			if err != nil {
				return fmt.Errorf("cannot decrypt TOTP row %T/%d; transaction aborted", model, id)
			}
			if !changed {
				return nil
			}
			result.NeedsRotation++
			if dryRun {
				return nil
			}
			update := tx.Session(&gorm.Session{NewDB: true}).Unscoped().Model(model).Where("id = ? AND "+column+" = ?", id, old).Update(column, replacement)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return fmt.Errorf("TOTP row changed during rotation; transaction aborted")
			}
			result.Rotated++
			return nil
		}
		for _, user := range users {
			if err := update(&models.User{}, user.ID, "two_factor_secret", user.TwoFactorSecret); err != nil {
				return err
			}
		}
		for _, item := range pending {
			if err := update(&models.TwoFactorPending{}, item.ID, "secret", item.Secret); err != nil {
				return err
			}
		}
		return nil
	}, &sql.TxOptions{ReadOnly: dryRun})
	if err != nil {
		result.Rotated = 0
	}
	return result, err
}
