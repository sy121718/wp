package orderhttp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderenums "go_wp/internal/module/order/enums"

	"go_wp/internal/web/shell"
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

// orderFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「库存不足，无法下单」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此只放行模块自己声明的
// orderenums.UserFacingMessages 白名单，其余一律落到统一提示。
func orderFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := orderFacingText(err.Error()); msg != "" {
		return msg
	}
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

// 批量动作结论文案的四个分支：写侧 bulkSummary 用它 Sprintf 出文案，
// 读侧 orderDoneTexts 用同一批字面量经 shell.NoticeTemplate 归一后比对。
// 各写一份的后果是静默的 —— 写侧改了措辞，读侧候选不再命中，运营看到的是「没有这条提示」。
const (
	orderBulkNothingSelected = "没有勾选任何%s。"
	orderBulkAllDone         = "%s %d 个%s。"
	orderBulkAllSkipped      = "0 个%s%s，%d 个被跳过（状态不允许或已不存在）。"
	orderBulkPartial         = "%s %d 个%s，跳过 %d 个（状态不允许或已不存在）。"
)

// orderBulkActions 批量动作的（动词，名词）对（= 各页 bulkSummary 的实参）。
//
// 写侧每个调用点都要在这里有一行：漏登记的症状是该页「批量操作完成了却没有回执」
// （可见、不致命），而不是伪造面。
var orderBulkActions = [][2]string{
	{"已流转", "订单"},
	{"已取消", "订单"},
	{"已同意", "退货申请"},
	{"已拒绝", "退货申请"},
	{"已删除", "优惠码"},
	{"已停用", "优惠码"},
	{"已启用", "优惠码"},
}

// orderBulkExtraNotices 不走 bulkSummary、但也进 ?done= 的本模块自造文案。
var orderBulkExtraNotices = []string{couponBulkTargetInvalidText}

// orderDoneTexts 列表页 ?done= 可以原样展示的受控文案（数字归一后可比）。
func orderDoneTexts() []string {
	out := make([]string, 0, len(orderBulkActions)*4+len(orderBulkExtraNotices))
	for _, action := range orderBulkActions {
		verb, noun := action[0], action[1]
		out = append(out,
			shell.NoticeTemplate(fmt.Sprintf(orderBulkNothingSelected, noun)),
			shell.NoticeTemplate(fmt.Sprintf(orderBulkAllDone, verb, 0, noun)),
			shell.NoticeTemplate(fmt.Sprintf(orderBulkAllSkipped, noun, verb, 0)),
			shell.NoticeTemplate(fmt.Sprintf(orderBulkPartial, verb, 0, noun, 0)),
		)
	}
	return append(out, orderBulkExtraNotices...)
}

// orderPageDone 列表页 ?done= 的受控出口（成功提示：未命中落空串）。
//
// 判定用 shell.FacingNotice 的**整体**匹配（逐字 / 数字归一 / 「候选 + ：」），不是
// strings.Contains —— 后者只要夹带一段已知文案就能往页面上塞任意前缀 / 后缀。
func orderPageDone(raw string) string {
	return shell.FacingQueryText(raw, "", func(msg string) string {
		return shell.FacingNotice(msg, orderDoneTexts())
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
