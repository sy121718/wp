package i18n

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/database"
	"go_wp/pkg/rls"
)

func Revision() string {
	db, err := database.GetDB()
	if err != nil || db == nil {
		return ""
	}
	var rev struct {
		Revision int64 `gorm:"column:revision"`
	}
	if err = db.Table("sys_i18n_revision").Select("revision").Where("id = ?", 1).Scan(&rev).Error; err == nil && rev.Revision > 0 {
		return fmt.Sprintf("i18n-rev-%d", rev.Revision)
	}

	var latest sql.NullTime
	if err = db.Table("sys_i18n").Select("max(update_time) AS latest").Scan(&latest).Error; err != nil || !latest.Valid {
		return ""
	}
	return "i18n-max-" + latest.Time.UTC().Format(time.RFC3339Nano)
}

func ContentRevisionForProject(ctx context.Context, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ContentRevision()
	}
	db, err := database.GetDB()
	if err != nil || db == nil {
		return ""
	}
	var latest sql.NullTime

	if err = rls.InProjectScope(ctx, db, projectID, func(tx *gorm.DB) error {
		return tx.Table("sys_translation").Select("max(update_time) AS latest").Scan(&latest).Error
	}); err != nil || !latest.Valid {
		return ""
	}
	return "trans-max-" + latest.Time.UTC().Format(time.RFC3339Nano)
}

func ContentRevision() string {
	db, err := database.GetDB()
	if err != nil || db == nil {
		return ""
	}
	var latest sql.NullTime
	if err = db.Table("sys_translation").Select("max(update_time) AS latest").Scan(&latest).Error; err != nil || !latest.Valid {
		return ""
	}
	return "trans-max-" + latest.Time.UTC().Format(time.RFC3339Nano)
}
