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
	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
)

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
	"已删除 %d 个，%d 个未能删除（被页面引用的全局块需先解除引用）。",
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
