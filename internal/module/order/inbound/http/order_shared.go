package orderhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	view "go_wp/pkg/view"
	"go_wp/pkg/logger"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/facing"
)

func countsFilled(counts map[string]int64, keys []string) map[string]int64 {
	out := make(map[string]int64, len(keys))
	for _, k := range keys {
		out[k] = counts[k]
	}
	return out
}

func orderStatusTabs() []StatusTabResp {
	tabs := make([]StatusTabResp, 0, len(orderStatusValues))
	for _, v := range orderStatusValues {
		tabs = append(tabs, StatusTabResp{Value: v, Kind: orderenums.OrderStatusTone(v)})
	}
	return tabs
}

func returnStatusTabs() []StatusTabResp {
	tabs := make([]StatusTabResp, 0, len(returnStatusValues))
	for _, v := range returnStatusValues {
		tabs = append(tabs, StatusTabResp{Value: v, Kind: orderenums.ReturnStatusTone(v)})
	}
	return tabs
}

var createdViaValues = []string{"", "checkout", "admin", "api"}

func fulfillmentTabs() []StatusTabResp {
	tabs := make([]StatusTabResp, 0, len(orderenums.FulfillmentStatuses))
	for _, v := range orderenums.FulfillmentStatuses {
		tabs = append(tabs, StatusTabResp{Value: v, Kind: orderenums.FulfillmentTone(v)})
	}
	return tabs
}

func fulfillmentPageFacts(filterValue string, orders []*orderdto.OrderResp) map[string]StatusLabelFact {
	list := append([]string(nil), orderenums.FulfillmentStatuses...)
	list = append(list, filterValue)
	for _, o := range orders {
		if o != nil {
			list = append(list, o.FulfillmentStatus)
		}
	}
	return statusLabelFacts(list, orderenums.FulfillmentLabel)
}

type translate = func(key, fallback string) string

func orderTr(c *gin.Context) translate {
	return func(key, fallback string) string {
		return response.ArgsText(c, key, fallback, nil)
	}
}

type orderLabel struct{ key, fallback string }

func orderLabelOf(tr translate, l orderLabel) string {
	if tr == nil {
		return l.fallback
	}
	return tr(l.key, l.fallback)
}

func orderStatusLabel(tr translate, status string) string {
	key, fallback := orderenums.OrderStatusLabel(status)
	if key == "" {
		if strings.TrimSpace(status) == "" {
			return orderFieldEmpty
		}
		return fallback
	}
	return orderLabelOf(tr, orderLabel{key, fallback})
}

func countryLabelFn(c *gin.Context, dict sysconfigcontract.DictReader) func(string) string {
	if c == nil || dict == nil {
		return nil
	}
	ctx := c.Request.Context()
	lang := response.RequestLanguage(c)
	return func(code string) string {
		code = strings.TrimSpace(code)
		if code == "" {
			return ""
		}
		if label := strings.TrimSpace(dict.CountryLabel(ctx, lang, code)); label != "" {
			return label
		}
		return code
	}
}

func orderListWindow(c *gin.Context) (page, limit int) {
	page, limit = reqparams.PageParams(c)
	if strings.TrimSpace(c.Query("page")) != "" {
		return page, limit
	}
	if offset, err := strconv.Atoi(strings.TrimSpace(c.Query("offset"))); err == nil && offset > 0 {
		page = offset/limit + 1
	}
	return page, limit
}

func orderQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// orderFacingMsg 把错误收成一条消息事实，取词留给出口。
//
// 允许上屏的只有 orderenums.UserFacingMessages 里的 key（这些都是词库条目，
// 中英双语都在），其余一律归口成内部错误文案，不把裸 key、SQL 原文或
// err.Error() 交给前端。这里不取词：Go 侧取词只有模板 t/tf 与 JSON 出口
// response 两条路，handler 自己翻译等于第三种出口。
func orderFacingMsg(c *gin.Context, err error) facing.Msg {
	if err == nil {
		return facing.Msg{}
	}
	if key := orderFacingText(err.Error()); key != "" {
		// Key 与 Fallback 同值是有意的：词库查得到就用译文，查不到时
		// response.ArgsText 会先看 fallback 是否等于 key，相等即整条归口
		// 成内部错误文案 —— 这正是我们要的「不许裸 key 上屏」。
		return facing.Msg{Key: key, Fallback: key}
	}
	// orderFacingText 白名单里有 ErrInternal 这个中文常量（不是 key），
	// 它上面已经命中过；走到这里说明是真实失败，记日志再归口。
	logger.Scene("order-page").With("path", c.Request.URL.Path).Error(err, "订单后台页处理失败")
	return view.InternalMsg()
}

// orderFacingError 保留原签名：调用点都在页面 View 里填 LoadErr 字符串。
//
// nil 必须在进 MsgText 之前短路：空 Msg{} 会被 ArgsText 当成「缺词」，
// 反过来产出一条内部错误文案 —— nil 不是错误，不该产出任何文案。
func orderFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	return response.MsgText(c, orderFacingMsg(c, err))
}

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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
