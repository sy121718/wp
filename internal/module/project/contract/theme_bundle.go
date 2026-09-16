package projectcontract

// theme_bundle.go — 主题包（Theme Bundle）导入导出契约（审计 VIS-014）。
//
// 为什么需要这个端口：主题包要把「主题令牌 + 引用到的块文档 + 可选页面文档」装进一个
// 可离线交付的 zip，而块与页面分别属于 block / page 两个模块 —— project 模块不能伸进
// 对方的表，也不允许在 service 里直查（AGENTS.md「model 层定位」与「表隔离约定」）。
// 所以这里按本项目既有形状（consumer-side port，样板见 LocaleRetirePort）声明一个
// **只覆盖主题包所需读写**的窄端口，由装配层用 block/page 的契约实现注入，
// 实现体在 project/service/theme_bundle_port.go（只依赖对方 contract）。
//
// 接口形状刻意收窄：只有「按 id 取一个块」「建一个块」「列某主题的页面」「建一个页面」，
// 拿不到删除、发布、改 URL 等越权能力。

import (
	"context"
	"encoding/json"
)

// ThemeBundleBlock 块资产的不可变投影（导出侧读取用）。
type ThemeBundleBlock struct {
	ID        string
	Name      string
	Kind      string
	Category  string
	ReuseMode string
	Document  json.RawMessage
}

// ThemeBundleBlockCreate 新建块（导入侧写入用）。
//
// 没有 ID 字段是刻意的：块 id 由实现方（block 模块）分配，主题包里的旧 id 一律不沿用。
type ThemeBundleBlockCreate struct {
	ProjectID string
	Name      string
	Kind      string
	Category  string
	ReuseMode string
	Document  json.RawMessage
}

// ThemeBundlePage 页面资产的不可变投影（导出侧读取用）。
type ThemeBundlePage struct {
	ID        string
	Kind      string
	DraftPath string
	Document  json.RawMessage
}

// ThemeBundlePageCreate 新建页面（导入侧写入用，id 同样由实现方分配）。
type ThemeBundlePageCreate struct {
	ProjectID string
	Kind      string
	DraftPath string
	Document  json.RawMessage
}

// ThemeBundleAssetPort 主题包所需的跨模块资产读写端口。
//
// 实现方约定：资产不存在时返回 ErrThemeBundleAssetMissing（错误哨兵由 project service 导出），
// 让导出侧能把「悬空引用」与「基础设施故障」区分开 —— 前者记 warning 继续，
// 后者必须整体失败（否则会导出一个看起来正常、实际缺块的包）。
type ThemeBundleAssetPort interface {
	// GetBlock 按 id 取块（含文档）。不存在即 ErrThemeBundleAssetMissing。
	GetBlock(ctx context.Context, projectID, blockID string) (res *ThemeBundleBlock, err error)
	// CreateBlock 新建块并返回新分配的 id。
	CreateBlock(ctx context.Context, req *ThemeBundleBlockCreate) (newID string, err error)
	// ListPages 列出工程下挂在指定主题的全部页面（含文档）。
	// themeID 为空时列出工程全部页面。
	ListPages(ctx context.Context, projectID, themeID string) (res []ThemeBundlePage, err error)
	// PagePathTaken 目标工程里该路径是否已被页面占用（导入页面前的预检，避免半途失败）。
	PagePathTaken(ctx context.Context, projectID, path string) (taken bool, err error)
	// CreatePage 新建页面并返回新分配的 id。
	CreatePage(ctx context.Context, req *ThemeBundlePageCreate) (newID string, err error)
}
