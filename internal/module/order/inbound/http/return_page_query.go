package orderhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
)

// return_page_query.go - 退货入库页的表单与查询取值、对外文案出口。

// returnFacingPageText 页面路径的提示取词出口（白名单判定 + **取当前语言的译文**）。
//
// 与 order 页同因（见 order_page_query.go 的 orderPageFacingText）：白名单里存的是
// item_key（order.msg.returnReceived / order.err.returnNotFound …），而页面上的
// {{.Ok}} / {{.Err}} 是直接渲染的文本、不经过 pkg/response 的 translate ——
// 只放行 key 的话页面显示的就是那串 key 本身。
//
// 白名单判定仍只有一份（returnFacingText），本函数只把命中的值按当前语言取词：
// 命中的是 item_key 就出译文；命中的是本页自造的中文常量（returnIDInvalidText）时
// 按 key 查不到词条，取词函数据 fallback 原样返回。
func returnFacingPageText(c *gin.Context) func(string) string {
	return func(raw string) string {
		hit := returnFacingText(raw)
		if hit == "" {
			return ""
		}
		return shell.TranslateFor(c)(hit, hit)
	}
}

// returnFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「退货数量超过可退数量」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此先放行模块自己声明的
// orderenums.UserFacingMessages（orderFacingText），再放行本页登记的后台审核侧文案
// （returnFacingExtras），其余一律落到统一提示。
func returnFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := returnFacingPageText(c)(err.Error()); msg != "" {
		return msg
	}
	return shell.PageInternalText(c)
}

// returnFacingText 白名单校验：命中返回原文，未命中返回空串。
func returnFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if hit := orderFacingText(msg); hit != "" {
		return hit
	}
	for _, allowed := range returnFacingExtras {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

// returnFacingQueryText 成功提示的回显（?ok=）：同样过白名单，未命中落空串 ——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
// 命中项按当前语言取词（returnFacingPageText）：写侧回带的是 item_key。
func returnFacingQueryText(c *gin.Context, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return returnFacingPageText(c)(raw)
}

// returnFormBool 表单里的布尔勾选（checkbox 勾上才提交值，没勾就整键缺失）。
func returnFormBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// returnStatusBadge 状态 → 徽章样式（认不出的状态给中性徽章，不猜颜色）。
func returnStatusBadge(status string) string {
	for _, view := range returnStatusViews {
		if view.Value == status {
			return view.Badge
		}
	}
	return "badge-mute"
}

// returnStatusLabel 状态 → 中文标签（筛选项回显用；未知值原样返回）。
func returnStatusLabel(status string) string {
	for _, view := range returnStatusViews {
		if view.Value == status {
			return view.Label
		}
	}
	if strings.TrimSpace(status) == "" {
		return "全部"
	}
	return status
}

// returnStatusNote 当前状态该做什么 / 为什么没有按钮 —— 一屏内有且只有一句说明。
//
// 返回 (词条 key, 中文兜底)：**文案源仍是这里这一份**（key 与中文原文绑在一起），
// 但取值交给模板的 t(key, fallback) —— 原先是纯中文常量，英文界面上整块露中文。
// 调用点（returnDetailView）把两者一起塞进模板数据，模板用 detail.NoteKey/Note 取词。
func returnStatusNote(status string) (key, fallback string) {
	switch status {
	case returnStatusRequested:
		return "admin.returns.note.requested", "客户已提交，等待审核：同意后可以勾「立即完成入库 + 退款」（货已经在手上时），也可以等货到仓库再点确认收货。"
	case returnStatusApproved:
		return "admin.returns.note.approved", "已同意，等待收货：货到仓库后点下面的「确认收货并退货」—— 它会先入库、入库成功后立刻退款。"
	case returnStatusReceived:
		return "admin.returns.note.received", "货已入库，但退款没做完：点下面的「补退款」重试退款即可（入库那一步已完成，不会重复加库存）。"
	case returnStatusCompleted:
		return "admin.returns.note.completed", "这单已完成：货已入库、款已退。没有可执行的操作。"
	case returnStatusRejected:
		return "admin.returns.note.rejected", "这单已被拒绝（终态）。客户如有异议，需要他重新提交退货申请。"
	case returnStatusCancelled:
		return "admin.returns.note.cancelled", "客户已撤销这单申请（终态）。没有可执行的操作。"
	default:
		return "", ""
	}
}

// returnOrderIDText orderId 的展示文本：0 → 空串（模板据此决定要不要显示筛选提示）。
func returnOrderIDText(orderID uint64) string {
	if orderID == 0 {
		return ""
	}
	return strconv.FormatUint(orderID, 10)
}
