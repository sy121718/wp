package orderhttp

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

type couponPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
}

func NewCouponPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService) *couponPageHandle {
	return &couponPageHandle{orders: orders, projects: projects}
}

type couponFilter struct {
	Status string

	Keyword string

	CouponID uint64
}

const couponRedemptionPageSize = 50

var couponStatusFilterValues = []string{
	orderenums.CouponStateEnabled,
	orderenums.CouponStateDisabled,
	orderenums.CouponStateExpired,
	orderenums.CouponStateExhausted,
}

// couponStateLabelFacts 把优惠券显示状态的词条 key 交给模板按请求语言取词。
// 模板不拼 key 字符串：状态到词条的映射是枚举事实，留在 Go 侧。
func couponStateLabelFacts() map[string]StatusLabelFact {
	out := make(map[string]StatusLabelFact, len(couponStateLabelValues))
	for _, v := range couponStateLabelValues {
		key, fallback := orderenums.CouponStateLabel(v)
		out[v] = StatusLabelFact{Key: key, Fallback: fallback}
	}
	return out
}

var couponStateLabelValues = []string{
	orderenums.CouponStateEnabled,
	orderenums.CouponStateDisabled,
	orderenums.CouponStateExpired,
	orderenums.CouponStateNotStarted,
	orderenums.CouponStateExhausted,
}

func (h *couponPageHandle) CouponsPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)

	loadErrText := ""
	if loadErr != nil {
		projects = nil
		loadErrText = orderFacingError(c, loadErr)
	}

	selected := ""
	if loadErr == nil {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	page, limit := orderListWindow(c)
	filter := couponFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		CouponID: orderQueryID(c.Query("couponId")),
	}

	filterStatusValue, filterStatusValid := couponFilterStatusValue(filter.Status)

	var coupons []*orderdto.CouponResp
	var total int64
	if selected != "" {
		list, lerr := h.orders.ListCoupons(ctx, &orderdto.CouponListReq{
			ProjectID: selected,
			Keyword:   filter.Keyword,
			Status:    filter.Status,
			Offset:    utils.PageOffset(page, limit),
			Limit:     limit,
		})
		if lerr != nil {
			loadErrText = firstNonEmpty(loadErrText, couponFacingError(c, lerr))
		} else {
			coupons, total = list.List, list.Total
		}
	}

	var expanded *orderdto.CouponResp
	var redemptions []*orderdto.CouponRedemptionResp
	var redemptionTotal int64
	if selected != "" && filter.CouponID > 0 {
		cp, gerr := h.orders.GetCoupon(ctx, filter.CouponID)
		switch {
		case gerr != nil:
			loadErrText = firstNonEmpty(loadErrText, couponFacingError(c, gerr))
		case cp == nil || cp.ProjectID != selected:

			loadErrText = firstNonEmpty(loadErrText, response.MsgText(c, couponForeignLabel))
		default:
			expanded = cp
			rl, rerr := h.orders.ListCouponRedemptions(ctx, &orderdto.CouponRedemptionListReq{
				ProjectID: selected,
				CouponID:  filter.CouponID,
				Offset:    0,
				Limit:     couponRedemptionPageSize,
			})
			if rerr != nil {
				loadErrText = firstNonEmpty(loadErrText, couponFacingError(c, rerr))
			} else {
				redemptions, redemptionTotal = rl.List, rl.Total
			}
		}
	}

	view := &CouponListPageResp{

		Title: orderenums.CouponsHeading,
		Menu:  "coupons",

		Projects:        projects,
		SelectedProject: selected,
		Coupons:         coupons,
		Total:           total,
		Expanded:        expanded,
		Redemptions:     redemptions,
		RedemptionTotal: redemptionTotal,
		RedemptionLimit: couponRedemptionPageSize,

		StatusValues:      couponStatusFilterValues,
		StateLabels:       couponStateLabelFacts(),
		FilterStatus:      filterStatusValue,
		FilterStatusValid: filterStatusValid,
		FilterKeyword:     filter.Keyword,

		LoadErr: loadErrText,

		ListQuery: couponListQuery(selected, filter, page, limit),
		Page:      page,
		Limit:     limit,
	}

	view.Pagination = pagination.Page{
		Total: total, Page: page, Limit: limit,
		Base: backpath.WithParams("/admin/coupons", map[string]string{
			"project": selected, "status": filter.Status, "keyword": filter.Keyword,
		}),
	}
	pageview.RenderTemplate(c, CouponListPageTemplate, view)
}

func (h *couponPageHandle) CouponEditForm(c *gin.Context) {
	c.Header("Cache-Control", "no-store")

	rawID := c.Query("id")
	id := orderQueryID(rawID)
	projectID := strings.TrimSpace(c.Query("project"))
	if id == 0 || strconv.FormatUint(id, 10) != rawID || projectID == "" {
		// 页面 GET 失败也要交 JSON 体：空 body 的 400 会让前端拿不到 message。
		response.ParamError(c)
		return
	}
	ctx := c.Request.Context()
	exists, err := h.projects.Exists(ctx, projectID)
	if err != nil {
		response.InternalErrorForPage(c, "coupon-edit-form", err)
		return
	}
	if !exists {
		response.ErrorWithMessage(c, http.StatusNotFound, orderenums.ErrCouponNotFound)
		return
	}
	cp, err := h.orders.GetCoupon(ctx, id)
	if err != nil {
		// service 交的是 facing 错误（Error() 返回裸 key），匹配走 errors.Is；
		// 拿 err.Error() 和 key 做字符串比较会在包装或换文案时静默失效。
		if errors.Is(err, orderenums.Err(orderenums.ErrCouponNotFound)) {
			response.ErrorWithMessage(c, http.StatusNotFound, orderenums.ErrCouponNotFound)
			return
		}
		response.InternalErrorForPage(c, "coupon-edit-form", err)
		return
	}
	if cp == nil || cp.ID != id || cp.ProjectID != projectID {
		response.ErrorWithMessage(c, http.StatusNotFound, orderenums.ErrCouponNotFound)
		return
	}

	pageview.RenderTemplate(c, CouponEditFormTemplate, CouponEditFormView{

		Coupon:    cp,
		ListQuery: couponListQuery(projectID, couponFilter{Status: c.Query("status"), Keyword: c.Query("keyword")}, 0, 0),
	})
}

func couponListQuery(projectID string, filter couponFilter, page, limit int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("status", filter.Status)
	set("keyword", filter.Keyword)
	if filter.CouponID > 0 {
		set("couponId", strconv.FormatUint(filter.CouponID, 10))
	}
	if page > 0 {
		set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

func couponFilterStatusValue(raw string) (value string, ok bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", false
	}
	for _, v := range couponStatusFilterValues {
		if v == s {
			return v, true
		}
	}
	return strings.TrimSpace(raw), false
}

var couponFacingExtras = []string{
	orderenums.MsgCouponCreated, orderenums.MsgCouponUpdated, orderenums.MsgCouponDeleted,
	orderenums.ErrCouponCodeTaken, orderenums.ErrCouponTypeInvalid, orderenums.ErrCouponValueInvalid,
	orderenums.ErrCouponWindowInvalid, orderenums.ErrCouponInUse,
	couponIDInvalidLabel.Fallback, couponForeignLabel.Fallback,
}

var (
	couponIDInvalidLabel = facing.Msg{Key: orderenums.CouponFormInvalidID, Fallback: "优惠码编号不合法，请回到列表页重新操作。"}

	couponForeignLabel = facing.Msg{Key: orderenums.CouponFormForeignProject, Fallback: "这张优惠码不属于当前选中的站点工程，请切回它所属的工程再操作。"}
)

func couponFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if hit := orderFacingText(msg); hit != "" {
		return hit
	}
	for _, allowed := range couponFacingExtras {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

func couponFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if key := couponFacingText(err.Error()); key != "" {
		return response.MsgText(c, facing.Msg{Key: key, Fallback: key})
	}
	logger.Scene("order-coupon").With("path", c.Request.URL.Path).Error(err, "优惠码写操作失败")
	return response.InternalText(c)
}

func couponEcho(c *gin.Context) url.Values {
	if c == nil || c.Request == nil {
		return url.Values{}
	}
	_ = c.Request.ParseMultipartForm(couponEchoMemory)
	if c.Request.PostForm == nil {
		return url.Values{}
	}
	return c.Request.PostForm
}

const couponEchoMemory = 32 << 20
