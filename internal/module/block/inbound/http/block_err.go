package blockhttp

// block_err.go — 全局块页写动作的**出口归口**（错误文案三件套 + 整页提示）。
//
// 三件套（AGENTS.md §响应与错误处理）：
//
//	① 白名单 —— blockErrSentinels（= blockcontract 的 sentinel，值即 i18n key，见 block_page.go）；
//	② 归口文案 —— 未命中时返回 shell.MsgInternalError（可翻译 key + 中文兜底）；
//	③ 结构化日志 —— 原文只进日志，带 user_id / path（在各写路径的调用点记）。
//
// 传输通道：写动作的结论由 shell.RenderJump 渲染成整页提示（对应 ThinkPHP 的
// success() / error()），**不再**经 302 + `?err=` / `?done=` 回带列表页。
//
// **读侧（?err= / ?done= 的受控文案集合与判定）已整批删除**：那条通道的代价是每个模块
// 都要维护一份「受控文案 + 数字归一模板」来证明提示出自本仓（blockNoticeTexts /
// blockPageErr / blockPageDone 就是那套），而查询参数不是可信边界。文案走响应体之后，
// 那套判定随之不需要了。
//
// 本文件剩下的都是**写侧**：把 service 的错误 / 计数结论渲染成可展示的成品文案，
// 以及回跳地址（shell.BackPath 从表单 action 的 query 读回筛选上下文）。

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
)

// blockRefDetailMaxBytes 引用明细允许占用的字节上限（**展示封顶**）。
//
// 提示页正文太长会挤掉「立即前往」的链接与其它提示；这里按可读性封顶。
// （原先是按读侧 shell.NoticeMaxBytes 的 URL 形状判定反推出来的，那条判定已删除。）
const blockRefDetailMaxBytes = 420

// blockUsageKindKey 引用类别 → i18n key（真文案在 sys_i18n，迁移 297）。
//
// switch 穷举而不是拼字符串（"MsgBlockUsage" + ...）：类别是枚举，新增取值时
// 漏改这里会静默退回渲染成英文枚举值（"page_structure"），而它看起来"像"一个正常文案。
func blockUsageKindKey(kind blockcontract.BlockUsageKind) string {
	switch kind {
	case blockcontract.UsageKindPageDocument:
		return blockenums.MsgBlockUsagePageDocument
	case blockcontract.UsageKindPageStructure:
		return blockenums.MsgBlockUsagePageStructure
	case blockcontract.UsageKindPageRevision:
		return blockenums.MsgBlockUsagePageRevision
	case blockcontract.UsageKindThemeSlot:
		return blockenums.MsgBlockUsageThemeSlot
	case blockcontract.UsageKindBlockDocument:
		return blockenums.MsgBlockUsageBlockDocument
	case blockcontract.UsageKindContentTemplate:
		return blockenums.MsgBlockUsageContentTemplate
	case blockcontract.UsageKindPresentationInstance:
		return blockenums.MsgBlockUsagePresentationInstance
	default:
		return string(kind)
	}
}

// blockRefUsageText 把拒绝错误里的引用明细渲染成当前语言的定位串（无明细返回空串）。
//
// 同类引用合并成一行（类别 + 至多 3 个定位 + 「等 N 处」）：块引用常常一次命中同一类
// 的几十个页面，逐条铺开既超长又不可读 —— 而这条文案的用途恰恰是「让人立刻知道去哪解除引用」。
func blockRefUsageText(c *gin.Context, err error) string {
	usages := blockcontract.BlockUsages(err)
	if len(usages) == 0 {
		return ""
	}
	tr := shell.TranslateFor(c)
	order := make([]blockcontract.BlockUsageKind, 0, len(usages))
	grouped := map[blockcontract.BlockUsageKind][]string{}
	for _, u := range usages {
		loc := strings.TrimSpace(u.Label)
		if loc == "" {
			loc = strings.TrimSpace(u.EntityID)
		}
		if detail := strings.TrimSpace(u.Detail); detail != "" {
			loc = loc + "（" + detail + "）"
		}
		if _, ok := grouped[u.Kind]; !ok {
			order = append(order, u.Kind)
		}
		grouped[u.Kind] = append(grouped[u.Kind], loc)
	}
	parts := make([]string, 0, len(order))
	for _, kind := range order {
		key := blockUsageKindKey(kind)
		locs := grouped[kind]
		shown := locs
		tail := ""
		if len(locs) > 3 {
			shown = locs[:3]
			tail = i18n.FillTranslate(tr, blockenums.UsageMore, " 等 {count} 处",
				map[string]string{"count": strconv.Itoa(len(locs))})
		}
		parts = append(parts, tr(key, key)+"："+strings.Join(shown, "、")+tail)
	}
	return truncateRunes(strings.Join(parts, "；"), blockRefDetailMaxBytes)
}

// truncateRunes 按字节上限截断（不切断多字节字符），超出时以「…」收尾。
func truncateRunes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// blockBulkResultTemplates 批量删除的结论文案模板（{count} 等是命名占位符）。
//
// 单条删除的成功回执也复用这里的 allDeleted（count=1），所以「删一个块」与「批量删一个块」
// 说的是同一句话 —— 两处各写一句的下场是同一个动作在两种入口下措辞不同。
var blockBulkResultTemplates = []blockText{
	{"admin.blocks.bulkResult.noneSelected", "没有选中任何块，列表未改动。"},
	{"admin.blocks.bulkResult.allDeleted", "已删除 {count} 个块。"},
	{"admin.blocks.bulkResult.allSkipped", "{count} 个块都未能删除，列表未改动。"},
	{"admin.blocks.bulkResult.partial", "已删除 {deleted} 个，{skipped} 个未能删除（被引用的全局块需先解除引用）。"},
}

// blockBulkFilled 把批量结论文案模板填成成品句子（占位符命名形态，见 pkg/i18n/placeholder.go）。
func blockBulkFilled(c *gin.Context, t blockText, params map[string]string) string {
	return i18n.FillTranslate(shell.TranslateFor(c), t.Key, t.Fallback, params)
}

// blockRefErrText 块删除被拒时的页面文案：受控文案 + "：" + 引用明细。
//
// 明细是可定位的数据（哪一类引用、哪些实体），受控文案来自白名单；整串按展示上限封顶。
func blockRefErrText(c *gin.Context, err error) string {
	msg := blockErrText(c, err)
	if detail := blockRefUsageText(c, err); detail != "" {
		msg = truncateRunes(msg+"："+detail, shell.NoticeMaxBytes-1)
	}
	return msg
}

// blockRefSkipDetail 批量删除中单个块的跳过原因（块名 + 受控原因 + 引用明细）。
//
// 带块名而不是只带 id：批量列表里用户看的是「名字」，一串 uuid 定位不了任何东西。
// 名字取不到时由调用方退化成 id 前缀。
func blockRefSkipDetail(c *gin.Context, name string, err error) string {
	reason := blockErrText(c, err)
	if detail := blockRefUsageText(c, err); detail != "" {
		reason = reason + "：" + detail
	}
	if strings.TrimSpace(name) == "" {
		return reason
	}
	return strings.TrimSpace(name) + "：" + reason
}

// blockListPath 全局块列表页路径（写动作失败时的回跳目标）。
const blockListPath = "/admin/blocks"

// blockBackText 提示页那个链接的文字（复用页面标题词条，不新增全站词条）。
func blockBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(blockenums.MsgBlockTitle, "全局块")
}

// blockListBack 回列表页的回跳地址：从**本次请求的 query** 读回工程筛选。
//
// 上下文随表单 action 的 query 一起提交（`action="/admin/blocks/delete?project=…"`），
// 服务端按调用点显式列出的键读回来 —— 不再从隐藏域读整串返回 URL，也不再由 Go 拼 ?err=。
func blockListBack(c *gin.Context) string {
	return shell.BackPath(c, blockListPath, "project")
}

// blockPageJump 页面写动作的统一出口：整页提示（回列表页）。
//
// 取代原先的 303 + `?err=` / `?done=`：那条通道要求读侧再判一次「这条提示是不是本仓给的」
// （blockNoticeTexts 的候选集合），而查询参数不是可信边界。现在文案走响应体，读侧判定随之删除。
//
// 提示文本必须**已过本模块白名单 / 已归口**（blockErrText / blockRefErrText /
// shell.BulkIDsFacingText / blockBulkFilled 的产物），原文只进日志 ——
// 换个页面呈现不等于可以把 err.Error() 铺在页面上。
//
// 失败不自动跳转（Seconds=0）：运营要看清楚原因。成功 1 秒后自动回列表页
// （与 sysconfig / plugin / order 同一取舍）。
func blockPageJump(c *gin.Context, ok bool, msg, back string) {
	blockJump(c, ok, msg, back, blockBackText(c))
}

// blockJump 带自定义链接文字的提示页出口（新建成功要跳工作台，链接文字不是「全局块」）。
func blockJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}
