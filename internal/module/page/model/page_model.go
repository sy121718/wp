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

	// ErrProjectRequired 调用方没有给出工程作用域（DB-009 第三批）。
	//
	// pages 在迁移 215 里带 FORCE 策略，谓词读会话变量 app.project_id：不设变量的路径
	// 在换非超级角色后**静默返回 0 行**（fail closed 不报错）。所以本 model 中所有跨工程
	// 形态的入口（整站标记、按主题/块标记、全站草稿扫描）一律要求显式工程，由 service
	// 层枚举工程表后逐工程独立作用域调用 —— 「不限工程」这条默认路径在这里被彻底删掉。
	ErrProjectRequired = errors.New("page: 需要显式工程作用域")
)

const (
	tableNamePages         = "pages"
	tableNamePageRevisions = "page_revisions"
)

// ListAllProjectIDs 列出全部站点工程 id（跨工程扇出的兜底清单）。
//
// 为什么 page model 要读 projects 表（DB-009 第四批）：一批跨工程扇出入口（整站标记、
// 全站草稿扫描、按依赖源标记）的工程清单来自 project 契约 —— 契约未注入时（测试装配、
// 或将来某个装配点漏接）扇出会整体失败，表现为「译文改了页面不被标记」这类静默失效。
// projects 是隔离的**主体**：它没有 project_id 列、不在迁移 215 的 53 个对象里，
// 读它不涉及任何被隔离数据。order model 的 ListAllProjectIDs 是同一处境的同形兜底。
//
// 正确的修法是装配点注入 project 契约（生产装配已注入，见 routers 的 SetupPageRoutes；
// 漏的是两处测试装配 —— 落点见 DB-009 第四批报告）。
func (m *Model) ListAllProjectIDs(ctx context.Context) (ids []string, err error) {
	err = m.db.WithContext(ctx).
		Raw("SELECT id::text FROM projects ORDER BY create_time ASC, id ASC").Scan(&ids).Error
	return ids, err
}

// PageEntity 对应 pages 表的手工 Page 字段。
type PageEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	// ThemeID 工程当前激活主题的快照；激活主题时 ReattachProjectPagesToTheme 会全工程转挂，
	// 不支持页面级异主题 —— 勿当作「每页可选主题」维度。
	ThemeID           *string         `gorm:"column:theme_id"`
	Kind              string          `gorm:"column:kind;not null"`
	ContentTargetType string          `gorm:"column:content_target_type;not null"`
	ContentTargetID   *string         `gorm:"column:content_target_id"`
	DraftPath         string          `gorm:"column:draft_path;not null"`
	ActivePath        *string         `gorm:"column:active_path"`
	DraftDocument     json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion      int64           `gorm:"column:draft_version;not null"`
	StagedArtifactID  *string         `gorm:"column:staged_artifact_id"`
	ActiveArtifactID  *string         `gorm:"column:active_artifact_id"`
	Stale             bool            `gorm:"column:stale;not null"`
	DeletedAt         *time.Time      `gorm:"column:deleted_at"`
	PublishedAt       *time.Time      `gorm:"column:published_at"`
	CreatedAt         time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt         time.Time       `gorm:"column:update_time;not null"`
}

func (PageEntity) TableName() string { return tableNamePages }

// RevisionEntity 对应 page_revisions 表：每次保存的不可变草稿快照。
type RevisionEntity struct {
	ID            string          `gorm:"column:id;primaryKey"`
	PageID        string          `gorm:"column:page_id;not null"`
	Version       int64           `gorm:"column:version;not null"`
	DraftPath     string          `gorm:"column:draft_path;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	SourceHash    string          `gorm:"column:source_hash;not null"`
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

// ListDraftDocuments 列出**本工程**未删除页面的草稿文档（多语言 P5c 翻译工作台的全站扫描用）。
//
// 与 ListAll 的区别：带 draft_document 大字段（工作台要按组件白名单收集候选，
// 无法在 SQL 侧完成——白名单在 Go 里）；按 update_time 倒序，便于诊断。
// 代价：一次查询返回本工程全部草稿 JSONB，调用方必须自带缓存与页数上限（见 workbench 工作台）。
//
// projectID 必填（DB-009 第三批）：本方法原是「全站扫描」，而 pages 带 FORCE 策略——
// 「全站」在多工程部署下只能由 service 层逐工程调用拼出来（model 层不许出现「不限工程」，
// 那在换非超级角色后是静默 0 行：工作台会显示「全站 0 条草稿」而不报任何错）。
func (m *Model) ListDraftDocuments(ctx context.Context, projectID string) (list []PageEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("id", "project_id", "draft_path", "draft_document", "update_time").
			Where("project_id = ? AND deleted_at IS NULL", projectID).
			Order("update_time DESC, id DESC").
			Find(&list).Error
	})
	return list, err
}

// ThemePageSnapshot 主题刷新时逐页合成快照所需的「页面 ID + 页面级主题覆盖」。
type ThemePageSnapshot struct {
	ID       string
	Override json.RawMessage
}

// ListThemePageSnapshots 取**本工程内**该主题下全部未删除页面的 ID 与 settings.themeOverride。
//
// 为什么刷新快照不能再一条 SQL 批量写：快照 = 站点主题 + 页面覆盖（每页覆盖不同），
// 而 PostgreSQL 的 jsonb || 是浅合并（嵌套对象整块替换），做不了键级深合并 ——
// 一条 SQL 写下去会把页面的覆盖项连同它没覆盖的项一起冲掉。
//
// projectID 必填（DB-009 第三批）：pages 带 FORCE 策略，「挂在某主题下的页面」在多工程
// 部署下只能逐工程取（service 层枚举工程后逐个调用）。不设作用域的读取在换非超级角色后
// 是静默空集 —— 表现为「主题设置保存成功但所有页面快照一个都没更新」。
func (m *Model) ListThemePageSnapshots(ctx context.Context, projectID, themeID string) (rows []ThemePageSnapshot, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	type row struct {
		ID         string
		ThemeOverr json.RawMessage `gorm:"column:theme_override"`
	}
	var raw []row
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("id", "draft_document #> '{settings,themeOverride}' AS theme_override").
			Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
			Find(&raw).Error
	}); err != nil {
		return nil, err
	}
	for _, r := range raw {
		rows = append(rows, ThemePageSnapshot{ID: r.ID, Override: r.ThemeOverr})
	}
	return rows, nil
}

// UpdateThemeSnapshot 写单页的 settings.theme 快照（不动内容与版本，主题是展示层）。
//
// projectID 必填（DB-009 第三批）：这是一条裸 SQL 的 UPDATE，作用域只能由调用方给出。
// 漏了作用域时它在非超级角色下匹配 0 行且**不报错**（快照看似刷新成功、实际没写）。
func (m *Model) UpdateThemeSnapshot(ctx context.Context, projectID, pageID string, themeJSON []byte) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Exec(
			"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,theme}', ?, true), update_time = ? "+
				"WHERE id = ? AND project_id = ? AND deleted_at IS NULL",
			themeJSON, time.Now().UTC(), pageID, projectID,
		).Error
	})
}

// ThemePageStructureSnapshot 主题刷新 structure 时逐页合成所需的页面级绑定。
type ThemePageStructureSnapshot struct {
	ID        string
	Structure json.RawMessage
}

// ListThemePageStructureSnapshots 取**本工程内**该主题下全部页面的 settings.structure。
//
// projectID 必填（DB-009 第三批）：理由同 ListThemePageSnapshots —— 漏作用域时它在
// 非超级角色下静默返回空集，页眉/页脚绑定刷新会「成功但什么都没改」。
func (m *Model) ListThemePageStructureSnapshots(ctx context.Context, projectID, themeID string) (rows []ThemePageStructureSnapshot, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	type row struct {
		ID        string
		Structure json.RawMessage `gorm:"column:page_structure"`
	}
	var raw []row
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("id", "draft_document #> '{settings,structure}' AS page_structure").
			Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
			Find(&raw).Error
	}); err != nil {
		return nil, err
	}
	for _, r := range raw {
		rows = append(rows, ThemePageStructureSnapshot{ID: r.ID, Structure: r.Structure})
	}
	return rows, nil
}

// UpdateStructureSnapshot 写单页 settings.structure（不动内容与版本）。
//
// projectID 必填（DB-009 第三批）：裸 SQL UPDATE，作用域只能由调用方给（见 UpdateThemeSnapshot）。
func (m *Model) UpdateStructureSnapshot(ctx context.Context, projectID, pageID string, structureJSON []byte) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Exec(
			"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,structure}', ?, true), update_time = ? "+
				"WHERE id = ? AND project_id = ? AND deleted_at IS NULL",
			structureJSON, time.Now().UTC(), pageID, projectID,
		).Error
	})
}

// MarkStaleForTheme 把**本工程内**挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用），
// 返回本次 UPDATE **真正命中**的页面 ID。
//
// 为什么改成 RETURNING id（影响面回执那一批）：整站标记原先只回 error，service 的逐工程
// 扇出手里因此没有任何逐页凭据 —— 「这次换主题 / 改页眉块影响了哪些页面」只剩「全站 stale
// 数」这一个近似值，而主题一变恰恰是全站都 stale，那个数字最没有区分度。写法与既有
// MarkStaleByDependency 一致（幂等：重复标记同一页仍会返回它）。
//
// projectID 必填（DB-009 第三批）：themeID 只说明「哪套主题」，说不出「哪个工程」；
// pages 带 FORCE 策略，漏作用域时这条 UPDATE 在非超级角色下匹配 0 行且不报错 ——
// 现象是「换了主题设置但页面不被标记待重建」，站点上一直跑旧产物。
func (m *Model) MarkStaleForTheme(ctx context.Context, projectID, themeID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(
			"UPDATE pages SET stale = true, update_time = ? "+
				"WHERE project_id = ? AND theme_id = ? AND deleted_at IS NULL "+
				"RETURNING id",
			time.Now().UTC(), projectID, themeID,
		).Scan(&ids).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// MarkStaleForI18n 把**本工程内**全部未删除页面标记为待重建（界面文案词条变更后调用），
// 返回本次 UPDATE **真正命中**的页面 ID（RETURNING id，与 MarkStaleForTheme 同形）。
//
// 这是本模块命中面最大的整站标记（一条 UPDATE 覆盖全站），也正是最需要逐页样本的地方：
// 只有「N 个页面」时读者无法判断这次改动是否真的按预期铺开，而样本能让「哪些页被标了」
// 落到具体标题 / 路径上。
//
// 文案词条（sys_i18n）参与构建：组件固定文案由构建期取词注入 HTML 字节
// （docs/06-D §10）。词条改动后所有页面产物都可能过期，故整站标记 stale；
// 触发源为后台 i18n CRUD（决策 D7，尚未实现）或运维脚本，内核只提供能力。
// 与 MarkStaleForTheme 同一模式（stale=true 幂等）。
//
// projectID 必填（DB-009 第三批）：本方法原是「全表更新」，即模型层唯一一处隐含的
// 「不限工程」。词条变更的调用方（后台翻译页）没有工程上下文，所以「全站」由 service
// 层逐工程拼出来 —— 这里的 project_id 条件与策略是双保险，缺作用域直接显式失败。
func (m *Model) MarkStaleForI18n(ctx context.Context, projectID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var merr error
		ids, merr = m.markStaleForI18nIn(ctx, tx, projectID, time.Now().UTC())
		return merr
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// markStaleForI18nIn 整站标记的 SQL 本体：MarkStaleForI18n（自带事务）与
// MarkStaleForI18nTx（在调用方事务内，见 page_tx.go）两条入口共用同一段语句 ——
// 两处各写一遍 SQL 的话，改动时漏一处会让「自带事务」与「透传事务」两条路径
// 对同一批页面产生不同的标记结果，而两者在调用方眼里是同一个语义。
func (m *Model) markStaleForI18nIn(ctx context.Context, tx *gorm.DB, projectID string, at time.Time) (ids []string, err error) {
	err = tx.Raw(
		"UPDATE pages SET stale = true, update_time = ? WHERE project_id = ? AND deleted_at IS NULL "+
			"RETURNING id",
		at, projectID,
	).Scan(&ids).Error
	return ids, err
}

// MarkStaleByIDs 在**指定工程作用域内**按页面 ID 列表标记待重建，返回**本次真正命中**的页面 ID。
//
// 与 MarkStaleForI18n 的整站标记区分：调用方已经算出了精确的影响集合
// （如「产物由旧组件产出」的页面），不做无谓的全站标记。
//
// 返回值是 RETURNING id 的回读结果，**不是入参 ids 的回显**（2026-09 收口，与
// MarkStaleForTheme / MarkStaleByDependency / MarkStaleForBlock 同形）：
// 入参里可能混着已删除、已换工程或根本不存在的 id，回显入参会让「日志说标了 8 个、
// 其实只有 3 个存在」永远查不出来 —— 而调用方（MarkStaleByRegistryVersion）正是拿
// 这个集合做影响面日志与回执的。幂等：重复标记同一页仍会返回它。
//
// projectID 必填（DB-009 第三批）。调用方（组件版本变更）手里的 ids 来自 artifact 元数据，
// 可能横跨多个工程 —— 这也是为什么这里的 WHERE 同时带上 project_id：每个工程各自一次
// 独立作用域的事务（service 层逐工程调用），本工程之外的行由 project_id 条件与策略双重拦下，
// 不存在「把多个工程的 id 并进一次查询」的依赖。漏作用域时这条 UPDATE 会静默 0 行。
//
// 为什么是 id = ANY(string_to_array(?, ',')::uuid[]) 而不是 gorm 的 id IN ?（参数规模）：
// 入参来自「全站待重建 id 列表」，一次组件更新可能上万，而 gorm 的 IN ? 会把它展开成
// 同数量的绑定参数、逼近 PostgreSQL 的 65535 参数上限（超限直接报错，结果是组件更新后
// 全站一个页面都标不上）。ANY(数组) 只占一个参数、仍是**单条语句**（原子性与 IN ? 完全相同，
// 不需要退化成「事务 + 分块」），并且 `id = ANY(uuid[])` 仍能走 pages 的主键索引
// （写成 id::text = ANY(text[]) 也能跑，但类型转换会让主键索引失效，上万 id 时退化成全表扫）。
// 数组以逗号拼接传入：pages.id 是 uuid（形如 8-4-4-4-12 十六进制，不含逗号），分隔安全。
// 数组元素类型必须是 uuid 而不是 text：pages.id 的列型是 uuid，PostgreSQL 没有
// uuid = text 算子（实测报 `operator does not exist: uuid = text`），
// 而 ::uuid[] 对非法 id 的报错与原先 IN ? 的绑定参数取 uuid 时完全一致。
// 这也解释了为什么不能照抄 order/model 里 `status = ANY(string_to_array(?, ',')::text[])`
// 的写法 —— 那里比较的是 text 列，这里比较的是 uuid 列。
// at 参数保留给未来的 update_time 写入，当前实现与原行为一致（不动 update_time）。
func (m *Model) MarkStaleByIDs(ctx context.Context, projectID string, ids []string, at time.Time) (marked []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(
			"UPDATE pages SET stale = true WHERE project_id = ? AND deleted_at IS NULL "+
				"AND id = ANY(string_to_array(?, ',')::uuid[]) "+
				"RETURNING id",
			projectID, strings.Join(ids, ","),
		).Scan(&marked).Error
	})
	if err != nil {
		return nil, err
	}
	return marked, nil
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

// blockStructureMatchCond settings.structure 的槽位绑定匹配条件（页眉 / 页脚 / 其余槽位）。
//
// 三个通道：headerBlockId / footerBlockId 是既有字段，Slots 是「槽位名 → 全局块 ID」
// 的合并通道（合并规则见 builder.StructureBindings.SlotBindings，Slots 优先）。
// 漏掉 Slots 的后果是确定的：公告条 / 侧边栏这类槽位绑定的块**查不出引用** ——
// 块被删掉、页面在下次构建后少一个区块，而删除动作一路绿灯（审计 ARCH-02）。
//
// 为什么用 jsonb_path_query_array 收集槽位值而不是拼字符串：Slots 的值是 JSON 字符串，
// LIKE '%"id"%' 会同时命中「正文里恰好写了这个 id」的噪音 —— 引用保护宁可多拦一次
// （人工确认），不可制造一堆假引用让人忽略提示。也不用 jsonb_each_text：它对非 object
// 的 slots（历史数据 / 手工改坏的文档）会直接报错，让删除入口 500；路径查询对缺失、
// null、非 object 一律返回空序列，语义是「没有这条引用」。
const blockStructureMatchCond = `(draft_document->'settings'->'structure'->>'headerBlockId' = ?::text
			OR draft_document->'settings'->'structure'->>'footerBlockId' = ?::text
			OR jsonb_path_query_array(draft_document, '$.settings.structure.slots.*') @> jsonb_build_array(?::text))`

// CountBlockReference 统计**本工程内**引用该块的未删除页面数（与 MarkStaleForBlock 同一匹配条件）：
// core.globalref 节点（blockId）或 settings.structure 的页眉/页脚/槽位绑定。
// 供 block 模块删除/切换 global→template 前的引用拦截（docs/02-D §9）与块列表页的「影响面」列。
//
// projectID 必填（DB-009 第三批）：块 id 本身说不出工程，而 pages 带 FORCE 策略。
// 「全站引用数」由调用方（service）逐工程调用后求和 —— 这正是 block 模块删块前那道
// 拦截的判据：漏作用域时它静默返回 0，于是「有页面在引用」的块被安静地删掉。
func (m *Model) CountBlockReference(ctx context.Context, projectID, blockID string) (count int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("project_id = ? AND deleted_at IS NULL AND ("+
				blockRefMatchCond+" OR "+blockStructureMatchCond+")",
				projectID, blockID, blockID, blockID, blockID,
			).Count(&count).Error
	})
	return count, err
}

// BlockSourceRefRow 引用某块的页面行（审计 ARCH-02 的删除保护读模型）。
//
// 两个通道分列而不是合并成一条：页眉绑定与正文里插一块的**解除路径不同**
// （页面设置 vs 编辑器），提示里说清是哪一条，操作者才知道去哪里改。
type BlockSourceRefRow struct {
	ID          string `gorm:"column:id"`
	Path        string `gorm:"column:draft_path"`
	InDocument  bool   `gorm:"column:in_document"`
	InStructure bool   `gorm:"column:in_structure"`
}

// ListBlockSourceRefs 列出**本工程内**引用了该块的未删除页面，并标出命中通道
// （文档树 / settings.structure 槽位绑定；同一页面可同时命中两条）。
//
// 与 CountBlockReference 的区别只在形状：那条回答「有几处」，这条回答「是哪些、走哪条通道」——
// 删除保护要把「哪一类引用、哪些实体」告诉操作者，计数说不出实体。
// 两者共用同一组匹配条件（blockRefMatchCond + blockStructureMatchCond），
// 改一处不改另一处就会出现「计数说有引用、明细却是空的」这种自相矛盾的提示。
func (m *Model) ListBlockSourceRefs(ctx context.Context, projectID, blockID string) (rows []BlockSourceRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(
			"SELECT id::text AS id, draft_path, ("+blockRefMatchCond+") AS in_document, ("+blockStructureMatchCond+") AS in_structure "+
				"FROM pages WHERE project_id = ? AND deleted_at IS NULL AND ("+
				"("+blockRefMatchCond+") OR ("+blockStructureMatchCond+")) "+
				"ORDER BY draft_path ASC, id ASC",
			blockID, blockID, blockID, blockID, // SELECT：文档树 1 + 结构 3
			projectID,
			blockID, blockID, blockID, blockID, // WHERE：文档树 1 + 结构 3
		).Scan(&rows).Error
	})
	return rows, err
}

// BlockRevisionRefRow 引用了某块的历史修订（审计 ARCH-02 补齐的第三类页面引用）。
//
// 带修订版本号而不是只给页面：修订是可回滚的源码历史，操作者要判断的是
// 「这一版还该不该留」，只说「页面 /x」等于让他自己去翻修订列表。
type BlockRevisionRefRow struct {
	PageID  string `gorm:"column:page_id"`
	Path    string `gorm:"column:draft_path"`
	Version int64  `gorm:"column:version"`
}

// ListBlockRevisionRefs 列出**本工程内**历史修订文档引用了该块的页面修订（审计 ARCH-02）。
//
// 为什么历史修订也要算源码引用：page_revisions 是**可回滚的源码历史**（回滚是一个显式动作，
// 回滚后引用就回到当前草稿上），与 content_template_versions 是同一类事实 ——
// 只查当前草稿会让「删掉块 → 回滚到旧修订」在回滚那一刻才暴露断链（最难排查的一类）。
// 与「历史 artifact 不阻断」不冲突：修订不是编译产物，它有保留期兜底
// （page_retention：90 天 / 每页最近 20 个版本），不会造成永久阻断。
//
// page_revisions 没有 project_id 列（属主是页面），工程作用域由 JOIN 的 pages 行承担；
// 页面已软删时其修订不再阻断（修订跟着页面走，页面都不在了谈不上回滚）。
func (m *Model) ListBlockRevisionRefs(ctx context.Context, projectID, blockID string) (rows []BlockRevisionRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(
			"SELECT r.page_id::text AS page_id, p.draft_path, r.version "+
				"FROM page_revisions r JOIN pages p ON p.id = r.page_id "+
				"WHERE p.project_id = ? AND p.deleted_at IS NULL "+
				"AND jsonb_path_query_array(r.draft_document, '$.**.blockId') @> jsonb_build_array(?::text) "+
				"ORDER BY p.draft_path ASC, r.version ASC",
			projectID, blockID,
		).Scan(&rows).Error
	})
	return rows, err
}

// MarkStaleForBlock 把**本工程内**文档中经 core.globalref 引用（draft_document 树内
// "blockId": "<blockID>" 节点）或 settings.structure 的页眉/页脚/槽位绑定
// （headerBlockId / footerBlockId / slots，页面级覆盖，非主题默认）该块的页面标记为待重建，
// 返回本次 UPDATE **真正命中**的页面 ID（RETURNING id）。
// 与 MarkStaleForTheme 可能重叠命中同一页面，stale=true 幂等，无妨。
//
// 匹配条件与 CountBlockReference / ListBlockSourceRefs 共用同一组常量：
// 删除保护说「这个块被引用」而变更传播不覆盖同一处引用时，两边会各说各话 ——
// 现象是「删不掉，但改了它，页面也从不重建」（ARCH-02 的同一处缺口）。
//
// projectID 必填（DB-009 第三批）：块 id 说不出工程，而这条 UPDATE 的可见范围由策略决定。
// 「全站标记」由 service 层逐工程调用拼出来；漏作用域时它静默匹配 0 行 ——
// 现象是「改了全局块，引用它的页面不被标记」，站点上一直显示旧块内容。
func (m *Model) MarkStaleForBlock(ctx context.Context, projectID, blockID string) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(
			"UPDATE pages SET stale = true, update_time = ? WHERE project_id = ? AND deleted_at IS NULL AND ("+
				blockRefMatchCond+" OR "+blockStructureMatchCond+") "+
				"RETURNING id",
			time.Now().UTC(), projectID, blockID, blockID, blockID, blockID,
		).Scan(&ids).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
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

// GetByID 按 ID 查询本工程内未删除的 Page（projectID 必填，防跨工程 IDOR）。
//
// projectID 必填（DB-009 第五批）：这里原先保留着「projectID 为空 = 不限工程」的历史
// 分支。第四批把 page 侧所有按 id 的调用点都改成了逐工程定位，那个分支已经**没有任何
// 调用者**；留着它就是留一条静默的 fail-closed 路径（换非超级角色后恒 ErrRecordNotFound，
// 表现为「页面不存在」）。现在缺工程直接显式失败。
func (m *Model) GetByID(ctx context.Context, id, projectID string) (e *PageEntity, err error) {
	e = &PageEntity{}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("id = ? AND deleted_at IS NULL AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		return nil, err
	}
	return e, nil
}

// ListRevisions 在**指定工程内**按版本倒序读取页面的修订快照。
//
// projectID 必填（DB-009 第四批）：page_revisions **没有 project_id 列**，不在迁移 215 的
// 清单里，因此它不受策略约束 —— 换角色后「按 page_id 直查」照样能读到**别的工程**的
// 历史文档。这个入口的隔离只能靠父实体：先确认页面属于该工程，再读它的修订。
func (m *Model) ListRevisions(ctx context.Context, projectID, pageID string) (list []RevisionEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
			return err
		}
		return tx.Model(&RevisionEntity{}).Where("page_id = ?", pageID).
			Order("version DESC").Find(&list).Error
	})
	return list, err
}

// PruneRevisions 把单页的历史快照收敛到「最近 keep 个」，返回删除行数。
//
// 保存草稿后顺手调用（IDX-005）：改一次存一份完整 draft_document，高频编辑的页面
// 会把表撑起来，而保留条数之外的历史版本本来就是给回退用的、不需要无限留着。
// 只按条数收敛、不看时间：编辑者刚存的那几个版本必须都在。
//
// projectID 必填（DB-009 第四批）：修订表不受策略约束（无 project_id 列），
// 缺归属校验时这条 DELETE 能删掉**别的工程**页面的历史版本。先经 pages 确认归属。
func (m *Model) PruneRevisions(ctx context.Context, projectID, pageID string, keep int) (int64, error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if strings.TrimSpace(pageID) == "" || keep < 1 {
		return 0, nil
	}
	var deleted int64
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requirePageOwned(ctx, tx, projectID, pageID); err != nil {
			return err
		}
		var threshold int
		err := tx.Model(&RevisionEntity{}).Where("page_id = ?", pageID).
			Order("version DESC").Offset(keep-1).Limit(1).
			Pluck("version", &threshold).Error
		if err != nil || threshold <= 1 {
			// 没有第 keep 个版本（说明总数还不够）→ 无可收敛。
			return err
		}
		res := tx.Model(&RevisionEntity{}).Where("page_id = ? AND version < ?", pageID, threshold).
			Delete(&RevisionEntity{})
		deleted = res.RowsAffected
		return res.Error
	})
	return deleted, err
}

// DeleteStaleRevisions 全库分批清理「超出保留条数**且**早于保留期」的历史快照。
//
// 两个条件同时满足才删，是刻意的保守取舍：
//   - 只看条数：刚发布后密集保存的版本会被立刻删掉，而这正是编辑者要回退的东西；
//   - 只看时间：长期不编辑的页面反而留不住上限（老版本永远删不掉）。
//
// 已发布版本不在这里单独排除：page_revisions 没有「哪个版本已发布」的标记（发布状态在
// publication / artifacts 侧），因此以保留期兜底 —— 保留期内的版本一律不动。
//
// projectID 必填（DB-009 第四批）：修订表无 project_id 列、不受策略约束，所以「全库清理」
// 必须由调用方**逐工程**展开 —— 否则这条 DELETE 会跨工程删除历史快照，而它连一句日志
// 都不会报。归属经 pages 判断（EXISTS 子查询）而不是靠会话变量，读起来更直白。
func (m *Model) DeleteStaleRevisions(ctx context.Context, projectID string, keep int, cutoff time.Time, limit int) (int64, error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if keep < 1 || limit < 1 {
		return 0, nil
	}
	// ctid 定位：PostgreSQL 的 DELETE 不支持 LIMIT，用子查询挑出本批目标。
	const q = `DELETE FROM page_revisions WHERE ctid IN (
		SELECT ctid FROM (
			SELECT ctid, row_number() OVER (PARTITION BY page_id ORDER BY version DESC) AS rn, create_time, page_id
			FROM page_revisions
		) t WHERE t.rn > ? AND t.create_time < ?
		  AND EXISTS (SELECT 1 FROM pages p WHERE p.id = t.page_id AND p.project_id = ?)
		LIMIT ?
	)`
	res := m.DB(ctx).Exec(q, keep, cutoff, projectID, limit)
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
// projectID 必填（DB-009 第四批）：pages 带 FORCE 策略，本事务第一条就是 UPDATE pages ——
// 不设 app.project_id 时它在非超级角色下匹配 0 行，返回的却是 ErrDraftVersionConflict
// （乐观锁未命中），于是「保存草稿」被误报成「版本冲突」。
func (m *Model) SaveDraftWithRevision(
	ctx context.Context,
	projectID string,
	pageID string,
	expectedVersion int64,
	path string,
	document json.RawMessage,
	nextVersion int64,
	updatedAt time.Time,
	revision *RevisionEntity,
) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		result := tx.Model(&PageEntity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL AND draft_version = ?", pageID, projectID, expectedVersion).
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

// MoveDraftPath 发布改 URL 后同步草稿路径（逻辑路径，不含语言前缀）。
// 激活路径不再在此处写：它按语言存放在 page_publications，
// 由 MovePublicationPath 单独同步（多语言 P3，docs/06-D §15.5 第 2 条）。
// projectID 必填（DB-009 第四批）：裸 UPDATE pages，缺作用域时在非超级角色下匹配 0 行
// 且不报错 —— 表现为「改了 URL 但草稿路径没变」，而接口回报成功。
func (m *Model) MoveDraftPath(ctx context.Context, projectID, pageID, newPath string, at time.Time) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
			Updates(map[string]any{"draft_path": newPath, "update_time": at}).Error
	})
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

// requirePageOwned 校验页面属于给定工程，否则返回 gorm.ErrRecordNotFound。
//
// 用途（DB-009 第四批）：不带 project_id 列的子表（page_revisions / page_dependencies）
// 不在策略覆盖范围内，「这行属于哪个工程」只能经父实体判断。必须在**同一事务**里做：
// 事务外判定会留下 TOCTOU 窗口（判定后页面被迁走/删除）。
func (m *Model) requirePageOwned(ctx context.Context, tx *gorm.DB, projectID, pageID string) error {
	var n int64
	if err := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ?", pageID, projectID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
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
