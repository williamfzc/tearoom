// Dev-mode administration. Until the GitHub OAuth exchange lands (Phase 2),
// a hosted server treats TEAROOM_HOSTED_ADMIN as the bearer token of one
// dev admin account when TEAROOM_HOSTED_DEV=1. The account is created on
// first use. Nothing here exists in production deployments.
package control

import (
	"context"
	"errors"
	"os"
	"time"

	"gorm.io/gorm"
)

const (
	envDev   = "TEAROOM_HOSTED_DEV"
	envAdmin = "TEAROOM_HOSTED_ADMIN"

	devLogin = "dev"
)

// DevAdmin returns the dev admin token when dev mode is on, else "".
func DevAdmin() string {
	if os.Getenv(envDev) != "1" {
		return ""
	}
	return os.Getenv(envAdmin)
}

// DevLogin returns the dev admin account, creating it on first use.
func DevLogin(ctx context.Context, db *gorm.DB) (*Account, error) {
	var account Account
	err := db.WithContext(ctx).Where("login = ?", devLogin).Limit(1).Find(&account).Error
	if err != nil {
		return nil, err
	}
	if account.ID != 0 {
		return &account, nil
	}
	account = Account{Login: devLogin, Name: "Dev Admin", CreatedAt: time.Now().UTC()}
	if err := db.WithContext(ctx).Create(&account).Error; err != nil {
		// Concurrent first logins race the unique index; re-read wins.
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return DevLogin(ctx, db)
		}
		return nil, err
	}
	return &account, nil
}
