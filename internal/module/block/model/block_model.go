// Package blockmodel 实现 blocks 表持久化：全局块（跨页面复用的结构片段）。
package blockmodel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ErrProjectRequired 调用方没有给出工程作用域（与 page model 同形）。
//
// blocks 带 FORCE 策略（迁移 215）：不设 app.project_id 的读取在换非超级角色后
// **静默返回 0 行**，而「查不到引用」在删除保护里等于放行 —— 所以缺作用域必须
// 显式失败，不能退化成空集合。
var ErrProjectRequired = errors.New("block: 需要显式工程作用域")

const tableNameBlocks = "blocks"

// 块类型（docs/02-D §4/§5）：站点骨架 / 复用内容段 / 布局骨架 / 片段模板。
// header/footer 为页眉页脚候选；snippet 为片段模板类（配合 ReuseTemplate 一次性复制）。
const (
	KindBlock        = "block"        // 普通区块
	KindHeader       = "header"       // 页眉
	KindFooter       = "footer"       // 页脚
	KindAnnouncement = "announcement" // 公告栏（促销条，页眉上方）
	KindSidebar      = "sidebar"      // 侧边栏
	KindBreadcrumb   = "breadcrumb"   // 面包屑导航
	KindDrawer       = "drawer"       // 移动端抽屉导航
	KindSearch       = "search"       // 全局搜索框
	KindCTA          = "cta"          // 全局 CTA 段（订阅/联系）
	KindTrust        = "trust"        // 信任徽章/支付方式条
	KindBrands       = "brands"       // 品牌 logo 墙
	KindContact      = "contact"      // 客服联系方式条
	KindAbout        = "about"        // 「关于我们」简介段
	KindBanner       = "banner"       // 全宽横幅
	KindGrid         = "grid"         // 多栏布局
	KindSnippet      = "snippet"      // 片段模板（商品卡/表单/弹窗，template 复用方式的典型 kind）
)

// 复用方式（docs/02-D §5）。
const (
	// ReuseGlobal 全局引用：页面存 block_id，改处处变 + stale 传播。
	ReuseGlobal = "global"
	// ReuseTemplate 一次性复制：插入时复制完整 AST（重生成 Node ID），此后独立、不传播 stale。
	ReuseTemplate = "template"
)

// DefaultCategory 块默认分类（自由分类体系，组织/筛选维度）。
const DefaultCategory = "general"

// BlockEntity 对应 blocks 表：全局块（组件树文档与页面 root 同构）。
type BlockEntity struct {
	ID         string          `gorm:"column:id;primaryKey"`
	ProjectID  string          `gorm:"column:project_id;not null"`
	Name       string          `gorm:"column:name;not null"`
	Kind       string          `gorm:"column:kind;not null"`
	Category   string          `gorm:"column:category;not null;default:general"`
	ReuseMode  string          `gorm:"column:reuse_mode;not null;default:global"`
	Document   json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreateTime time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time       `gorm:"column:update_time;not null"`
}

func (BlockEntity) TableName() string { return tableNameBlocks }

// Model blocks 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewBlockModel 创建 Block Model。
func NewBlockModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 blocks 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&BlockEntity{})
}

// ListAllProjectIDs 列出全部站点工程 id（只带 id 的入口做逐工程定位时的兜底清单）。
//
// 为什么 block model 要读 projects 表（DB-009）：「按 id 更新 / 删除 / 复制」这些入口的
// 请求里只有块 id，而 blocks 在迁移 215 里带 FORCE 策略 —— 作用域只能落到某个具体工程，
// 归属必须先探测出来。探测要枚举工程清单，清单来自 project 契约；契约未注入时（测试装配，
// 或将来某个装配点漏接）定位会整体失败，表现为「块明明在却报不存在」，而日志里什么都没有。
// page / order / navigation model 的 ListAllProjectIDs 是同一处境的同形兜底。
//
// projects 是隔离的**主体**：它没有 project_id 列、不在迁移 215 的策略名单里，
// 读它不涉及任何被隔离数据。正确做法仍是装配点注入 project 契约（生产装配已注入）。
func (m *Model) ListAllProjectIDs(ctx context.Context) (ids []string, err error) {
	err = m.db.WithContext(ctx).
		Raw("SELECT id::text FROM projects ORDER BY create_time ASC, id ASC").Scan(&ids).Error
	return ids, err
}

// Create 新增块。
// RLS（迁移 215）：blocks 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
func (m *Model) Create(ctx context.Context, e *BlockEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).Create(e).Error
	})
}

// ListByProject 列出工程全部块（kind/category/reuseMode 可选过滤；类型序 + 创建序）。
func (m *Model) ListByProject(ctx context.Context, projectID, kind, category, reuseMode string) (list []BlockEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&BlockEntity{}).Where("project_id = ?", projectID)
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		if category != "" {
			q = q.Where("category = ?", category)
		}
		if reuseMode != "" {
			q = q.Where("reuse_mode = ?", reuseMode)
		}
		return q.Order("kind ASC, create_time ASC").Find(&list).Error
	})
	return list, err
}

// ExistsByName 判断工程下是否已存在同名块（大小写不敏感，参数化单条查询）。
// 用于 Create 前判重：避免拉全量 Document(jsonb) 大字段后在内存 EqualFold。
// 注意：并发下同名仍可能穿透（需 DB 唯一索引兜底，见 service 层说明）。
func (m *Model) ExistsByName(ctx context.Context, projectID, name string) (exists bool, err error) {
	var count int64
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).
			Where("project_id = ? AND LOWER(name) = LOWER(?)", projectID, name).
			Count(&count).Error
	})
	return count > 0, err
}

// GetByID 按 ID 查询块。projectID 非空时追加工程归属条件（防跨工程 IDOR）。
func (m *Model) GetByID(ctx context.Context, id string, projectID string) (e *BlockEntity, err error) {
	e = &BlockEntity{}
	// projectID 为空是「不限工程」的历史调用形态：不设 scope 时不筛工程，换非超级角色后
	// 该路径会 fail closed（0 行 → ErrRecordNotFound）而**不会**读到别的工程，方向是安全的；
	// 需要该路径可用时必须由调用方补上 projectID（列入 DB-009 剩余清单）。
	if strings.TrimSpace(projectID) == "" {
		if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
			return nil, err
		}
		return e, nil
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).Where("id = ?", id).Where("project_id = ?", projectID).First(e).Error
	}); err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateDocument 更新块名称、类型、分类、复用方式与文档（覆盖式，编辑器整树保存）。
//
// projectID 非空时在工程作用域内写（blocks 带 FORCE 策略）：越界写会被 WITH CHECK
// 直接拒绝而不是静默改到别的工程。为空沿用「不限工程」的历史形态（DB-009 剩余清单）。
func (m *Model) UpdateDocument(ctx context.Context, projectID, id string, name, kind, category, reuseMode string, document json.RawMessage, updatedAt time.Time) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
			"name": name, "kind": kind, "category": category, "reuse_mode": reuseMode, "document": document, "update_time": updatedAt,
		}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
			"name": name, "kind": kind, "category": category, "reuse_mode": reuseMode, "document": document, "update_time": updatedAt,
		}).Error
	})
}

// BlockDocRefRow 文档树里引用了目标块的其它全局块（只读定位数据，审计 ARCH-02）。
type BlockDocRefRow struct {
	ID   string `gorm:"column:id"`
	Name string `gorm:"column:name"`
}

// ListBlockDocumentRefs 列出**本工程内**文档树引用了 blockID 的其它全局块。
//
// 这是 ARCH-02 补上的第三类引用：块引用块（嵌套 globalref）此前完全不在删除保护
// 的判据里 —— 删掉内层块，外层块在下次构建时静默少一段，只有在产物上才看得出来。
//
// 排除自身：构建期的防环（visited 集合）已经拦下自引用，但库里若存在这样的行，
// 它不该让「删除这个块」被它自己挡住。
//
// 与页面侧同一判定形态（jsonb_path_query_array + @>）：blocks 表没有块引用索引，
// 这里按工程小范围扫描 —— 单工程块数量在几十量级，且删除是低频动作。
func (m *Model) ListBlockDocumentRefs(ctx context.Context, projectID, blockID string) (rows []BlockDocRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(
			"SELECT id::text AS id, name FROM blocks "+
				"WHERE project_id = ? AND id::text <> ?::text "+
				"AND jsonb_path_query_array(document, '$.**.blockId') @> jsonb_build_array(?::text) "+
				"ORDER BY name ASC, id ASC",
			projectID, blockID, blockID,
		).Scan(&rows).Error
	})
	return rows, err
}

// Delete 删除块。
// projectID 非空时在工程作用域内删（越界删在换角色后会被策略拒绝，而不是删掉别的工程的块）。
func (m *Model) Delete(ctx context.Context, projectID, id string) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id = ?", id).Delete(&BlockEntity{}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&BlockEntity{}).Where("id = ? AND project_id = ?", id, projectID).Delete(&BlockEntity{}).Error
	})
}
