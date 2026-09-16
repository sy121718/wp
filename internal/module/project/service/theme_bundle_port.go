package projectservice

// theme_bundle_port.go — 主题包资产端口的默认实现（审计 VIS-014）。
//
// 只依赖 block / page 两个模块的 **contract**（规则允许的跨模块形状），把对方的能力
// 翻译成主题包需要的最小读写面：取块 / 建块 / 列页面 / 路径占用预检 / 建页。
//
// 归属校验与业务规则留在对方模块：这里不复制「块名同工程唯一」「页面路径占用」这类判据，
// 而是调用对方的入口并在被拒时**补一个可用的名字/跳过并上报**。复制规则会在对方演进时静默分叉。
//
// 装配：由 internal/routers 在 block / page 两侧构造完成后注入
//（projectService.SetThemeBundleAssetPort(projectservice.NewThemeBundleAssetPort(blockSvc, pageSvc))）。
// 未注入时导出/导入返回 ErrThemeBundlePortUnavailable，不静默降级成「只导令牌」。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"gorm.io/gorm"
)

var _ projectcontract.ThemeBundleAssetPort = (*themeBundleAssetAdapter)(nil)

// themeBundleAssetAdapter 主题包资产端口实现。
type themeBundleAssetAdapter struct {
	blocks blockcontract.BlockService
	pages  pagecontract.PageService
}

// NewThemeBundleAssetPort 创建主题包资产端口（装配期调用）。
func NewThemeBundleAssetPort(blocks blockcontract.BlockService, pages pagecontract.PageService) projectcontract.ThemeBundleAssetPort {
	return &themeBundleAssetAdapter{blocks: blocks, pages: pages}
}

// pagesReady 页面端口是否可用。
// 允许装配方只注入块端口（pages 为 nil）：此时页面相关能力统一返回
// ErrThemeBundlePortUnavailable，而不是在 nil 接口上 panic 或静默返回空页面集。
func (a *themeBundleAssetAdapter) pagesReady() bool { return a.pages != nil }

// GetBlock 取块详情；不存在映射为 ErrThemeBundleAssetMissing（导出侧据此记 warning 继续）。
func (a *themeBundleAssetAdapter) GetBlock(ctx context.Context, projectID, blockID string) (res *projectcontract.ThemeBundleBlock, err error) {
	blk, derr := a.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: projectID, ID: blockID})
	if derr != nil {
		if errors.Is(derr, blockcontract.ErrNotFound) || errors.Is(derr, gorm.ErrRecordNotFound) {
			return nil, ErrThemeBundleAssetMissing
		}
		return nil, derr
	}
	return &projectcontract.ThemeBundleBlock{
		ID: blk.ID, Name: blk.Name, Kind: blk.Kind, Category: blk.Category,
		ReuseMode: blk.ReuseMode, Document: blk.Document,
	}, nil
}

// CreateBlock 新建块（id 由 block 模块分配）。
//
// 名称去重在这里补：block 模块要求同工程块名唯一，而导入的包里携带的是**来源工程**的名字，
// 与目标工程重名极其常见 —— 直接透传会让整个导入失败，静默改名又会让使用者找不到对应的块。
// 折中是「加后缀并保留原始名可见」：名字前缀仍是包内的名字，括号里说明是导入产物。
func (a *themeBundleAssetAdapter) CreateBlock(ctx context.Context, req *projectcontract.ThemeBundleBlockCreate) (newID string, err error) {
	name, nerr := a.uniqueBlockName(ctx, req.ProjectID, req.Name)
	if nerr != nil {
		return "", nerr
	}
	res, cerr := a.blocks.Create(ctx, &blockcontract.CreateReq{
		ProjectID: req.ProjectID,
		Name:      name,
		Kind:      req.Kind,
		Category:  req.Category,
		ReuseMode: req.ReuseMode,
		Document:  req.Document,
	})
	if cerr != nil {
		return "", cerr
	}
	return res.ID, nil
}

// ListPages 列出并补齐页面文档（列表投影省略大字段，逐页取详情）。
func (a *themeBundleAssetAdapter) ListPages(ctx context.Context, projectID, themeID string) (res []projectcontract.ThemeBundlePage, err error) {
	if !a.pagesReady() {
		return nil, ErrThemeBundlePortUnavailable
	}
	list, lerr := a.pages.List(ctx, &pagecontract.ListReq{ProjectID: projectID, ThemeID: themeID})
	if lerr != nil {
		return nil, lerr
	}
	out := make([]projectcontract.ThemeBundlePage, 0, len(list))
	for _, item := range list {
		detail, derr := a.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: item.ID})
		if derr != nil {
			if errors.Is(derr, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, derr
		}
		out = append(out, projectcontract.ThemeBundlePage{
			ID: detail.ID, Kind: detail.Kind, DraftPath: detail.DraftPath, Document: detail.DraftDocument,
		})
	}
	return out, nil
}

// PagePathTaken 目标工程里该路径是否已被页面占用（草稿路径或已激活路径）。
func (a *themeBundleAssetAdapter) PagePathTaken(ctx context.Context, projectID, path string) (taken bool, err error) {
	if !a.pagesReady() {
		return false, ErrThemeBundlePortUnavailable
	}
	list, lerr := a.pages.List(ctx, &pagecontract.ListReq{ProjectID: projectID})
	if lerr != nil {
		return false, lerr
	}
	target := normalizeThemeBundlePath(path)
	for _, p := range list {
		if normalizeThemeBundlePath(p.DraftPath) == target {
			return true, nil
		}
		if p.ActivePath != nil && normalizeThemeBundlePath(*p.ActivePath) == target {
			return true, nil
		}
	}
	return false, nil
}

// CreatePage 新建页面（id 由 page 模块分配）。
func (a *themeBundleAssetAdapter) CreatePage(ctx context.Context, req *projectcontract.ThemeBundlePageCreate) (newID string, err error) {
	if !a.pagesReady() {
		return "", ErrThemeBundlePortUnavailable
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		// "page" 是 page 模块的常规页面类型（pageenums.PageKindPage）；
		// contract 未重导出该常量，这里用字面量并在此说明来源。
		kind = "page"
	}
	res, cerr := a.pages.Create(ctx, &pagecontract.CreateReq{
		ProjectID:     req.ProjectID,
		Kind:          kind,
		DraftPath:     req.DraftPath,
		DraftDocument: req.Document,
	})
	if cerr != nil {
		return "", cerr
	}
	return res.ID, nil
}

// uniqueBlockName 在目标工程里找一个可用块名（重名则加「(导入 N)」后缀）。
func (a *themeBundleAssetAdapter) uniqueBlockName(ctx context.Context, projectID, desired string) (name string, err error) {
	list, lerr := a.blocks.List(ctx, &blockcontract.ListReq{ProjectID: projectID})
	if lerr != nil {
		return "", lerr
	}
	used := make(map[string]struct{}, len(list))
	for _, b := range list {
		used[strings.ToLower(strings.TrimSpace(b.Name))] = struct{}{}
	}
	base := strings.TrimSpace(desired)
	if base == "" {
		base = "导入块"
	}
	candidate := base
	for i := 2; i <= 50; i++ {
		if _, hit := used[strings.ToLower(candidate)]; !hit {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s (导入 %d)", base, i)
	}
	return "", blockcontract.ErrDuplicate
}

// normalizeThemeBundlePath 路径归一化：去空白、统一前导斜杠、去尾部斜杠。
// 只用于「是否同一路径」的比较，不写回任何页面。
func normalizeThemeBundlePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}
