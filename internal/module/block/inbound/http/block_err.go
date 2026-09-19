package blockhttp

// block_err.go — 全局块页**读侧**回执文案（?err= / ?done=）的收口。
//
// 写侧早就是把错误收敛过的（blockErrText / shell.BulkIDsFacingText），但列表页把它
// **原样**从 query 读回来渲染（BlocksList 的 `Err: strings.TrimSpace(c.Query("err"))`）：
// 任何人手拼一个 /admin/blocks?err=任意文案 就能在页面上塞一条顶着「上一次操作未完成」
// 样式的伪造消息（Jet 已做 HTML 转义，所以不是 XSS —— 问题是「看起来像系统说的话」）。
// 查询参数与响应体、模板数据一样**不是可信边界**。
//
// 收口形状照抄订单页的样板（order_page_handle.go:169）：
//
//	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), 判定)
//
// 判定 = shell.FacingNotice（受控形状：逐字相等 / 数字归一相等 / 文案 + "：" + 定位信息），
// 候选文案由本页**自己的**白名单派生（blockErrSentinels + shell 的批量上限模板 +
// 批量结论文案模板）。未命中：?err= 落 shell.PageInternalText(c)（页面显示「系统内部错误」
// 而不是什么都不显示），?done= 落空串（成功提示未命中的唯一正确表现是「没有这条提示」）。

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/internal/web/shell"
)

// blockRefDetailMaxBytes 引用明细允许占用的字节上限。
//
// 整串（受控文案 + "：" + 明细）要能通过读侧的形状判定（shell.NoticeMaxBytes = 512），
// 所以明细自己必须封顶 —— 超长的 ?err= 会被读侧判成伪造消息，页面反而显示
// 「系统内部错误」，把一次**有定位信息**的拒绝变成一句看不懂的兜底话。
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
			tail = " 等 " + strconv.Itoa(len(locs)) + " 处"
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

// blockBulkResultTemplates 批量删除的结论文案模板（%d 是计数字段）。
//
// **写侧与读侧共用这一份字面量**：写侧 blocksBulkDeleteResult 用它 Sprintf 出文案，
// 读侧 blockNoticeTexts 用它（经 shell.NoticeTemplate 归一）判定 URL 回显。
// 各写一份的后果是静默的 —— 写侧改了措辞，读侧白名单不再命中，运营看到的
// 就从「已删除 3 个块。」退化成「系统内部错误」。
var blockBulkResultTemplates = []string{
	"没有选中任何块，列表未改动。",
	"已删除 %d 个块。",
	"%d 个块都未能删除，列表未改动。",
	"已删除 %d 个，%d 个未能删除（被引用的全局块需先解除引用）。",
}

// blockRefErrText 块删除被拒时的页面文案：受控文案 + "：" + 引用明细。
//
// 明细是可定位的数据（哪一类引用、哪些实体），受控文案是白名单的来源 ——
// 形状「候选文案 + ：+ 定位」正是 shell.FacingNotice 的形态 3，读侧才能原样放行；
// 长度封顶同样必须由写侧做（读侧超 512 字节一律判伪造，页面会退化成「系统内部错误」）。
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

// blockNoticeTexts 本页可以原样展示的回执文案（当前语言）。
//
// 三类来源，与写侧的取值一一对应：
//  1. blockErrSentinels 的译文（blockErrText 的产物）；
//  2. 归口文案（tr(shell.MsgInternalError, ...) —— blockErrText 未命中时给的就是它）
//     与 shell 的批量上限提示模板（shell.BulkIDsFacingText 的产物）；
//  3. 批量结论文案模板（数字归一后与实际文案可比）。
func blockNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, len(blockErrSentinels)+len(blockBulkResultTemplates)+2)
	for _, sentinel := range blockErrSentinels {
		key := sentinel.Error()
		out = append(out, tr(key, key))
	}
	out = append(out, tr(shell.MsgInternalError, blockErrInternalFallback))
	out = append(out, shell.BulkIDsNoticeTemplate(c))
	for _, tpl := range blockBulkResultTemplates {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	return out
}

// blockPageErr 列表页 ?err= 的统一出口（写侧两条通道都走它判定）。
func blockPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, blockNoticeTexts(c))
	})
}

// blockPageDone 列表页 ?done= 的统一出口（成功提示：未命中落空串）。
func blockPageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return shell.FacingNotice(raw, blockNoticeTexts(c))
	})
}
