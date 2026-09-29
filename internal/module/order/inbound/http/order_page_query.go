package orderhttp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderenums "go_wp/internal/module/order/enums"

	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// order_page_query.go - 订单管理页的查询参数解析与对外文案出口。

// orderListWindow 解析列表窗口：以 page/limit 为准（与分页组件一致），
// 兼容只有 offset 的链接（offset 换算成页码，每页条数取同一个 limit）。
func orderListWindow(c *gin.Context) (page, limit int) {
	page, limit = shell.PageParams(c)
	if strings.TrimSpace(c.Query("page")) != "" {
		return page, limit
	}
	if offset, err := strconv.Atoi(strings.TrimSpace(c.Query("offset"))); err == nil && offset > 0 {
		page = offset/limit + 1
	}
	return page, limit
}

// orderQueryID 解析 orderId 查询参数（非法即 0 = 不渲染详情块）。
func orderQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// orderPageFacingText 页面路径的提示取词出口（白名单判定 + **取当前语言的译文**）。
//
// 为什么要多这一层：orderenums 里的值分两种形态 —— 多数是 sys_i18n 的 item_key
// （order.err.orderNotFound），少数是中文常量（ErrInternal = "操作失败，请稍后重试"）。
// orderFacingText 只做白名单判定、原样返回命中的那个值，因为 **API 出口需要 key**：
// order_handle.go 把 key 交给 pkg/response 去翻译。
//
// 但页面路径是**直接渲染**的文本（模板里就是 {{.Err}} / {{.Ok}}，不经过 response 的
// translate）。实测（2026-09）：/admin/orders?err=order.err.orderNotFound 的提示条上
// 显示的就是那一串裸 key —— 同一份白名单在 API 出口是 key、在页面出口也是 key，
// 于是「订单不存在」这句现成的译文永远到不了运营眼前。
//
// 判定仍只有一份（orderFacingText），本函数只负责把命中的值按当前语言取词：
// 命中的是 item_key 就出译文（词条缺失时回落 key 本身，与之前的行为一致）；
// 命中的是中文常量时按 key 查不到词条，取词函数据 fallback 原样返回 —— 两种形态都对。
func orderPageFacingText(c *gin.Context) func(string) string {
	return func(raw string) string {
		hit := orderFacingText(raw)
		if hit == "" {
			return ""
		}
		return shell.TranslateFor(c)(hit, hit)
	}
}

// orderFacingError 把订单模块的错误转成可展示文案（本模块页面路径的**错误文案三件套**出口）。
//
// 订单模块的业务错误本来就是给运营看的中文（「库存不足，无法下单」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此只放行模块自己声明的
// orderenums.UserFacingMessages 白名单，其余一律落到统一提示。
//
// 三件套在这里的落法：
//
//	① 白名单 —— orderFacingText（enums.UserFacingMessages，与 API 出口共用同一份）；
//	② 归口文案 —— shell.PageInternalText(c)：**当前语言的译文**，不是裸 key
//	   （列表页装载失败时它进的是模板 {{.Err}}，那是直接渲染的文本，不经过 response 的 translate；
//	   原先那条路径是 `c.String(500, orderenums.ErrInternal)` —— 硬编码中文，英文界面照旧显示中文）；
//	   命中白名单的那一支同样取译文（orderPageFacingText），否则页面显示的是裸 key；
//	③ 结构化日志 —— **只有落到归口文案那一支才记**：命中白名单的是业务错误（预期内的用户输入问题），
//	   记 ERROR 只会淹没真正的故障；未命中的原文（表名 / SQL 片段 / 驱动前缀）只进日志、绝不进响应。
func orderFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := orderPageFacingText(c)(err.Error()); msg != "" {
		return msg
	}
	logger.Scene("order-page").With("path", c.Request.URL.Path).Error(err, "订单后台页处理失败")
	return shell.PageInternalText(c)
}

// orderFacingText 白名单校验：命中返回原文，未命中返回空串。
func orderFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	for _, allowed := range orderenums.UserFacingMessages {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

// —— 批量动作的结论（?done=）——
//
// 批量动作的结论是带计数的整句（「已发货 3 个订单，跳过 2 个（状态不允许或已不存在）。」），
// 过不了 ?err= / ?ok= 的文案白名单，所以三个列表页（订单 / 退货申请 / 优惠码）单独走 ?done=。
// 它此前是 `"Done": strings.TrimSpace(c.Query("done"))` 原样进模板：手拼一个
// /admin/orders?done=任意文案 就能往页面上塞一条顶着「成功」样式的伪造消息
//（Jet 已做 HTML 转义，所以不是 XSS；问题是「看起来像系统说的话」）。
//
// 判据与其它模块的读侧出口一致：与写侧**共用同一份模板字面量**，整体匹配（数字归一后相等，
// 计数因此可以变），未命中落空串 —— 成功提示没有「必须说点什么」的语义。

// orderBulkText 批量结论文案的一条模板 / 动作词（i18n key + 中文原文）。
//
// **key 与中文原文只有这一份**：写侧 bulkSummary 拿它 Sprintf 出整句，读侧 orderDoneTexts
// 拿**同一个值**、经同一处取词（orderBulkTextOf）得到当前语言模板再归一比对。
// 读侧另抄一份中文的后果是静默的 —— 写侧改了措辞候选就失配，页面上变成「没有这条提示」。
type orderBulkText struct{ key, fallback string }

// orderBulkTextOf 取一条批量结论文案的当前语言文本（写侧与读侧**共用这一个取法**）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（本文件的模板一律只允许 %s，数字先经
// strconv.Itoa）：否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。
func orderBulkTextOf(c *gin.Context, t orderBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// 批量动作结论文案的四个分支：写侧 bulkSummary 用它 Sprintf 出文案，
// 读侧 orderDoneTexts 用同一批模板（当前语言）经 shell.NoticeTemplate 归一后比对。
var (
	orderBulkNothingSelected = orderBulkText{orderenums.BulkNoneSelected, "没有勾选任何%s。"}
	orderBulkAllDone         = orderBulkText{orderenums.BulkAllDone, "%s %s 个%s。"}
	orderBulkAllSkipped      = orderBulkText{orderenums.BulkAllSkipped, "0 个%s%s，%s 个被跳过（状态不允许或已不存在）。"}
	orderBulkPartial         = orderBulkText{orderenums.BulkPartial, "%s %s 个%s，跳过 %s 个（状态不允许或已不存在）。"}
)

// 批量动作的动词（i18n key + 中文原文）：**动词也 key 化**，否则英文界面上会出现
// 「Moved 3 个订单」这种中英混排 —— 语序不同，不能只翻模板。
var (
	orderBulkVerbFlowed    = orderBulkText{orderenums.BulkVerbFlowed, "已流转"}
	orderBulkVerbCancelled = orderBulkText{orderenums.BulkVerbCancelled, "已取消"}
	orderBulkVerbApproved  = orderBulkText{orderenums.BulkVerbApproved, "已同意"}
	orderBulkVerbRejected  = orderBulkText{orderenums.BulkVerbRejected, "已拒绝"}
	orderBulkVerbDeleted   = orderBulkText{orderenums.BulkVerbDeleted, "已删除"}
	orderBulkVerbDisabled  = orderBulkText{orderenums.BulkVerbDisabled, "已停用"}
	orderBulkVerbEnabled   = orderBulkText{orderenums.BulkVerbEnabled, "已启用"}
)

// 批量动作的名词（i18n key + 中文原文）。
var (
	orderBulkNounOrder  = orderBulkText{orderenums.BulkNounOrder, "订单"}
	orderBulkNounReturn = orderBulkText{orderenums.BulkNounReturn, "退货申请"}
	orderBulkNounCoupon = orderBulkText{orderenums.BulkNounCoupon, "优惠码"}
)

// orderBulkActions 批量动作的（动词，名词）对（= 各页 bulkSummary 的实参）。
//
// 写侧每个调用点都要在这里有一行：漏登记的症状是该页「批量操作完成了却没有回执」
// （可见、不致命），而不是伪造面。
var orderBulkActions = [][2]orderBulkText{
	{orderBulkVerbFlowed, orderBulkNounOrder},
	{orderBulkVerbCancelled, orderBulkNounOrder},
	{orderBulkVerbApproved, orderBulkNounReturn},
	{orderBulkVerbRejected, orderBulkNounReturn},
	{orderBulkVerbDeleted, orderBulkNounCoupon},
	{orderBulkVerbDisabled, orderBulkNounCoupon},
	{orderBulkVerbEnabled, orderBulkNounCoupon},
}

// orderBulkExtraNotices 不走 bulkSummary、但也进 ?done= 的本模块自造文案。
var orderBulkExtraNotices = []orderBulkText{
	couponBulkTargetInvalidText,
	orderBulkCancelReasonRequired,
	returnBulkRejectReasonRequired,
}

// 两条「缺必填参数、整批不处理」的提示（与批量结论同一个通道，写读共用同一份模板）。
var (
	// orderBulkCancelReasonRequired 批量取消订单缺原因。
	orderBulkCancelReasonRequired = orderBulkText{orderenums.BulkCancelReasonRequired,
		"批量取消需要先填原因（表单里的备注框），本次没有处理任何订单。"}
	// returnBulkRejectReasonRequired 批量拒绝退货缺理由。
	returnBulkRejectReasonRequired = orderBulkText{orderenums.BulkReturnRejectReasonRequired,
		"批量拒绝需要先填理由（表单里的备注框），本次没有处理任何退货申请。"}
)

// orderDoneTexts 列表页 ?done= 可以原样展示的受控文案（**当前语言**，数字归一后可比）。
func orderDoneTexts(c *gin.Context) []string {
	out := make([]string, 0, len(orderBulkActions)*4+len(orderBulkExtraNotices))
	for _, action := range orderBulkActions {
		verb, noun := orderBulkTextOf(c, action[0]), orderBulkTextOf(c, action[1])
		out = append(out,
			shell.NoticeTemplate(fmt.Sprintf(orderBulkTextOf(c, orderBulkNothingSelected), noun)),
			shell.NoticeTemplate(fmt.Sprintf(orderBulkTextOf(c, orderBulkAllDone), verb, "0", noun)),
			shell.NoticeTemplate(fmt.Sprintf(orderBulkTextOf(c, orderBulkAllSkipped), noun, verb, "0")),
			shell.NoticeTemplate(fmt.Sprintf(orderBulkTextOf(c, orderBulkPartial), verb, "0", noun, "0")),
		)
	}
	for _, extra := range orderBulkExtraNotices {
		out = append(out, orderBulkTextOf(c, extra))
	}
	return out
}

// orderPageDone 列表页 ?done= 的受控出口（成功提示：未命中落空串）。
//
// 判定用 shell.FacingNotice 的**整体**匹配（逐字 / 数字归一 / 「候选 + ：」），不是
// strings.Contains —— 后者只要夹带一段已知文案就能往页面上塞任意前缀 / 后缀。
func orderPageDone(c *gin.Context, raw string) string {
	return shell.FacingQueryText(raw, "", func(msg string) string {
		return shell.FacingNotice(msg, orderDoneTexts(c))
	})
}

// orderBulkIDsText shell.BulkIDs 的失败文案（单次提交的 id 超过上限）。
//
// 只是转调 shell 的受控出口：超限错误是 shell 的类型（shell.BulkIDsError），
// 「一次最多操作 N 项，当前 M 项，请分批进行」按当前语言生成，其中**当前 M 项**
// （去重后的条数）只有 shell 知道 —— 本模块不再用 shell.MaxBulkIDs 重算一遍：
// 那是第二份真相，而且必然丢掉 Count（旧的实现正是如此）。
// 判据也不再是「文案来自哪里」而是类型：出口只认 sentinel，认不出就回落归口文案。
// 留痕（哪个页面、哪个操作人触发）由 shell 的出口统一记日志。
func orderBulkIDsText(c *gin.Context, err error) string {
	return shell.BulkIDsFacingText(c, err)
}

// firstNonEmpty 取第一个非空字符串（回显拼接用：业务错误优先、内部提示兜底）。
//
// 页面不能把两处错误都摊开 —— ?err= 只有一个位置，先到的那条才是用户当下要看的。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
