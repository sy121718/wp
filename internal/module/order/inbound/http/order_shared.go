package orderhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
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
		return adminui.TranslateArgs(c, key, fallback, nil)
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

func orderFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}

	if msg := orderFacingText(err.Error()); msg != "" {
		return i18n.TranslateFunc(response.RequestLanguage(c))(msg, msg)
	}
	logger.Scene("order-page").With("path", c.Request.URL.Path).Error(err, "订单后台页处理失败")
	return adminui.PageInternalText(c)
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
