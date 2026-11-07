package orderhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	orderservice "go_wp/internal/module/order/service"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const orderErrScene = "order"

// orderWriteFallback 是纯成功消息的缺词兜底：只作为词库缺失时的最后一句，
// 不能把裸 key 交给界面（与 membership 的同类约定一致）。
var orderWriteFallback = map[string]string{
	"ordSaved":        "已保存。",
	"ordStChanged":    "订单状态已流转。",
	"ordCancelled":    "订单已取消，未发货部分库存已归还。",
	"ordRefunded":     "订单已退款。",
	"ordNoteSaved":    "后台备注已保存。",
	"ordCpDeleted":    "优惠码已删除。",
	"ordShipped":      "已发货，库存已按明细扣减。",
	"ordCarrierSaved": "承运商已保存。",
	"ordRetApproved":  "退货申请已通过。",
	"ordRetRejected":  "退货申请已拒绝。",
	"ordRetReceived":  "退货已收货，库存与退款已处理。",
}

// orderWriteOK 输出纯成功消息：只有一句结论，走共享成功出口。
func orderWriteOK(c *gin.Context, key string) {
	pageview.SuccessMessage(c, key, orderWriteFallback[key])
}

// orderWriteResultNav 渲染带实体链接的结论片段。
// 链接由调用方按实体事实算好，前端只按发起模板声明的 data-write-nav 导航；
// 纯消息不走这里（共用共享成功出口）。
func orderWriteResultNav(c *gin.Context, key, href string) {
	c.HTML(http.StatusOK, "admin/order/order_write_result.html", gin.H{"Key": key, "Href": href})
}

// orderWriteBadInput 把原生 binder / 表单校验的失败交给统一失败出口。
func orderWriteBadInput(c *gin.Context, key string) {
	orderWriteFail(c, facing.WithStatus(facing.New(key), http.StatusBadRequest))
}

// orderWriteFail 是 order 写端点的 HTML 失败出口：状态码取错误事实，
// 文案一律由失败模板按请求语言取词，不在这里产出成品句。
func orderWriteFail(c *gin.Context, err error) {
	pageview.FailStatus(c, response.ErrorStatus(err), orderErrScene, err)
}

// orderBulkFail 是批量写被整批拒绝时的失败片段：逐条原因由 Jet 逐条取词。
func orderBulkFail(c *gin.Context, items []facing.BlockedItem) {
	pageview.FailStatus(c, http.StatusConflict, orderErrScene,
		&orderBulkRejectedError{items: items})
}

type orderBulkRejectedError struct {
	items []facing.BlockedItem
}

func (e *orderBulkRejectedError) Error() string { return orderenums.ErrBulkRejected }

func (e *orderBulkRejectedError) Facing() (string, string) {
	return orderenums.ErrBulkRejected, ""
}

func (e *orderBulkRejectedError) BlockedItems() []facing.BlockedItem { return e.items }

// couponBackURL 是优惠码写成功后回到列表的地址：保留当前筛选上下文。
func couponBackURL(c *gin.Context, projectID string) string {
	return "/admin/coupons?" + couponListQuery(projectID,
		couponFilter{Status: c.Query("status"), Keyword: c.Query("keyword")}, 0, 0)
}

// orderNormalizeBulkIDs 把归一交给 service（去重与上限属于业务规则），
// 这里只负责把失败事实交给共享出口。
func orderNormalizeBulkIDs(c *gin.Context, ids []string) ([]string, bool) {
	normalized, err := orderservice.NormalizeBatchIDs(ids)
	if err != nil {
		orderWriteFail(c, err)
		return nil, false
	}
	return normalized, true
}

// orderBulkFormIDs 从表单读 ids；完全没有这个键时返回 nil。
// 必须先解析表单：Gin 在 binder 之前不会填充 PostForm，直接读会让批量请求
// 被误判成单条请求（然后因 orderId 缺失整批 400）。
func orderBulkFormIDs(c *gin.Context) []string {
	if c.Request == nil {
		return nil
	}
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		if perr := c.Request.ParseForm(); perr != nil {
			return nil
		}
	}
	vals, ok := c.Request.PostForm["ids"]
	if !ok {
		return nil
	}
	return orderdto.OrderBulkIDs(vals)
}

// orderLogWriteError 记录写失败的内部原因，对外只给声明词条。
func orderLogWriteError(c *gin.Context, err error, message string) {
	logger.Scene(orderErrScene).With("user_id", c.GetInt64("user_id")).Error(err, message)
}

// orderWriteTranslation 取一个词条的当前语言译文；取不到时返回 key 本身。
func orderWriteTranslation(c *gin.Context, key string) string {
	return response.TranslateMessage(c, key)
}
