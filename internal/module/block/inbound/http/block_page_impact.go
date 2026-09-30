package blockhttp

// block_page_impact.go — 全局块变更的「影响面」只读可见性。
//
// 现象：块内容变更 / 删除后，装配层注入的 stale 传播器（internal/routers/block_page_bridge.go
// 的 BlockStalePropagator）会把「绑定了该块的主题下全部页面」与「文档里 globalref /
// settings.structure 引用了该块的页面」标记为 stale。但这件事对运营**完全不可见**：
// 块列表页只显示名称与更新时间，块编辑保存后除了页面上的徽章之外没有任何地方能回答
// 「改这个块会影响哪些页面」；reuse_mode=template 的块又**不传播**（插入时已复制 AST），
// 运营也无从区分这两类块。
//
// 本文件补的是**只读可见性**（本批不做自动重建）：
//   · 每个「引用」模式的块显示它当前被多少页面引用（page.CountBlockReference，只读）；
//     「复制」模式的块显示「—」并说明原因 —— 少一行数据不会误导人，
//     而把「不传播」的块也标上引用数会让人以为改它需要重建。
//   · 页面顶部汇总当前待重建页面数 + 清单（page.List 的 Stale 面）。
//
// 与 content 侧同名实现的取舍：两处各留一份约 40 行的只读统计，而不是抽公共包 ——
// 它依赖的是**各自的模块契约**（content 侧用 pagecontract + projectcontract，
// block 侧同一对），抽出去要么放进 page 模块（清单外）要么造成 block↔content 反向依赖。
// 两处的口径必须一致：改一边请同步另一边（页面上的文案也共用同一套 key 说明）。

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// blockStalePageLimit 影响面清单一次列出的页面数（与 /admin/pages 的 staleOverviewLimit 同值）。
//
// 消费者口径，定义在调用方：不填会落到 model 的 50 条默认，而只读区块的作用是让人**看见**
// 影响面、不是给出完整清单（被截断的条数由 Total 给出并在页面上说明）。
const blockStalePageLimit = 8

// blockStaleImpact 取「全站待重建」影响面的数据（只读观测）。
//
// 取数走 page 契约的 ListStalePages —— 与 /admin/pages 的「全站待重建」、/admin/articles 的
// 「待重建影响面」是同一个查询。**这里曾经是逐工程 List + 自行截断的第二份实现**：
// 那份注释说「抽公共包要么放进 page 模块」，而 ListStalePages 就是那个落点，
// 于是三处口径合并成一份（此前三处的数与清单长度都不保证相同）。
//
// 降级语义：pages 未注入 / 读取失败 → Available=false（显示「读不到」），
// 绝不渲染成「0 个待重建」—— 两者对运营的含义完全不同。契约返回 (nil, nil) 是异常形态，
// 同样按读不到处理。
func (h *blockPageHandle) blockStaleImpact(tr func(key, fallback string) string, ctx context.Context) gin.H {
	unavailable := func(hint string) gin.H {
		return gin.H{
			"Available": false, "Pages": []gin.H{}, "Total": 0, "Truncated": false,
			"Limit": blockStalePageLimit, "Hint": hint,
		}
	}
	if h == nil || h.pages == nil {
		return unavailable(tr(blockenums.ImpactUnavailableNoPageCapability, "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。"))
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		Limit:      blockStalePageLimit,
		Descending: true,
	})
	if err != nil {
		// 工程表为空时契约返回 ErrProjectRequired：与真正的读取失败合并显示为「读不到」，
		// 因为跨模块 import page/service 取那个哨兵是禁止的（AGENTS.md 模块边界）。
		logger.Scene("block").Error(err, "读取全站待重建清单失败，待重建影响面本次不可用")
		return unavailable(tr(blockenums.ImpactUnavailableProjectReadFailed, "读取站点工程失败，本次无法统计待重建影响面。"))
	}
	if res == nil {
		logger.Scene("block").Warn("全站待重建清单返回空结果（契约实现异常）")
		return unavailable("")
	}
	pages := make([]gin.H, 0, len(res.Pages))
	for i := range res.Pages {
		pages = append(pages, gin.H{
			"ID":          res.Pages[i].ID,
			"Path":        res.Pages[i].Path,
			"ProjectID":   res.Pages[i].ProjectID,
			"ProjectName": res.Pages[i].ProjectName,
		})
	}
	return gin.H{
		"Available": true, "Pages": pages, "Total": res.Total, "Truncated": res.Truncated,
		"Limit": blockStalePageLimit, "Hint": "",
	}
}

// fillRefCounts 给一组列表行填上「被多少页面引用」的显示文案（就地改切片元素）。
//
// 逐块一次 COUNT 查询：块数量是后台量级（每工程几十个），与本页其余查询同一量级；
// pages 未注入时 blockRefCount 返回 -1，全部显示为「未知」，不产生任何查询。
func (h *blockPageHandle) fillRefCounts(tr func(key, fallback string) string, ctx context.Context, rows []blockRow) {
	for i := range rows {
		rows[i].RefCountText = blockRefCountText(tr, rows[i], h.blockRefCount(ctx, rows[i]))
	}
}

// blockRefCountText 引用数 → 页面文案。
//
// 「0 个引用」「未知」「不传播」是三件不同的事，必须显示成三种文案：
// 把「读不到」显示成 0 会让人以为这个块没人用（进而放心删除），
// 把 template 块显示成 0 会让人以为它需要重建。
func blockRefCountText(tr func(key, fallback string) string, row blockRow, n int64) string {
	switch {
	case row.ReuseMode != blockmodel.ReuseGlobal:
		return "—"
	case n < 0:
		return tr(blockenums.RefCountUnknown, "未知")
	case n == 0:
		return tr(blockenums.RefCountNone, "未被页面引用")
	default:
		// 占位符是命名形态（{count}），不用 Sprintf：词条可被运营在后台改，
		// 裸 % 与中英参数错位都会让 Sprintf 输出乱码，命名替换对此免疫。
		return i18n.FillTranslate(tr, blockenums.RefCountPages, "{count} 个页面引用",
			map[string]string{"count": strconv.FormatInt(n, 10)})
	}
}

// blockRefCount 该块当前被多少页面引用（只读；page.CountBlockReference 同一口径，
// 也就是删除拦截用的那个计数）。pages 未注入或查询失败返回 -1，页面显示为未知。
//
// 只对 reuse_mode=global 的块算：template 块插入时已复制 AST，改它不影响任何页面，
// 给它标一个引用数会让人以为「改它需要重建」。
func (h *blockPageHandle) blockRefCount(ctx context.Context, b blockRow) int64 {
	if h == nil || h.pages == nil {
		return -1
	}
	if b.ReuseMode != blockmodel.ReuseGlobal {
		return -1
	}
	n, err := h.pages.CountBlockReference(ctx, b.ID)
	if err != nil {
		logger.Scene("block").With("block_id", b.ID).Error(err, "统计块引用页面数失败（影响面显示为未知）")
		return -1
	}
	return n
}

// BlocksStaleDrawer 待重建页面清单的**只读抽屉**片段（GET /admin/blocks/stale/drawer）。
//
// 与 /admin/articles 的同名抽屉共用片段模板与词条：两处说的是同一件事（同一份 ListStalePages），
// 各写一份模板的下场是同一个现象在两个页面上有两种样子。
//
// 取不到数据一律只给状态码：缺 data-drawer-fragment / 缺列的片段会被 drawer.js 的
// fragmentRoot 校验判非法，用户看到的同样是「加载失败」，但那时还多花了一次渲染。
func (h *blockPageHandle) BlocksStaleDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.pages == nil {
		c.Status(http.StatusNotFound)
		return
	}
	tr := shell.TranslateFor(c)
	res, err := h.pages.ListStalePages(c.Request.Context(), &pagecontract.StalePageListReq{
		Limit:      blockStalePageLimit,
		Descending: true,
	})
	if err != nil {
		logger.Scene("block").Error(err, "读取全站待重建清单失败（抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	if res == nil {
		logger.Scene("block").Warn("全站待重建清单返回空结果（契约实现异常，抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	rows := make([]gin.H, 0, len(res.Pages))
	projects := make(map[string]struct{}, 2)
	for i := range res.Pages {
		projects[res.Pages[i].ProjectID] = struct{}{}
		rows = append(rows, gin.H{
			"ID":          res.Pages[i].ID,
			"Path":        res.Pages[i].Path,
			"ProjectName": res.Pages[i].ProjectName,
			"Published":   res.Pages[i].Published,
			// 失败痕迹（迁移 474）：非空说明这页不是「还没轮到」，而是重建失败过。
			"FailedNote": rebuildFailureNote(tr, res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt),
		})
	}
	c.HTML(http.StatusOK, "admin/partials/stale_pages_drawer.html", shell.Prepare(c, gin.H{
		"Rows": rows, "Total": res.Total, "Truncated": res.Truncated,
		"Limit": blockStalePageLimit, "MultiProject": len(projects) > 1,
	}))
}

// rebuildFailureNote 组装「最近一次自动重建失败」的一句文案；没有失败痕迹时返回空串。
//
// 阶段与时刻来自迁移 474 落在 pages 上的两列；**不含错误原文**（后台页面不得直出内部错误，
// 原文只进结构化日志，带 page_id 可定位）。与 /admin/articles、/admin/blocks 上同名函数
// 是三份：各自在自己的模块包内（跨模块共用要走契约，而这是纯展示装配）。
// 三处文案与阶段取值必须一致，改一处请同步另两处。
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
