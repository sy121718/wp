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
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	blockenums "go_wp/internal/module/block/enums"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// staleImpactPageLimit 影响面清单一次列出的页面数（与 /admin/pages 的 staleOverviewLimit 同值）。
//
// 这是**消费者口径**，所以定义在调用方：ListStalePages 的 limit 由调用方给，
// 不填时落到 model 的 50 条默认 —— 一份 50 行的清单即使折叠着也会让人觉得「影响面很大」。
// 只读区块的作用是让人**看见**影响面，不是给出完整清单；被截断的条数由 Total 给出并在页面上说明。
const staleImpactPageLimit = 8

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

// articleStaleImpact 取「全站待重建」影响面的数据（只读观测）。
//
// 取数走 page 契约的 ListStalePages：它与 /admin/pages 的「全站待重建」区块是**同一个查询**，
// 三处（pages / blocks / articles）因此显示同一份数与同一份清单。此前 content 与 block 各自
// 逐工程 List 再自行截断 —— 同一个「待重建」概念有三份实现，三处的数并不保证相同。
// 这份契约就是 block 侧那份注释里说的「要么放进 page 模块」的落点。
//
// 降级语义与 page 侧一致：失败 → Available=false（显示「读不到」），**绝不**渲染成
// 「0 个待重建」（那会把一次读取失败伪装成一切正常）；契约返回 (nil, nil) 是异常形态，
// 同样按读不到处理。
func articleStaleImpact(ctx context.Context, h *articlePageHandle, trs ...func(key, fallback string) string) gin.H {
	tr := articlePublishTr(trs)
	if h == nil || h.pages == nil {
		// 与 block 侧的待重建影响面文案**共用同一批 key**（两个页面说的是同一件事，
		// 各写一份的下场是同一个现象在两页上有两种说法 —— 见本文件顶部注释）。
		return staleImpactView(false, nil, 0, false,
			tr(blockenums.ImpactUnavailableNoPageCapability, "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。"))
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		Limit:      staleImpactPageLimit,
		Descending: true,
	})
	if err != nil {
		// 工程表为空时契约返回 ErrProjectRequired（没有可作用域的工程）：这里与真正的读取失败
		// 合并显示为「读不到」。page 侧能把它单独当作空态，是因为它同模块可直接引用该哨兵；
		// 跨模块 import page/service 是禁止的（AGENTS.md 模块边界），所以这里不做区分。
		logger.Scene("content").Error(err, "读取全站待重建清单失败，待重建影响面本次不可用")
		return staleImpactView(false, nil, 0, false,
			tr(blockenums.ImpactUnavailableProjectReadFailed, "读取站点工程失败，本次无法统计待重建影响面。"))
	}
	if res == nil {
		logger.Scene("content").Warn("全站待重建清单返回空结果（契约实现异常）")
		return staleImpactView(false, nil, 0, false, "")
	}
	pages := make([]gin.H, 0, len(res.Pages))
	for i := range res.Pages {
		// 转成 gin.H 而不是把契约 DTO 直接交给模板：模板的取值链是 .StaleImpact.Pages[i].Path，
		// map 缺键能被 isset 兜住，而契约 DTO 将来改名/加字段会直接打断整页渲染。
		pages = append(pages, gin.H{
			"ID":          res.Pages[i].ID,
			"Path":        res.Pages[i].Path,
			"ProjectID":   res.Pages[i].ProjectID,
			"ProjectName": res.Pages[i].ProjectName,
		})
	}
	return staleImpactView(true, pages, res.Total, res.Truncated, "")
}

// ArticleStaleDrawer 待重建页面清单的**只读抽屉**片段（GET /admin/articles/stale/drawer）。
//
// 与页头徽章同源（同一个 ListStalePages）：徽章给「有几个」，抽屉给「是哪几个」。
// 清单收进抽屉之后，文章列表页的首屏只留一行可点的徽章。
//
// 取不到数据一律只给状态码，不拼半截片段：drawer.js 对非 200 显示「加载失败，请重试」，
// 而缺 data-drawer-fragment 或缺列的片段会被它的 fragmentRoot 校验判非法 ——
// 两者在用户眼里是同一个失败界面，但后者还多花一次渲染。
func (h *articlePageHandle) ArticleStaleDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.pages == nil {
		c.Status(http.StatusNotFound)
		return
	}
	tr := shell.TranslateFor(c)
	res, err := h.pages.ListStalePages(c.Request.Context(), &pagecontract.StalePageListReq{
		Limit:      staleImpactPageLimit,
		Descending: true,
	})
	if err != nil {
		logger.Scene("content").Error(err, "读取全站待重建清单失败（抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	if res == nil {
		logger.Scene("content").Warn("全站待重建清单返回空结果（契约实现异常，抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	rows := make([]gin.H, 0, len(res.Pages))
	// 工程数决定「所属工程」列显不显示：单工程时那一列整列都是同一个名字，
	// 是纯噪声；多工程时缺了它又分不清两个工程同名的 /about。
	projects := make(map[string]struct{}, 2)
	for i := range res.Pages {
		projects[res.Pages[i].ProjectID] = struct{}{}
		rows = append(rows, gin.H{
			"ID":          res.Pages[i].ID,
			"Path":        res.Pages[i].Path,
			"ProjectName": res.Pages[i].ProjectName,
			// Published 区分「已发布但有更新」与「从未上线」：后者的下一步不是重建而是发布。
			"Published": res.Pages[i].Published,
			// 失败痕迹（迁移 474）：非空说明这页**不是还没轮到，而是重建失败过**。
			"FailedNote": rebuildFailureNote(tr, res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt),
		})
	}
	c.HTML(http.StatusOK, "admin/partials/stale_pages_drawer.html", shell.Prepare(c, gin.H{
		"Rows": rows, "Total": res.Total, "Truncated": res.Truncated,
		"Limit": staleImpactPageLimit, "MultiProject": len(projects) > 1,
	}))
}

// rebuildFailureNote 组装「最近一次自动重建失败」的一句文案；没有失败痕迹时返回空串。
//
// 阶段与时刻来自迁移 474 落在 pages 上的两列。**不含错误原文** —— 原文可能带 SQL / 路径 /
// 内部标识，后台页面不得直出内部错误（AGENTS.md 红线），它只进结构化日志（带 page_id 可定位）；
// 这里给的是「失败在哪一步、什么时候」，让人知道下一步去哪查。
//
// 与 /admin/blocks、/admin/pages 上同名函数是三份：它们各自在自己的模块包内（跨模块共用
// 要走契约，而这是纯展示装配）。三处文案与阶段取值必须一致，改一处请同步另两处。
func rebuildFailureNote(tr func(key, fallback string) string, stage string, at *utils.JSONTime) string {
	if strings.TrimSpace(stage) == "" && at == nil {
		return ""
	}
	label := stage
	switch stage {
	case "plan":
		label = tr("admin.pages.impact.stage_plan", "计划阶段（站点语言清单或旧发布范围读不到）")
	case "build":
		label = tr("admin.pages.impact.stage_build", "构建 / 发布阶段")
	}
	prefix := tr("admin.pages.impact.rebuild_failed", "最近一次自动重建失败：")
	if at == nil {
		return prefix + label
	}
	return prefix + label + "（" + time.Time(*at).Local().Format("2006-01-02 15:04") + "）"
}
