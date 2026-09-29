package contenthttp

// article_stale_impact.go — 依赖失效「影响面」的只读可见性。
//
// 现象：文章保存后，依赖扇出（pipeline.Fanout）会按依赖表把引用它的**手工页面**与
// **已发布详情页**标记为 stale；同一件事也会由块内容变更（page.MarkStaleForBlock /
// MarkStaleForTheme）、主题与导航变更触发。但这一步**只发生在日志与 pages.stale 列里** ——
// 运营在后台看不到「现在有多少个页面等着重建、是哪些页面」：
//
//   · 文章编辑页只有一句静态说明「引用这篇文章的页面会被标记待重建」（没有数字，没有清单）；
//   · pages.stale 只有一个徽标，没有任何地方把它汇总成影响面。
//
// 本文件补的是**只读可见性**：不改变任何构建 / 发布 / 标记行为，也**不做自动重建**
//（本批只做可见性）。数据源是 page 契约已有的只读面（List + PageResp.Stale），
// 因此不新造巡检页面、不新增路由与权限点。
//
// 性能口径：工程数与页面数都是后台量级，逐工程 List 一次即可；清单截断到
// staleImpactPageLimit 条并在页面上显式说明「还有更多」——不给出一份看起来完整、
// 实际被静默截断的清单（那会让人以为重建范围就这么大）。

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	blockenums "go_wp/internal/module/block/enums"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/pkg/logger"
)

// staleImpactPageLimit 影响面清单最多列出的页面数（超出部分只计数，页面上显式说明）。
const staleImpactPageLimit = 30

// staleImpactView 组装「待重建页面影响面」的渲染数据（纯函数，取数在 articleStaleImpact）。
//
// 统一返回 gin.H 而不是结构体：模板里是 map 取值链（.StaleImpact.Pages），
// 嵌套 map 在 Jet 上的求值路径最短、也最容易用 isset 判存在。
func staleImpactView(available bool, pages []gin.H, total int, truncated bool, hint string) gin.H {
	if pages == nil {
		pages = []gin.H{}
	}
	return gin.H{
		"Available": available,
		"Pages":     pages,
		"Total":     total,
		"Truncated": truncated,
		"Limit":     staleImpactPageLimit,
		"Hint":      hint,
	}
}

// articleStaleImpact 汇总当前所有工程里「待重建」的页面（只读）。
//
// 取不到数据时返回明确说明而不是空清单：「影响面 0」与「读不到影响面」是两件事，
// 混在一起会让运营把一次读取失败当成「没有待重建页面」。
func articleStaleImpact(ctx context.Context, h *articlePageHandle, trs ...func(key, fallback string) string) gin.H {
	tr := articlePublishTr(trs)
	if h == nil || h.pages == nil || h.projects == nil {
		// 与 block 侧的待重建影响面文案**共用同一批 key**（两个页面说的是同一件事，
		// 各写一份的下场是同一个现象在两页上有两种说法 —— 见本文件顶部注释）。
		return staleImpactView(false, nil, 0, false,
			tr(blockenums.ImpactUnavailableNoPageCapability, "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。"))
	}
	projects, err := h.projects.List(ctx)
	if err != nil {
		logger.Scene("content").Error(err, "读取工程列表失败，待重建影响面本次不可用")
		return staleImpactView(false, nil, 0, false,
			tr(blockenums.ImpactUnavailableProjectReadFailed, "读取站点工程失败，本次无法统计待重建影响面。"))
	}

	pages := make([]gin.H, 0, staleImpactPageLimit)
	total, truncated := 0, false
	for i := range projects {
		rows, lerr := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: projects[i].ID})
		if lerr != nil {
			// 单个工程读失败不整批失败，但必须留痕 —— 静默跳过会少算影响面。
			logger.Scene("content").With("project_id", projects[i].ID).Error(lerr, "读取工程页面失败，影响面可能少算")
			continue
		}
		for j := range rows {
			if !rows[j].Stale {
				continue
			}
			total++
			if len(pages) >= staleImpactPageLimit {
				truncated = true
				continue
			}
			pages = append(pages, gin.H{
				"ID":          rows[j].ID,
				"Path":        stalePagePath(rows[j]),
				"ProjectID":   projects[i].ID,
				"ProjectName": projects[i].Name,
			})
		}
	}
	return staleImpactView(true, pages, total, truncated, "")
}

// stalePagePath 页面的可读标识：优先已上线路径，其次草稿路径，都没有时退回 id
// （不编造路径 —— 一个不存在的 URL 比一串 id 更误导人）。
func stalePagePath(p pagecontract.PageResp) string {
	if p.ActivePath != nil && strings.TrimSpace(*p.ActivePath) != "" {
		return *p.ActivePath
	}
	if strings.TrimSpace(p.DraftPath) != "" {
		return p.DraftPath
	}
	return p.ID
}
