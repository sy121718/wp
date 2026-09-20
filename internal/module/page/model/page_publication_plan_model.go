package pagemodel

// page_publication_plan_model.go — 页面「发布计划」持久化（审计 I18N-01，迁移 308）。
//
// 计划 = 这次发布依据的站点语言输入（语言表 + 默认语言），发布时冻结一次。
// 为什么必须落库而不是只写进产物 Manifest：Manifest 跟着**产物**走，而重建入口
// （组件升级后的批量重建、按草稿重新构建再发布）手里只有页面与语言，
// 产物行可能已经不可用（文件丢失正是重建的触发场景）。计划跟着**页面+语言**走，
// 重建时先读到它，再用它去编译 —— 冻结才成立。
//
// 键与 page_stagings / page_publications 同形（page_id, lang）：三个台账各自回答
// 一个问题 —— 暂存了什么、激活了什么、依据哪份语言输入。分开而不合并的理由是
// 生命周期不同：暂存每次构建重写、激活每次发布重写、计划只在「新的发布决策」上重写。
//
// 无 project_id：与 page_stagings / page_publications 一致，不在 RLS 清单里
// （这两张表也没有工程列，口径必须一致，否则同一张表读写在两种作用域下行为不同）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/internal/pipeline"
	"go_wp/pkg/rls"
)

const tableNamePagePublicationPlans = "page_publication_plans"

// PublicationPlanEntity 对应 page_publication_plans 表。
type PublicationPlanEntity struct {
	PageID string `gorm:"column:page_id;primaryKey"`
	Lang   string `gorm:"column:lang;primaryKey"`
	// Plan 冻结的发布计划原文（jsonb）：{siteLangs, defaultLang}。
	//
	// 存整份 JSON 而不是拆两列：计划是会长的（目标语言、canonical origin、
	// 构建依赖版本都是后续批次要加的），拆列意味着每加一个输入都要再来一次迁移，
	// 而这里要回答的问题始终只有一个 ——「这次发布依据的输入是什么」。
	Plan json.RawMessage `gorm:"column:plan;type:jsonb;not null"`
	// PlanHash 规范化计划的内容指纹（与 pipeline.PublicationPlan.Hash 同源）。
	// 单独存一列是为了让「计划换没换」可以在 SQL 里判定与核对，不必解 JSON。
	PlanHash string `gorm:"column:plan_hash;not null"`
	// DraftVersion 冻结时的草稿版本（审计 I18N-01 的重新冻结判据）。
	//
	// 判据是「作者动了草稿 = 新的发布决策」：草稿没变的重建（组件升级、依赖失效）
	// 必须沿用冻结计划，否则配置一改、批量重建就把既有产物的 hreflang 换掉了 ——
	// 那正是本条审计要消灭的失效。
	DraftVersion int64     `gorm:"column:draft_version;not null"`
	CreatedAt    time.Time `gorm:"column:create_time;not null"`
	UpdatedAt    time.Time `gorm:"column:update_time;not null"`
}

func (PublicationPlanEntity) TableName() string { return tableNamePagePublicationPlans }

// PublicationPlanDB 返回已绑定 page_publication_plans 表的 GORM 实例。
func (m *Model) PublicationPlanDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublicationPlanEntity{})
}

// GetPublicationPlan 按 (page_id, lang) 取冻结计划；不存在返回 gorm.ErrRecordNotFound。
func (m *Model) GetPublicationPlan(ctx context.Context, pageID, lang string) (e *PublicationPlanEntity, err error) {
	e = &PublicationPlanEntity{}
	if err = m.PublicationPlanDB(ctx).
		Where("page_id = ? AND lang = ?", pageID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// UpdatePublicationPlanRecordTx 在**调用方的事务**内冻结某语言的发布计划。
//
// 与「构建产物暂存指针」同一个事务：产物与它依据的语言输入必须同生共死 ——
// 只写一半会出现「暂存指针指向按 A 份语言输入构建的产物，而计划记的是 B 份」，
// 后续重建按 B 复现不出那份产物，且没有任何报错。
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

// DeletePublicationPlansByLangTx 在外部事务内删除某页面某语言的发布计划（幂等）。
//
// 只删一个语言（与 DeletePublicationsByLangTx 同一取舍）：整页删会让仍在服务的其它
// 语言失去冻结输入，它们的下一次重建会退回现场解析 —— 也就是这条审计要消灭的不确定性。
// 页面整体删除时的清理写在 softDeleteTx 里（与 page_publications / page_stagings 同处）。
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
