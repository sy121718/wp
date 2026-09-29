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
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// blockStalePageLimit 影响面清单最多列出的页面数（超出只计数，页面上显式说明）。
const blockStalePageLimit = 30

// blockStaleImpact 汇总当前所有工程里「待重建」的页面（只读）。
//
// pages 未注入（装配层尚未把 page 契约传进来）时返回 Available=false + 明确说明，
// 而不是给一份「0 个待重建」的假结论 —— 两者对运营的含义完全不同。
func (h *blockPageHandle) blockStaleImpact(tr func(key, fallback string) string, ctx context.Context) gin.H {
	if h == nil || h.pages == nil || h.projects == nil {
		return gin.H{
			"Available": false, "Pages": []gin.H{}, "Total": 0, "Truncated": false,
			"Limit": blockStalePageLimit,
			"Hint":  tr(blockenums.ImpactUnavailableNoPageCapability, "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。"),
		}
	}
	projects, err := h.projects.List(ctx)
	if err != nil {
		logger.Scene("block").Error(err, "读取工程列表失败，待重建影响面本次不可用")
		return gin.H{
			"Available": false, "Pages": []gin.H{}, "Total": 0, "Truncated": false,
			"Limit": blockStalePageLimit,
			"Hint":  tr(blockenums.ImpactUnavailableProjectReadFailed, "读取站点工程失败，本次无法统计待重建影响面。"),
		}
	}
	pages := make([]gin.H, 0, blockStalePageLimit)
	total, truncated := 0, false
	for i := range projects {
		rows, lerr := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: projects[i].ID})
		if lerr != nil {
			// 单个工程读失败不整批失败，但必须留痕 —— 静默跳过会让影响面少算。
			logger.Scene("block").With("project_id", projects[i].ID).Error(lerr, "读取工程页面失败，影响面可能少算")
			continue
		}
		for j := range rows {
			if !rows[j].Stale {
				continue
			}
			total++
			if len(pages) >= blockStalePageLimit {
				truncated = true
				continue
			}
			pages = append(pages, gin.H{
				"ID":          rows[j].ID,
				"Path":        blockStalePagePath(rows[j]),
				"ProjectName": projects[i].Name,
			})
		}
	}
	return gin.H{
		"Available": true, "Pages": pages, "Total": total, "Truncated": truncated,
		"Limit": blockStalePageLimit, "Hint": "",
	}
}

// blockStalePagePath 页面的可读标识：优先已上线路径，其次草稿路径，都没有时退回 id。
func blockStalePagePath(p pagecontract.PageResp) string {
	if p.ActivePath != nil && strings.TrimSpace(*p.ActivePath) != "" {
		return *p.ActivePath
	}
	if strings.TrimSpace(p.DraftPath) != "" {
		return p.DraftPath
	}
	return p.ID
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
