package presentationmodel

// presentation_publication_model.go — 自动发布实例「每语言激活状态」（I18N-013）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const tableNamePresentationPublications = "presentation_publications"

// PublicationEntity 对应 presentation_publications 表。
type PublicationEntity struct {
	PresentationID string  `gorm:"column:presentation_id;primaryKey"`
	Lang           string  `gorm:"column:lang;primaryKey"`
	ActivePath     string  `gorm:"column:active_path;not null"`
	ArtifactID     *string `gorm:"column:artifact_id"`
	ArtifactHash   string  `gorm:"column:artifact_hash;not null"`
	// ReceiptID 写入这一行语言账本的发布回执 id（publication_receipts.id，迁移 700 加的列）。
	//
	// 它回答的是「**哪一次 attempt** 让这行账本成立」，而不是「线上是哪份字节」——
	// 后者 artifact_id 已经回答了。两件事必须分开：内容寻址 + 同版本重建会命中同一条
	// 产物行（GetArtifactByHashTx 复用），于是两个不同的 attempt 可以拿到同一个
	// artifact_id；只比 artifact_id 的恢复判定分不出「镜像里这份账本是我写的」还是
	// 「是另一个 attempt 写的」，而这两者的处置完全相反（补齐 vs superseded）。
	//
	// NULL = 本列上线前的历史行（当时没有这个事实），读侧一律按「未知」处理，
	// 退回 artifact_id 比较 —— 不是「没有 attempt」。
	ReceiptID   *string   `gorm:"column:receipt_id"`
	PublishedAt time.Time `gorm:"column:published_at;not null"`
	UpdatedAt   time.Time `gorm:"column:update_time;not null"`
}

func (PublicationEntity) TableName() string { return tableNamePresentationPublications }

// PublicationRecord 单语言激活状态写入参数。
type PublicationRecord struct {
	PresentationID string
	Lang           string
	ActivePath     string
	ArtifactID     string
	ArtifactHash   string
	// ReceiptID 提交事务里让本轮发布成立的回执 id（空串 = 调用方没有回执，
	// 例如单元测试或未装配回执设施的进程；写 NULL，不写空串）。
	//
	// 必须与激活同一次事务写入（见 service 侧 commitPublish）：先写账本再记回执、
	// 或反过来，中间崩溃都会留下「账本在、回执在、(receipt, artifact) 配对不在」的行。
	ReceiptID   string
	PublishedAt time.Time
}

// PublicationDB 绑定 presentation_publications 表。
func (m *Model) PublicationDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PublicationEntity{})
}

// ListPublications 列出实例全部语言的激活状态。
func (m *Model) ListPublications(ctx context.Context, presentationID string) (list []PublicationEntity, err error) {
	err = m.PublicationDB(ctx).Where("presentation_id = ?", presentationID).Order("lang ASC").Find(&list).Error
	return list, err
}

// GetPublication 按 (presentation_id, lang) 查询。
func (m *Model) GetPublication(ctx context.Context, presentationID, lang string) (e *PublicationEntity, err error) {
	e = &PublicationEntity{}
	if err = m.PublicationDB(ctx).Where("presentation_id = ? AND lang = ?", presentationID, lang).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// PublishedArtifactIDTx 事务内取某语言当前登记的产物行 id（无行 / 无产物返回空串）。
//
// 回执恢复的 G4 判定要它：账本里的产物 ≠ 回执的产物 = 本次 attempt 已被别的成功 attempt
// 取代。**不能**用 GetPublicationTx 那种「找不到就返回错误」的形态 —— 在这里「没有行」
// 是一个正常答案（切换成功、账本还没写，正是要补齐的形态），当成错误会让恢复把
// 待补齐全判成取消失败。
func (m *Model) PublishedArtifactIDTx(tx *gorm.DB, presentationID, lang string) (string, error) {
	var rows []struct{ ArtifactID *string }
	err := tx.Model(&PublicationEntity{}).
		Select("artifact_id").
		Where("presentation_id = ? AND lang = ?", presentationID, lang).
		Limit(1).Find(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ArtifactID == nil {
		return "", nil
	}
	return *rows[0].ArtifactID, nil
}

// PublishedReceiptIDTx 事务内取某语言账本行上的回执 id（无行 / 历史行返回空串）。
//
// 与 PublishedArtifactIDTx 同一个形状与同一条理由：「没有行」与「没有回执」都是正常
// 答案而不是错误（两者分别对应「切换成功但账本还没写」与「本列上线前的历史行」）。
// 恢复判定据此区分「账本是本次 attempt 写的」与「账本是另一个 attempt 写的」。
func (m *Model) PublishedReceiptIDTx(tx *gorm.DB, presentationID, lang string) (string, error) {
	var rows []struct{ ReceiptID *string }
	err := tx.Model(&PublicationEntity{}).
		Select("receipt_id").
		Where("presentation_id = ? AND lang = ?", presentationID, lang).
		Limit(1).Find(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ReceiptID == nil {
		return "", nil
	}
	return *rows[0].ReceiptID, nil
}

// PublishedReceiptID 非事务版本：本次发布前的账本成功身份（无行 / 历史行返回空串）。
//
// 只用于**登记前的事实捕获**（commitPublish 在写回执之前读它）。与 *Tx 版本同一查询、
// 同一语义，之所以不共用一处实现：本模块约定「…Tx 收调用方事务」，非事务读走
// context 连接，两者混在一处会让调用方分不清自己是不是已经站在事务里。
func (m *Model) PublishedReceiptID(ctx context.Context, presentationID, lang string) (string, error) {
	if strings.TrimSpace(presentationID) == "" || strings.TrimSpace(lang) == "" {
		return "", nil
	}
	var rows []struct{ ReceiptID *string }
	err := m.db.WithContext(ctx).Model(&PublicationEntity{}).
		Select("receipt_id").
		Where("presentation_id = ? AND lang = ?", presentationID, lang).
		Limit(1).Find(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ReceiptID == nil {
		return "", nil
	}
	return *rows[0].ReceiptID, nil
}

// MarkPublishedLang 写入某语言激活状态（upsert）。
func (m *Model) MarkPublishedLang(ctx context.Context, rec PublicationRecord) error {
	if rec.PresentationID == "" || rec.Lang == "" || rec.ActivePath == "" {
		return errors.New("激活记录缺少 presentation_id/lang/active_path")
	}
	row := &PublicationEntity{
		PresentationID: rec.PresentationID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	if rec.ReceiptID != "" {
		id := rec.ReceiptID
		row.ReceiptID = &id
	}
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "presentation_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "receipt_id", "published_at", "update_time",
		}),
	}).Create(row).Error
}

// MarkPublishedLangTx 事务内 upsert。
func (m *Model) MarkPublishedLangTx(tx *gorm.DB, rec PublicationRecord) error {
	if rec.PresentationID == "" || rec.Lang == "" || rec.ActivePath == "" {
		return errors.New("激活记录缺少 presentation_id/lang/active_path")
	}
	row := &PublicationEntity{
		PresentationID: rec.PresentationID, Lang: rec.Lang, ActivePath: rec.ActivePath,
		ArtifactHash: rec.ArtifactHash, PublishedAt: rec.PublishedAt, UpdatedAt: rec.PublishedAt,
	}
	if rec.ArtifactID != "" {
		id := rec.ArtifactID
		row.ArtifactID = &id
	}
	if rec.ReceiptID != "" {
		id := rec.ReceiptID
		row.ReceiptID = &id
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "presentation_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"active_path", "artifact_id", "artifact_hash", "receipt_id", "published_at", "update_time",
		}),
	}).Create(row).Error
}

// FindInstanceByActivePath 按某语言的已激活访问路径查占用实例（占用预检）。
func (m *Model) FindInstanceByActivePath(ctx context.Context, projectID, path, excludeInstanceID string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&InstanceEntity{}).
			Joins("JOIN "+tableNamePresentationPublications+" AS pp ON pp.presentation_id = presentation_instances.id").
			Where("presentation_instances.project_id = ? AND pp.active_path = ?", projectID, path)
		if excludeInstanceID != "" {
			q = q.Where("presentation_instances.id <> ?", excludeInstanceID)
		}
		return q.First(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListCurrentArtifactIDs 返回本模块语言账本里「当前」指向的产物行 ID（去重、输出稳定）。
//
// 真源是 presentation_publications.artifact_id —— 每个语言的激活产物指针，由
// publishOneLang 在「访问面已切换、路由已登记」之后写入（见 presentation_publish.go）。
// 供组件升级后的待重建识别使用（presentation 侧 MarkStaleByRegistryVersion）：判据必须是
// 「现在这份账本指向的产物」，而不是 presentation_artifacts 里所有 available 行 ——
// 后者含未 GC 的历史产物（回滚 / 反复重建留下的行），拿它比对会让已经重建过的实例
// 在每次重启时被重新标成 stale（与手工 Page 侧同一失效，报告 ARCH-03）。
//
// 刻意**不**并入 presentation_instances.active_artifact_id / staged_artifact_id：那两列是
// 「本批次默认语言产物」的镜像（finalizeMultiLangBatch 整批成功后才推进），并入只会把
// 批次未收敛时滞后的旧指针重新变成误标来源。
func (m *Model) ListCurrentArtifactIDs(ctx context.Context) (ids []string, err error) {
	ids = []string{}
	var rows []struct {
		ArtifactID *string `gorm:"column:artifact_id"`
	}
	err = m.db.WithContext(ctx).Model(&PublicationEntity{}).
		Select("artifact_id").
		Where("artifact_id IS NOT NULL").
		Distinct().
		Scan(&rows).Error
	if err != nil {
		return ids, err
	}
	for _, r := range rows {
		if r.ArtifactID != nil {
			ids = append(ids, *r.ArtifactID)
		}
	}
	return ids, err
}

// ListActivePathsForInstance 返回实例全部已登记语言的激活路径。
// ListPublicationsForInstance 非事务读取实例的全部语言账本行（语言升序）。
//
// 回滚用它取"我基于哪一代发起这次请求"（From）；锁内的同一查询走 ListPublicationsTx。
// 两处读法保持一致（同表同列同序），否则 From/Own 会因为读取口径不同而永远不等。
func (m *Model) ListPublicationsForInstance(ctx context.Context, presentationID string) (rows []PublicationEntity, err error) {
	if err = m.PublicationDB(ctx).Where("presentation_id = ?", presentationID).
		Order("lang ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListPublicationsTx 事务内列出实例的账本行（语言 + active_path + artifact + 成功回执身份）。
//
// 回滚要在**行锁内**证明"这次切换发生在同一次成功 attempt 之后"：URL 相等不足，
// 同一 URL 下的两次发布路径相同、身份不同。读法与非事务版同源，只换句柄。
func (m *Model) ListPublicationsTx(tx *gorm.DB, presentationID string) (rows []PublicationEntity, err error) {
	if tx == nil {
		return nil, errors.New("presentation: 账本读取必须在调用方事务内")
	}
	if err = tx.Model(&PublicationEntity{}).Where("presentation_id = ?", presentationID).
		Order("lang ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *Model) ListActivePathsForInstance(ctx context.Context, presentationID string) (paths []string, err error) {
	var rows []PublicationEntity
	if err = m.PublicationDB(ctx).Where("presentation_id = ?", presentationID).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ActivePath != "" {
			paths = append(paths, r.ActivePath)
		}
	}
	return paths, nil
}
