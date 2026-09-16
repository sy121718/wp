// Package pagemodel 实现 page 模块 pages、page_revisions 与 page_routes 表持久化。
package pagemodel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

var (
	// ErrDraftVersionConflict 表示乐观锁更新未命中当前草稿版本。
	ErrDraftVersionConflict = errors.New("page 草稿版本冲突")
)

const (
	tableNamePages         = "pages"
	tableNamePageRevisions = "page_revisions"
)

// PageEntity 对应 pages 表的手工 Page 字段。
type PageEntity struct {
	ID        string `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string `gorm:"column:project_id;type:uuid;not null"`
	// ThemeID 工程当前激活主题的快照；激活主题时 ReattachProjectPagesToTheme 会全工程转挂，
	// 不支持页面级异主题 —— 勿当作「每页可选主题」维度。
	ThemeID           *string         `gorm:"column:theme_id;type:uuid"`
	Kind              string          `gorm:"column:kind;type:text;not null"`
	ContentTargetType string          `gorm:"column:content_target_type;type:text;not null"`
	ContentTargetID   *string         `gorm:"column:content_target_id;type:uuid"`
	DraftPath         string          `gorm:"column:draft_path;type:text;not null"`
	ActivePath        *string         `gorm:"column:active_path;type:text"`
	DraftDocument     json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion      int64           `gorm:"column:draft_version;not null"`
	StagedArtifactID  *string         `gorm:"column:staged_artifact_id;type:uuid"`
	ActiveArtifactID  *string         `gorm:"column:active_artifact_id;type:uuid"`
	Stale             bool            `gorm:"column:stale;not null"`
	DeletedAt         *time.Time      `gorm:"column:deleted_at"`
	PublishedAt       *time.Time      `gorm:"column:published_at"`
	CreatedAt         time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt         time.Time       `gorm:"column:update_time;not null"`
}

func (PageEntity) TableName() string { return tableNamePages }

// RevisionEntity 对应 page_revisions 表：每次保存的不可变草稿快照。
type RevisionEntity struct {
	ID            string          `gorm:"column:id;type:uuid;primaryKey"`
	PageID        string          `gorm:"column:page_id;type:uuid;not null"`
	Version       int64           `gorm:"column:version;not null"`
	DraftPath     string          `gorm:"column:draft_path;type:text;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	SourceHash    string          `gorm:"column:source_hash;type:text;not null"`
	CreatedAt     time.Time       `gorm:"column:create_time;not null"`
}

func (RevisionEntity) TableName() string { return tableNamePageRevisions }

// Model 封装 page 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewPageModel 创建 Page Model。
func NewPageModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 pages 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageEntity{})
}

// RevisionDB 返回已绑定 page_revisions 表的 GORM 实例。
func (m *Model) RevisionDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RevisionEntity{})
}

// Transaction 在数据库事务中执行给定函数；草稿、修订与路径占用必须原子提交。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ListAll 列出工程内未删除页面（排除大字段 draft_document，供列表页使用）。
// projectID 必填；themeID 为空时列该工程全部；非空时列「挂在该主题下」与「尚未挂主题」的页面 ——
// 主题是页面的归属（020_themes.sql：主题下面才是页面），但没归属的历史页面
// 不能因为按主题过滤而不可见（建站已自带默认主题，NULL 分支是它们的唯一可见路径）。
func (m *Model) ListAll(ctx context.Context, projectID, themeID string) (list []PageEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&PageEntity{}).Omit("draft_document").Where("deleted_at IS NULL AND project_id = ?", projectID)
		if themeID != "" {
			// 未挂主题的页面一并列出：列表按「激活主题」浏览，但主题创建前建的页面
			// （或绑定丢失的页面）不能因此从列表里消失 —— 那会变成「建了却找不到」。
			// 建站已有默认主题后，这条 NULL 分支是历史数据唯一的可见路径。
			q = q.Where("theme_id = ? OR theme_id IS NULL", themeID)
		}
		return q.Order("update_time DESC, id DESC").Find(&list).Error
	})
	return list, err
}

// ListDraftDocuments 列出全部未删除页面的草稿文档（多语言 P5c 翻译工作台的全站扫描用）。
//
// 与 ListAll 的区别：带 draft_document 大字段（工作台要按组件白名单收集候选，
// 无法在 SQL 侧完成——白名单在 Go 里）；按 update_time 倒序，便于诊断。
// 代价：一次查询返回全站草稿 JSONB，调用方必须自带缓存与页数上限（见 dashboard 工作台）。
func (m *Model) ListDraftDocuments(ctx context.Context) (list []PageEntity, err error) {
	err = m.DB(ctx).
		Select("id", "project_id", "draft_path", "draft_document", "update_time").
		Where("deleted_at IS NULL").
		Order("update_time DESC, id DESC").
		Find(&list).Error
	return list, err
}

// ThemePageSnapshot 主题刷新时逐页合成快照所需的「页面 ID + 页面级主题覆盖」。
type ThemePageSnapshot struct {
	ID       string
	Override json.RawMessage
}

// ListThemePageSnapshots 取该主题下全部未删除页面的 ID 与 settings.themeOverride。
//
// 为什么刷新快照不能再一条 SQL 批量写：快照 = 站点主题 + 页面覆盖（每页覆盖不同），
// 而 PostgreSQL 的 jsonb || 是浅合并（嵌套对象整块替换），做不了键级深合并 ——
// 一条 SQL 写下去会把页面的覆盖项连同它没覆盖的项一起冲掉。
func (m *Model) ListThemePageSnapshots(ctx context.Context, themeID string) (rows []ThemePageSnapshot, err error) {
	type row struct {
		ID         string
		ThemeOverr json.RawMessage `gorm:"column:theme_override"`
	}
	var raw []row
	if err = m.DB(ctx).
		Select("id", "draft_document #> '{settings,themeOverride}' AS theme_override").
		Where("theme_id = ? AND deleted_at IS NULL", themeID).
		Find(&raw).Error; err != nil {
		return nil, err
	}
	for _, r := range raw {
		rows = append(rows, ThemePageSnapshot{ID: r.ID, Override: r.ThemeOverr})
	}
	return rows, nil
}

// UpdateThemeSnapshot 写单页的 settings.theme 快照（不动内容与版本，主题是展示层）。
func (m *Model) UpdateThemeSnapshot(ctx context.Context, pageID string, themeJSON []byte) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,theme}', ?, true), update_time = ? WHERE id = ? AND deleted_at IS NULL",
		themeJSON, time.Now().UTC(), pageID,
	).Error
	return err
}

// ThemePageStructureSnapshot 主题刷新 structure 时逐页合成所需的页面级绑定。
type ThemePageStructureSnapshot struct {
	ID        string
	Structure json.RawMessage
}

// ListThemePageStructureSnapshots 取该主题下全部页面的 settings.structure。
func (m *Model) ListThemePageStructureSnapshots(ctx context.Context, themeID string) (rows []ThemePageStructureSnapshot, err error) {
	type row struct {
		ID        string
		Structure json.RawMessage `gorm:"column:page_structure"`
	}
	var raw []row
	if err = m.DB(ctx).
		Select("id", "draft_document #> '{settings,structure}' AS page_structure").
		Where("theme_id = ? AND deleted_at IS NULL", themeID).
		Find(&raw).Error; err != nil {
		return nil, err
	}
	for _, r := range raw {
		rows = append(rows, ThemePageStructureSnapshot{ID: r.ID, Structure: r.Structure})
	}
	return rows, nil
}

// UpdateStructureSnapshot 写单页 settings.structure（不动内容与版本）。
func (m *Model) UpdateStructureSnapshot(ctx context.Context, pageID string, structureJSON []byte) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,structure}', ?, true), update_time = ? WHERE id = ? AND deleted_at IS NULL",
		structureJSON, time.Now().UTC(), pageID,
	).Error
	return err
}

// MarkStaleForTheme 把挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用）。
func (m *Model) MarkStaleForTheme(ctx context.Context, themeID string) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET stale = true, update_time = ? WHERE theme_id = ? AND deleted_at IS NULL",
		time.Now().UTC(), themeID,
	).Error
	return err
}

// MarkStaleForI18n 把全部未删除页面标记为待重建（界面文案词条变更后调用）。
//
// 文案词条（sys_i18n）参与构建：组件固定文案由构建期取词注入 HTML 字节
// （docs/06-D §10）。词条改动后所有页面产物都可能过期，故整站标记 stale；
// 触发源为后台 i18n CRUD（决策 D7，尚未实现）或运维脚本，内核只提供能力。
// 全表更新，与 MarkStaleForTheme 同一模式（stale=true 幂等）。
func (m *Model) MarkStaleForI18n(ctx context.Context) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET stale = true, update_time = ? WHERE deleted_at IS NULL",
		time.Now().UTC(),
	).Error
	return err
}

// MarkStaleByIDs 按页面 ID 列表标记待重建，返回实际被标记的 ID。
//
// 与 MarkStaleForI18n 的全表更新区分：调用方已经算出了精确的影响集合
// （如「产物由旧组件产出」的页面），不做无谓的全站标记。
func (m *Model) MarkStaleByIDs(ctx context.Context, ids []string, at time.Time) (marked []string, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).
		Where("deleted_at IS NULL AND id IN ?", ids).
		Update("stale", true).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// blockRefMatchCond 块引用匹配条件（JSONB 路径查询）。
//
// 语义：draft_document 中任意深度出现键 blockId 且值为字符串 <blockID> 的节点
// （core.globalref 的 props.blockId 是唯一来源，但该节点可嵌在 root 树任意
// 容器层级，故用递归通配 $.** 收集全部 blockId 值，再判数组包含）。
// 与旧写法 draft_document::text LIKE '%"blockId": "<blockID>"%' 结果集完全一致：
// 旧写法把 JSONB 序列化成 text 后子串匹配，而字符串值内部的引号在 JSONB 文本
// 输出中已转义为 \"，两者都不会误命中字符串字面量。
//
// 为什么不是 @? '$.**.blockId ? (@ == "x")'：jsonb_path_ops 无法索引递归
// 通配，EXPLAIN 下退化为索引内全扫（2 万行实测：索引扫描 18182 行后丢弃，
// 比 seq scan 更慢）。改成「值集合 + 数组包含」后表达式可被 GIN 精确索引。
//
// 本表达式与迁移 068 的 idx_pages_blockref 表达式一致（PG 按解析后的表达式树
// 比较，空白无关）；同一查询的 structure 分支（headerBlockId / footerBlockId）
// 另有 idx_pages_structure_header / _footer 两个 btree 表达式索引，三个 OR 分支
// 由 planner 用 BitmapOr 合并（2 万行实测 0.27ms，旧写法全表扫 55ms）。
// 改动本表达式必须同步迁移 068，否则索引静默失效
// （public/test/page/unit 有等价性与 EXPLAIN 断言守住）。
const blockRefMatchCond = `jsonb_path_query_array(draft_document, '$.**.blockId') @> jsonb_build_array(?::text)`

// CountBlockReference 统计引用该块的未删除页面数（与 MarkStaleForBlock 同一匹配条件）：
// core.globalref 节点（blockId）或 settings.structure 页眉/页脚自选绑定。
// 供 block 模块删除/切换 global→template 前的引用拦截（docs/02-D §9）。
func (m *Model) CountBlockReference(ctx context.Context, blockID string) (count int64, err error) {
	err = m.DB(ctx).
		Where("deleted_at IS NULL AND ("+
			blockRefMatchCond+
			" OR draft_document->'settings'->'structure'->>'headerBlockId' = ?"+
			" OR draft_document->'settings'->'structure'->>'footerBlockId' = ?)",
			blockID, blockID, blockID,
		).Count(&count).Error
	return count, err
}

// MarkStaleForBlock 把文档中经 core.globalref 引用（draft_document 树内
// "blockId": "<blockID>" 节点）或 settings.structure 页眉/页脚自选绑定
// （headerBlockId/footerBlockId，页面级覆盖，非主题默认）该块的页面标记为待重建。
// 与 MarkStaleForTheme 可能重叠命中同一页面，stale=true 幂等，无妨。
func (m *Model) MarkStaleForBlock(ctx context.Context, blockID string) (err error) {
	err = m.DB(ctx).Exec(
		"UPDATE pages SET stale = true, update_time = ? WHERE deleted_at IS NULL AND ("+
			blockRefMatchCond+
			" OR draft_document->'settings'->'structure'->>'headerBlockId' = ?"+
			" OR draft_document->'settings'->'structure'->>'footerBlockId' = ?)",
		time.Now().UTC(), blockID, blockID, blockID,
	).Error
	return err
}

// AttachThemeToUnassigned 把工程内尚未挂主题的页面挂到指定主题。
// 工程首个主题创建时回填历史页面（迁移 020 的运行时兜底）。
func (m *Model) AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) (err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).Exec(
			"UPDATE pages SET theme_id = ?, update_time = ? WHERE project_id = ? AND theme_id IS NULL AND deleted_at IS NULL",
			themeID, time.Now().UTC(), projectID,
		).Error
	})
	return err
}

// ReattachProjectPagesToTheme 把工程内全部页面（含已挂其他主题的）转挂到指定主题。
// 切换激活主题时调用，是「整站换皮」的前置：只有转挂后批量刷新（Refresh*/MarkStale*）
// 才能以该主题为键命中整站页面。不改 draft_document 内容，也不 bump 版本。
func (m *Model) ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) (err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).Exec(
			"UPDATE pages SET theme_id = ?, update_time = ? WHERE project_id = ? AND deleted_at IS NULL",
			themeID, time.Now().UTC(), projectID,
		).Error
	})
	return err
}

// GetByID 按 ID 查询未删除的 Page。projectID 非空时追加工程归属条件（防跨工程 IDOR）。
func (m *Model) GetByID(ctx context.Context, id, projectID string) (e *PageEntity, err error) {
	e = &PageEntity{}
	// projectID 为空是「不限工程」的历史调用形态：不设 scope 时策略谓词为 NULL，
	// 换非超级角色后该路径 fail closed（0 行 → ErrRecordNotFound）而不会读到别的工程；
	// 要让它可用必须由调用方补 projectID（列入 DB-009 剩余清单）。
	if strings.TrimSpace(projectID) == "" {
		if err = m.DB(ctx).Where("id = ? AND deleted_at IS NULL", id).First(e).Error; err != nil {
			return nil, err
		}
		return e, nil
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		return nil, err
	}
	return e, nil
}

// ListRevisions 按版本倒序读取修订快照。
func (m *Model) ListRevisions(ctx context.Context, pageID string) (list []RevisionEntity, err error) {
	err = m.RevisionDB(ctx).Where("page_id = ?", pageID).Order("version DESC").Find(&list).Error
	return list, err
}

// PruneRevisions 把单页的历史快照收敛到「最近 keep 个」，返回删除行数。
//
// 保存草稿后顺手调用（IDX-005）：改一次存一份完整 draft_document，高频编辑的页面
// 会把表撑起来，而保留条数之外的历史版本本来就是给回退用的、不需要无限留着。
// 只按条数收敛、不看时间：编辑者刚存的那几个版本必须都在。
func (m *Model) PruneRevisions(ctx context.Context, pageID string, keep int) (int64, error) {
	if strings.TrimSpace(pageID) == "" || keep < 1 {
		return 0, nil
	}
	var threshold int
	err := m.RevisionDB(ctx).Where("page_id = ?", pageID).
		Order("version DESC").Offset(keep-1).Limit(1).
		Pluck("version", &threshold).Error
	if err != nil || threshold <= 1 {
		// 没有第 keep 个版本（说明总数还不够）→ 无可收敛。
		return 0, err
	}
	res := m.RevisionDB(ctx).Where("page_id = ? AND version < ?", pageID, threshold).Delete(&RevisionEntity{})
	_ = threshold
	return res.RowsAffected, res.Error
}

// DeleteStaleRevisions 全库分批清理「超出保留条数**且**早于保留期」的历史快照。
//
// 两个条件同时满足才删，是刻意的保守取舍：
//   - 只看条数：刚发布后密集保存的版本会被立刻删掉，而这正是编辑者要回退的东西；
//   - 只看时间：长期不编辑的页面反而留不住上限（老版本永远删不掉）。
//
// 已发布版本不在这里单独排除：page_revisions 没有「哪个版本已发布」的标记（发布状态在
// publication / artifacts 侧），因此以保留期兜底 —— 保留期内的版本一律不动。
func (m *Model) DeleteStaleRevisions(ctx context.Context, keep int, cutoff time.Time, limit int) (int64, error) {
	if keep < 1 || limit < 1 {
		return 0, nil
	}
	// ctid 定位：PostgreSQL 的 DELETE 不支持 LIMIT，用子查询挑出本批目标。
	const q = `DELETE FROM page_revisions WHERE ctid IN (
		SELECT ctid FROM (
			SELECT ctid, row_number() OVER (PARTITION BY page_id ORDER BY version DESC) AS rn, create_time
			FROM page_revisions
		) t WHERE t.rn > ? AND t.create_time < ? LIMIT ?
	)`
	res := m.DB(ctx).Exec(q, keep, cutoff, limit)
	return res.RowsAffected, res.Error
}

// CreateWithRevision 原子创建 Page 与初始 Revision。
// 路径占用（page_routes 的 reserved 行）由 service 层经 publication contract
// 的 ReservePath 处理——page_routes 单一所有归 publication，page model 不碰该表。
func (m *Model) CreateWithRevision(ctx context.Context, page *PageEntity, revision *RevisionEntity) (err error) {
	// RLS（迁移 215）：pages 已启用 FORCE 策略，写入承 page.ProjectID 的工程作用域。
	return rls.InProjectScope(ctx, m.db, page.ProjectID, func(tx *gorm.DB) error {
		if err := tx.Create(page).Error; err != nil {
			return err
		}
		return tx.Create(revision).Error
	})
}

// SaveDraftWithRevision 使用乐观锁原子保存草稿与修订。
// 改路径时的 reserved 占用迁移由 service 层经 publication contract 的
// RenameReserved 处理——page model 不再碰 page_routes。
func (m *Model) SaveDraftWithRevision(
	ctx context.Context,
	pageID string,
	expectedVersion int64,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
) (err error) {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		result := tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, expectedVersion).
			Updates(map[string]any{
				"draft_path":     path,
				"draft_document": document,
				"draft_version":  nextVersion,
				"stale":          true,
				"update_time":    updatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		return tx.Create(revision).Error
	})
}

// MarkPublished 回写活跃产物指针与发布元数据（发布/回滚共用）。
func (m *Model) MarkPublished(ctx context.Context, pageID, path, artifactID string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{
			"active_artifact_id": artifactID,
			"active_path":        path,
			"published_at":       at,
			"stale":              false,
			"update_time":        at,
		}).Error
}

// MoveDraftPath 发布改 URL 后同步草稿路径（逻辑路径，不含语言前缀）。
// 激活路径不再在此处写：它按语言存放在 page_publications，
// 由 MovePublicationPath 单独同步（多语言 P3，docs/06-D §15.5 第 2 条）。
func (m *Model) MoveDraftPath(ctx context.Context, pageID, newPath string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ? AND deleted_at IS NULL", pageID).
		Updates(map[string]any{"draft_path": newPath, "update_time": at}).Error
}

// SoftDelete 软删 Page（deleted_at 置时间，审计留痕）；页面不存在或已软删
// 返回 gorm.ErrRecordNotFound。路径占用清理由 service 层经 publication contract
// 的 DeleteRoutesByPage 处理——page model 不再碰 page_routes。
// 同时清理 page_publications（同聚合原子组合）：软删后残留的激活记录
// 会让「同路径新建页面」读到幽灵激活状态。
// projectID 非空时在工程作用域内软删（DB-009 第二批）：pages 带 FORCE 策略，
// 越界写会被 WITH CHECK 拒绝；同时显式带 project_id 条件，形成应用层与数据库层的双保险。
func (m *Model) SoftDelete(ctx context.Context, projectID, pageID string, at time.Time) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return m.softDeleteTx(ctx, nil, "", pageID, at)
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		return m.softDeleteTx(ctx, tx, projectID, pageID, at)
	})
}

// softDeleteTx 软删主体（tx 非空时在其上执行，并先设工程作用域）。
func (m *Model) softDeleteTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, at time.Time) (err error) {
	scope := func(tx *gorm.DB) error {
		result := tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL", pageID).
			Update("deleted_at", at)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if derr := tx.Model(&PublicationEntity{}).Where("page_id = ?", pageID).
			Delete(&PublicationEntity{}).Error; derr != nil {
			return derr
		}
		return tx.Model(&StagingEntity{}).Where("page_id = ?", pageID).
			Delete(&StagingEntity{}).Error
	}
	if tx == nil {
		// 无工程作用域的历史形态：不设 scope 时换非超级角色会自动 fail closed
		// （0 行 → ErrRecordNotFound），不会删到别的工程，方向是安全的。
		return m.Transaction(ctx, scope)
	}
	if projectID != "" {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
	}
	return scope(tx)
}

// DraftPathValue 返回草稿访问路径（空安全）。
func (e *PageEntity) DraftPathValue() string {
	if e == nil {
		return ""
	}
	return e.DraftPath
}

// ActivePathValue 返回当前线上路径（未发布为空）。
func (e *PageEntity) ActivePathValue() string {
	if e == nil || e.ActivePath == nil {
		return ""
	}
	return *e.ActivePath
}

// DraftDocumentFor 优先返回产物冻结源文档，回退到当前草稿。
func (e *PageEntity) DraftDocumentFor(source json.RawMessage) json.RawMessage {
	if len(source) > 0 {
		return source
	}
	return e.DraftDocument
}
