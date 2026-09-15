// Package blockmodel 实现 blocks 表持久化：全局块（跨页面复用的结构片段）。
package blockmodel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

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
	ID         int64           `gorm:"column:id;type:bigint;primaryKey"`
	ProjectID  string          `gorm:"column:project_id;type:uuid;not null"`
	Name       string          `gorm:"column:name;type:text;not null"`
	Kind       string          `gorm:"column:kind;type:text;not null"`
	Category   string          `gorm:"column:category;type:text;not null;default:general"`
	ReuseMode  string          `gorm:"column:reuse_mode;type:text;not null;default:global"`
	Document   json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreateTime time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time       `gorm:"column:updated_at;not null"`
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

// Create 新增块。
func (m *Model) Create(ctx context.Context, e *BlockEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// ListByProject 列出工程全部块（kind/category/reuseMode 可选过滤；类型序 + 创建序）。
func (m *Model) ListByProject(ctx context.Context, projectID, kind, category, reuseMode string) (list []BlockEntity, err error) {
	q := m.DB(ctx).Where("project_id = ?", projectID)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if reuseMode != "" {
		q = q.Where("reuse_mode = ?", reuseMode)
	}
	err = q.Order("kind ASC, create_time ASC").Find(&list).Error
	return list, err
}

// ExistsByName 判断工程下是否已存在同名块（大小写不敏感，参数化单条查询）。
// 用于 Create 前判重：避免拉全量 Document(jsonb) 大字段后在内存 EqualFold。
// 注意：并发下同名仍可能穿透（需 DB 唯一索引兜底，见 service 层说明）。
func (m *Model) ExistsByName(ctx context.Context, projectID, name string) (exists bool, err error) {
	var count int64
	err = m.DB(ctx).
		Where("project_id = ? AND LOWER(name) = LOWER(?)", projectID, name).
		Count(&count).Error
	return count > 0, err
}

// GetByID 按 ID 查询块。projectID 非空时追加工程归属条件（防跨工程 IDOR）。
func (m *Model) GetByID(ctx context.Context, id int64, projectID string) (e *BlockEntity, err error) {
	e = &BlockEntity{}
	q := m.DB(ctx).Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if err = q.First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateDocument 更新块名称、类型、分类、复用方式与文档（覆盖式，编辑器整树保存）。
func (m *Model) UpdateDocument(ctx context.Context, id int64, name, kind, category, reuseMode string, document json.RawMessage, updatedAt time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
		"name": name, "kind": kind, "category": category, "reuse_mode": reuseMode, "document": document, "updated_at": updatedAt,
	}).Error
}

// Delete 删除块。
func (m *Model) Delete(ctx context.Context, id int64) (err error) {
	return m.DB(ctx).Where("id = ?", id).Delete(&BlockEntity{}).Error
}
