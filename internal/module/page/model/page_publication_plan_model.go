package pagemodel

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/internal/pipeline"
	"go_wp/pkg/rls"
)

const tableNamePagePublicationPlans = "page_publication_plans"

type PublicationPlanEntity struct {
	PageID string `gorm:"column:page_id;primaryKey"`
	Lang   string `gorm:"column:lang;primaryKey"`

	Plan json.RawMessage `gorm:"column:plan;type:jsonb;not null"`

	PlanHash string `gorm:"column:plan_hash;not null"`

	DraftVersion int64     `gorm:"column:draft_version;not null"`
	CreatedAt    time.Time `gorm:"column:create_time;not null"`
	UpdatedAt    time.Time `gorm:"column:update_time;not null"`
}

func (PublicationPlanEntity) TableName() string { return tableNamePagePublicationPlans }

func (m *Model) PublicationPlanDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublicationPlanEntity{})
}

func (m *Model) GetPublicationPlan(ctx context.Context, pageID, lang string) (e *PublicationPlanEntity, err error) {
	e = &PublicationPlanEntity{}
	if err = m.PublicationPlanDB(ctx).
		Where("page_id = ? AND lang = ?", pageID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) UpdatePublicationPlanRecordTx(ctx context.Context, tx *gorm.DB, pageID, lang string, plan pipeline.PublicationPlan, draftVersion int64, at time.Time) (err error) {
	if strings.TrimSpace(pageID) == "" || strings.TrimSpace(lang) == "" {
		return errors.New("发布计划缺少 page_id/lang")
	}
	n := plan.Normalize()
	if n.Empty() {
		return errors.New("发布计划为空（语言表与默认语言都缺失）")
	}
	raw, merr := json.Marshal(n)
	if merr != nil {
		return merr
	}
	return m.updatePublicationPlanRecordTx(ctx, tx, pageID, lang, raw, n.Hash(), draftVersion, at)
}

func (m *Model) updatePublicationPlanRecordTx(ctx context.Context, tx *gorm.DB, pageID, lang string, planJSON []byte, planHash string, draftVersion int64, at time.Time) error {
	row := &PublicationPlanEntity{
		PageID: pageID, Lang: lang, Plan: planJSON, PlanHash: planHash,
		DraftVersion: draftVersion, CreatedAt: at, UpdatedAt: at,
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "page_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"plan", "plan_hash", "draft_version", "update_time",
		}),
	}).Create(row).Error
}

func (m *Model) DeletePublicationPlansByLangTx(ctx context.Context, tx *gorm.DB, projectID, pageID, lang string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	if err = rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PublicationPlanEntity{}).
		Where("page_id = ? AND lang = ?", pageID, lang).Delete(&PublicationPlanEntity{}).Error
}
