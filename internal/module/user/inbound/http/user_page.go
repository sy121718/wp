package userhttp

import (
	"go_wp/internal/shell"
	"go_wp/internal/backref"
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gsessions "github.com/gorilla/sessions"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	userservice "go_wp/internal/module/user/service"
	"go_wp/pkg/auth"
	"go_wp/pkg/crypto"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const customerCohortPath = "/admin/customers/cohort"

var customerCohortTitleLabel = userLabel{userenums.KeyCustomerCohortTitle, "群组留存"}

var customerCohortUnavailableLabel = userLabel{
	userenums.KeyCustomerCohortUnavailable,
	"群组留存暂时不可用（数据没接上）。",
}

func (h *customerPageHandle) CustomerCohortPage(c *gin.Context) {
	ctx := c.Request.Context()
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())

	var projects []projectcontract.ProjectResp
	if h.projects != nil {
		if list, perr := h.projects.List(ctx); perr == nil {
			projects = list
		}
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	var res *orderdto.CustomerCohortResp
	var errMsg customerMsg
	switch {
	case h.cohort == nil:
		errMsg = customerCohortUnavailableLabel.msg()
	case selected == "":

		errMsg = customerMsg{}
	default:
		got, err := h.cohort.CustomerCohortByRange(ctx, &orderdto.CustomerCohortReq{
			ProjectID: selected,
			From:      rng.From,
			To:        rng.To,
		})
		if err != nil {
			errMsg = customerErrMsg(c, err)
		} else {
			res = got
		}
	}

	view := customerOverviewPageData(customerLabelText(c, customerCohortTitleLabel), rng, nil, errMsg)
	view.Path = customerCohortPath
	view.CohortReady = res != nil

	cols := []CohortColumnResp{}
	rows := res
	if rows != nil {
		for k := 0; k < rows.Months; k++ {
			cols = append(cols, CohortColumnResp{Index: k, Text: strconv.Itoa(k)})
		}
	}
	view.Columns = cols
	view.Colspan = len(cols) + 2
	if res != nil {
		view.Cohorts = res.Cohorts
		view.Customers = res.Customers
		view.Months = res.Months
		view.Rows = toAnySlice(res.Rows)
	} else {
		view.Rows = []any{}
	}
	pageview.RenderTemplate(c, CustomerCohortPageTemplate, view)
}

type customerMsg = adminui.Msg

func customerErrMsg(c *gin.Context, err error) customerMsg {
	return customerErrFactOf(adminui.PageErrorFact(c, userErrScene, err))
}

func customerErrFactOf(f adminui.ErrFact) customerMsg {
	if f.Key == "" && f.Fallback == "" {
		return customerMsg{}
	}
	return customerMsg{Key: f.Key, Fallback: f.Fallback}
}

func customerErrText(c *gin.Context, err error) string {
	return adminui.MsgText(c, customerErrMsg(c, err))
}

type userLabel struct{ key, fallback string }

func (l userLabel) msg() customerMsg {
	return customerMsg{Key: l.key, Fallback: l.fallback}
}

func customerLabelText(c *gin.Context, l userLabel) string {
	return adminui.MsgText(c, l.msg())
}

func customerMsgFirst(values ...customerMsg) customerMsg {
	for _, v := range values {
		if !v.Empty() {
			return v
		}
	}
	return customerMsg{}
}

const (
	customerEmptyField = userenums.LabelEmptyField

	customerListPath   = "/admin/customers"
	customerDetailPath = "/admin/customers/detail"
)

var (
	customerPageTitleLabel       = userLabel{userenums.KeyCustomersHeading, "客户管理"}
	customerDetailPageTitleLabel = userLabel{userenums.KeyCustomerDetailHeading, "客户详情"}
)

const (
	customerStatusAll      = userenums.StatusAll
	customerStatusDisabled = userenums.StatusDisabled
	customerStatusActive   = userenums.StatusActive
	customerStatusPending  = userenums.StatusPending
)

const (
	customerActionDisable = userenums.KeyCustomerActionDisable
	customerActionEnable  = userenums.KeyCustomerActionEnable
)

var (
	customerUnavailableLabel = userLabel{userenums.KeyCustomerCapabilityMissing,
		"客户管理能力未装配（装配缺陷），本页只显示列表框架。"}

	customerInvalidIDLabel = userLabel{userenums.KeyCustomerFormInvalidID,
		"客户编号不合法，请回到列表页重新操作。"}

	customerOrdersUnavailableLabel = userLabel{userenums.KeyCustomerOrdersUnavailable,
		"订单摘要不可用：订单模块未装配（装配缺陷），客户资料本身不受影响。"}

	customerBulkNothingSelectedLabel = userLabel{userenums.KeyCustomerBulkNoneSelected,
		"批量操作：没有勾选任何账号，请先勾选左侧复选框再执行。"}

	customerStatusInvalidLabel = userLabel{userenums.ErrCustomerStatusInvalid, "账号状态取值不合法"}
)

type customerPageHandle struct {
	users usercontract.CustomerAdminPort

	orders ordercontract.CustomerOrderSummaryReader

	projects projectcontract.ProjectService

	membership membershipcontract.Reader

	growth ordercontract.CustomerGrowthReader

	segments ordercontract.CustomerSegmentReader

	rfm ordercontract.CustomerRfmReader

	cohort ordercontract.CustomerCohortReader

	membershipAdmin membershipcontract.AssignmentAdminPort
	membershipTiers membershipcontract.TierAdminPort
}

func (h *customerPageHandle) SetMembershipFilters(admin membershipcontract.AssignmentAdminPort, tiers membershipcontract.TierAdminPort) {
	h.membershipAdmin = admin
	h.membershipTiers = tiers
}

func (h *customerPageHandle) SetCustomerCohort(cohort ordercontract.CustomerCohortReader) {
	h.cohort = cohort
}

func (h *customerPageHandle) SetCustomerRfm(rfm ordercontract.CustomerRfmReader) {
	h.rfm = rfm
}

func (h *customerPageHandle) SetCustomerSegments(segments ordercontract.CustomerSegmentReader) {
	h.segments = segments
}

func (h *customerPageHandle) SetCustomerGrowth(growth ordercontract.CustomerGrowthReader) {
	h.growth = growth
}

func NewCustomerPageHandle(
	users usercontract.CustomerAdminPort,
	orders ordercontract.CustomerOrderSummaryReader,
	projects projectcontract.ProjectService,
) *customerPageHandle {
	return &customerPageHandle{users: users, orders: orders, projects: projects}
}

type customerFilter struct {
	Keyword        string
	Status         int
	EmailVerified  int
	Locked         bool
	RegisteredFrom string
	RegisteredTo   string

	Segment string

	SegmentFrom string
	SegmentTo   string

	MinOrders int

	TierID int64

	RfmSegment string
}

func customerPageSegment(v string) string {
	switch strings.TrimSpace(v) {
	case customerSegmentNew, customerSegmentReturning, customerSegmentRepurchasing:
		return strings.TrimSpace(v)
	}
	return ""
}

const (
	customerSegmentNew          = "new"
	customerSegmentReturning    = "returning"
	customerSegmentRepurchasing = "repurchasing"
)

var customerSegmentMinOrders = map[int]bool{2: true, 3: true, 5: true, 10: true}

var customerRfmSegments = map[string]bool{"vip": true, "potential": true, "low_value": true}

func customerPageRfmSegment(v string) string {
	trimmed := strings.TrimSpace(v)
	if customerRfmSegments[trimmed] {
		return trimmed
	}
	return ""
}

func customerPageTierID(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func customerPageMinOrders(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || !customerSegmentMinOrders[n] {
		return 0
	}
	return n
}

func (h *customerPageHandle) segmentCustomerIDs(ctx context.Context, f customerFilter) ([]int64, customerMsg) {
	if f.Segment == "" && f.MinOrders == 0 {
		return nil, customerMsg{}
	}
	if h.segments == nil {

		return nil, customerSegmentUnavailableLabel.msg()
	}
	if h.projects == nil {
		return nil, customerSegmentUnavailableLabel.msg()
	}
	list, perr := h.projects.List(ctx)
	if perr != nil {
		return nil, customerSegmentUnavailableLabel.msg()
	}
	if len(list) == 0 {

		return []int64{}, customerMsg{}
	}
	from, to := customerSegmentWindow(f, time.Now())
	res, err := h.segments.CustomerSegmentIDsByRange(ctx, &orderdto.CustomerSegmentIDsReq{
		ProjectID: list[0].ID,
		From:      from,
		To:        to,
		Segment:   f.Segment,
		MinOrders: f.MinOrders,

		Limit: customerSegmentFilterLimit,
	})
	if err != nil {
		return nil, customerSegmentUnavailableLabel.msg()
	}
	if res == nil {
		return []int64{}, customerMsg{}
	}
	return res.UserIDs, customerMsg{}
}

const customerSegmentFilterLimit = 500

var customerRfmSegmentUnavailableLabel = userLabel{
	userenums.KeyCustomerFilterRfmUnavailable,
	"按 RFM 分段筛选暂时不可用（数据没接上），请先用其它条件。",
}

var customerSegmentUnavailableLabel = userLabel{
	userenums.KeyCustomerFilterSegmentUnavailable,
	"按消费分段筛选暂时不可用（数据没接上），请先用其它条件。",
}

func customerSegmentWindow(f customerFilter, now time.Time) (from, to string) {
	from = strings.TrimSpace(f.SegmentFrom)
	to = strings.TrimSpace(f.SegmentTo)
	if from != "" && to != "" {
		return from, to
	}
	u := now.UTC()
	first := time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(f.SegmentFrom) == "" {
		from = first.Format(utils.LayoutDay)
	}
	if strings.TrimSpace(f.SegmentTo) == "" {
		to = today.Format(utils.LayoutDay)
	}
	return from, to
}

func (h *customerPageHandle) CustomersPage(c *gin.Context) {
	ctx := c.Request.Context()
	page, limit := reqparams.PageParams(c)
	filter := customerFilter{
		Keyword:        strings.TrimSpace(c.Query("keyword")),
		Status:         customerPageStatus(c.Query("status")),
		EmailVerified:  customerPageEmailVerified(c.Query("emailVerified")),
		Locked:         customerPageLocked(c.Query("locked")),
		RegisteredFrom: strings.TrimSpace(c.Query("registeredFrom")),
		RegisteredTo:   strings.TrimSpace(c.Query("registeredTo")),
		Segment:        customerPageSegment(c.Query("segment")),
		SegmentFrom:    strings.TrimSpace(c.Query("segmentFrom")),
		SegmentTo:      strings.TrimSpace(c.Query("segmentTo")),
		MinOrders:      customerPageMinOrders(c.Query("minOrders")),
		RfmSegment:     customerPageRfmSegment(c.Query("rfm")),
		TierID:         customerPageTierID(c.Query("tier")),
	}

	var pageErr customerMsg

	segIDs, segErr := h.segmentCustomerIDs(ctx, filter)

	rfmIDs, rfmErr := h.rfmSegmentIDs(ctx, filter)

	tierIDs, tierErr := h.tierCustomerIDs(ctx, filter)
	filterIDs := intersectCustomerIDs(intersectCustomerIDs(segIDs, rfmIDs), tierIDs)
	pageErr = customerMsgFirst(segErr, rfmErr, tierErr)

	var list *userdto.CustomerListResp
	switch {
	case h.users == nil:

		pageErr = customerMsgFirst(pageErr, customerUnavailableLabel.msg())
	case !pageErr.Empty():

	default:
		res, err := h.users.ListCustomers(ctx, &userdto.CustomerListReq{
			Keyword:        filter.Keyword,
			Status:         filter.Status,
			EmailVerified:  filter.EmailVerified,
			LockedOnly:     filter.Locked,
			RegisteredFrom: utils.NewJSONTimePtr(customerPageDayStart(filter.RegisteredFrom)),
			RegisteredTo:   utils.NewJSONTimePtr(customerPageDayEnd(filter.RegisteredTo)),
			UserIDs:        filterIDs,
			Offset:         utils.PageOffset(page, limit),
			Limit:          limit,
		})
		if err != nil {
			pageErr = customerMsgFirst(pageErr, customerErrMsg(c, err))
		} else {
			list = res
		}
	}

	view := customerListPageData(customerLabelText(c, customerPageTitleLabel), list, filter, page, limit, pageErr, h.users == nil)

	view.TierOptions = h.membershipTierOptions(ctx)
	view.BackPath = customerListPath

	view.Pagination = pagination.Page{
		Total: customerTotal(list), Page: page, Limit: limit,
		Base: reqparams.FilterBaseURL(customerListPath, customerFilterValues(filter)),
	}
	pageview.RenderTemplate(c, CustomersListPageTemplate, view)
}

func (h *customerPageHandle) CustomerDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := customerQueryID(c.Query("id"))
	if id == 0 {
		customerListJumpFail(c, customerInvalidIDLabel.msg())
		return
	}

	if h.users == nil {
		pageview.RenderTemplate(c, CustomerDetailPageTemplate, customerDetailPageData(
			customerLabelText(c, customerDetailPageTitleLabel), c,
			nil, nil, "", nil, false, false, customerUnavailableLabel.msg()))
		return
	}

	detail, err := h.users.GetCustomer(ctx, id)

	if err != nil || detail == nil {

		msg := customerErrMsg(c, err)
		if msg.Empty() {

			msg = customerMsg{Key: userenums.ErrUserNotFound, Fallback: "用户不存在"}
		}
		customerListJumpFail(c, msg)
		return
	}

	var projects []projectcontract.ProjectResp
	var projectsErr customerMsg
	if h.projects != nil {
		list, perr := h.projects.List(ctx)
		if perr != nil {
			projectsErr = customerErrMsg(c, perr)
		} else {
			projects = list
		}
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	var summary *ordercontract.CustomerOrderSummaryResp
	var summaryErr customerMsg
	switch {
	case h.orders == nil:
		summaryErr = customerOrdersUnavailableLabel.msg()
	case selected == "":

		summaryErr = customerMsg{}
	default:
		res, oerr := h.orders.CustomerOrderSummaryOf(ctx, &ordercontract.CustomerOrderSummaryReq{
			ProjectID: selected,
			UserID:    id,
		})
		if oerr != nil {
			summaryErr = customerErrMsg(c, oerr)
		} else {
			summary = res
		}
	}

	memberData := customerDetailPageData(
		customerLabelText(c, customerDetailPageTitleLabel), c,
		detail, projects, selected, summary, !projectsErr.Empty(), !summaryErr.Empty(),
		customerMsgFirst(projectsErr, summaryErr))
	applyCustomerMembership(memberData, h.membershipView(ctx, c, selected, id))
	pageview.RenderTemplate(c, CustomerDetailPageTemplate, memberData)
}

var customerListBackKeys = []string{
	"keyword", "status", "emailVerified", "locked",
	"registeredFrom", "registeredTo",
	"segment", "minOrders", "rfm", "tier", "segmentFrom", "segmentTo",
	"page", "limit",
}

func customerListQuery(filter customerFilter, page, limit int) string {
	q := url.Values{}
	for k, v := range customerFilterValues(filter) {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

var customerNoticeFallback = map[string]string{
	userenums.MsgCustomerEnabled:         "账号已启用",
	userenums.MsgCustomerDisabled:        "账号已停用",
	userenums.MsgCustomerUnlocked:        "账号已解除锁定",
	userenums.MsgCustomerFailuresCleared: "账号未处于锁定状态，登录失败计数已清零",
	userenums.MsgCustomerNotLocked:       "该账号没有处于锁定状态，无需解除",
}

func customerNotice(c *gin.Context, key string) string {
	return adminui.MsgText(c, customerMsg{Key: key, Fallback: customerNoticeFallback[key]})
}

func customerListBack(c *gin.Context) (back string, backText customerMsg) {
	return backref.BackPath(c, customerListPath, customerListBackKeys...),
		customerPageTitleLabel.msg()
}

func customerJump(c *gin.Context, ok bool, msg customerMsg, back string, backText customerMsg) {
	j := shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
	}
	if ok {
		j.Seconds = 1
	}
	shell.RenderJump(c, j)
}

func customerListJumpFail(c *gin.Context, msg customerMsg) {
	back, backText := customerListBack(c)
	customerJump(c, false, msg, back, backText)
}

func (h *customerPageHandle) rfmSegmentIDs(ctx context.Context, f customerFilter) ([]int64, customerMsg) {
	if f.RfmSegment == "" {
		return nil, customerMsg{}
	}

	if h.rfm == nil || h.projects == nil {
		return nil, customerRfmSegmentUnavailableLabel.msg()
	}
	list, perr := h.projects.List(ctx)
	if perr != nil {
		return nil, customerRfmSegmentUnavailableLabel.msg()
	}
	if len(list) == 0 {

		return []int64{}, customerMsg{}
	}
	from, to := customerSegmentWindow(f, time.Now())
	res, err := h.rfm.CustomerRfmSegmentIDsByRange(ctx, &orderdto.CustomerRfmSegmentIDsReq{
		ProjectID: list[0].ID,
		From:      from,
		To:        to,
		Segment:   f.RfmSegment,

		Limit: customerSegmentFilterLimit,
	})
	if err != nil {
		return nil, customerRfmSegmentUnavailableLabel.msg()
	}
	ids := res.UserIDs
	if ids == nil {
		ids = []int64{}
	}
	return ids, customerMsg{}
}

func intersectCustomerIDs(a, b []int64) []int64 {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return b
	case b == nil:
		return a
	}
	inA := make(map[int64]bool, len(a))
	for _, id := range a {
		inA[id] = true
	}
	out := make([]int64, 0, len(b))
	for _, id := range b {
		if inA[id] {
			out = append(out, id)
		}
	}
	return out
}

const (
	membershipFilterPageSize = 200

	membershipFilterMaxPages = 5
)

var membershipFilterTruncatedLabel = userLabel{
	userenums.KeyCustomerFilterTierTruncated,
	"该会员等级的人数超过筛选上限，无法作为筛选项使用。请配合其它条件一起筛。",
}

func (h *customerPageHandle) tierCustomerIDs(ctx context.Context, f customerFilter) ([]int64, customerMsg) {
	if f.TierID <= 0 {
		return nil, customerMsg{}
	}
	if h.membershipAdmin == nil || h.projects == nil {
		return nil, membershipFilterUnavailableLabel.msg()
	}
	list, perr := h.projects.List(ctx)
	if perr != nil {
		return nil, membershipFilterUnavailableLabel.msg()
	}
	if len(list) == 0 {

		return []int64{}, customerMsg{}
	}
	ids := make([]int64, 0, membershipFilterPageSize)
	for page := 1; page <= membershipFilterMaxPages; page++ {
		rows, err := h.membershipAdmin.ListAssignments(ctx, &membershipdto.ListAssignmentsReq{
			ProjectID: list[0].ID,
			TierID:    f.TierID,
			Page:      page,
			Size:      membershipFilterPageSize,
		})
		if err != nil {
			return nil, membershipFilterUnavailableLabel.msg()
		}
		for _, row := range rows {
			ids = append(ids, int64(row.UserID))
		}
		if len(rows) < membershipFilterPageSize {
			return ids, customerMsg{}
		}
	}

	return nil, membershipFilterTruncatedLabel.msg()
}

var membershipFilterUnavailableLabel = userLabel{
	userenums.KeyCustomerFilterTierUnavailable,
	"按会员等级筛选暂时不可用（会员模块没接上），请先用其它条件。",
}

func (h *customerPageHandle) membershipTierOptions(ctx context.Context) []MembershipTierOptionResp {
	out := []MembershipTierOptionResp{}
	if h.membershipTiers == nil || h.projects == nil {
		return out
	}
	list, perr := h.projects.List(ctx)
	if perr != nil || len(list) == 0 {
		return out
	}
	tiers, err := h.membershipTiers.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: list[0].ID})
	if err != nil {
		return out
	}
	for _, t := range tiers {

		out = append(out, MembershipTierOptionResp{ID: strconv.FormatInt(t.ID, 10), Name: t.Name})
	}
	return out
}

const customerOverviewPath = "/admin/customers/overview"

const customerOverviewDefaultPreset = "month"

var customerOverviewPresets = []string{"today", "yesterday", "week", "month", "lastMonth", "days7", "days30", "year"}

const customerOverviewRangeMaxDays = 366

var customerOverviewTitleLabel = userLabel{userenums.KeyCustomerOverviewTitle, "客户概览"}

type customerOverviewRange struct {
	Key  string
	From string
	To   string
}

func customerOverviewRangeOf(key, fromQ, toQ string, now time.Time) customerOverviewRange {
	if r, ok := customerOverviewExplicitRange(fromQ, toQ, now); ok {
		return r
	}

	today := customerOverviewDayStart(now)
	switch key {
	case "today":
		key = "today"
	case "yesterday":
		key = "yesterday"
		today = today.AddDate(0, 0, -1)
	case "week":
		today = today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	case "year":
		today = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case "lastMonth":
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	case "days7":
		today = today.AddDate(0, 0, -6)
	case "days30":
		today = today.AddDate(0, 0, -29)
	case "month":
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		key = customerOverviewDefaultPreset
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	}

	end := customerOverviewDayStart(now)
	return customerOverviewRange{
		Key:  key,
		From: today.Format(utils.LayoutDay),
		To:   end.Format(utils.LayoutDay),
	}
}

func customerOverviewExplicitRange(fromQ, toQ string, now time.Time) (customerOverviewRange, bool) {
	if fromQ == "" && toQ == "" {
		return customerOverviewRange{}, false
	}
	today := customerOverviewDayStart(now)

	from, fromOK := customerOverviewParseDay(fromQ)
	to, toOK := customerOverviewParseDay(toQ)
	if !fromOK && !toOK {

		return customerOverviewRange{}, false
	}
	if !fromOK {
		from = today
	}
	if !toOK {
		to = today
	}
	if from.After(to) {
		from, to = to, from
	}
	if to.After(today) {
		to = today
	}
	if from.After(to) {

		from = to
	}
	if maxFrom := to.AddDate(0, 0, -(customerOverviewRangeMaxDays - 1)); from.Before(maxFrom) {
		from = maxFrom
	}
	return customerOverviewRange{
		Key:  "custom",
		From: from.Format(utils.LayoutDay),
		To:   to.Format(utils.LayoutDay),
	}, true
}

func customerOverviewParseDay(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	d, err := time.Parse(utils.LayoutDay, s)
	if err != nil {
		return time.Time{}, false
	}
	return d.UTC(), true
}

func customerOverviewDayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func customerRangeQuery(rng customerOverviewRange) string {
	return "from=" + rng.From + "&to=" + rng.To
}

func customerOverviewPageData(title string, rng customerOverviewRange, growth *orderdto.CustomerGrowthResp, errMsg customerMsg) *CustomerCohortPageResp {

	view := &CustomerCohortPageResp{
		Title:       title,
		Path:        customerOverviewPath,
		Range:       rng,
		ErrKey:      errMsg.Key,
		ErrFallback: errMsg.Fallback,
		HasErr:      !errMsg.Empty(),
		GrowthReady: growth != nil,

		Rows: []any{},
	}
	if growth == nil {
		return view
	}
	view.OrderingCustomers = growth.OrderingCustomers
	view.NewCustomers = growth.NewCustomers
	view.ReturningCustomers = growth.ReturningCustomers
	view.Repurchasers = growth.Repurchasers
	view.NewRepurchasers = growth.NewRepurchasers
	view.RepurchaseRateLabel = growth.RepurchaseRateLabel
	return view
}

func (h *customerPageHandle) CustomerOverviewPage(c *gin.Context) {
	ctx := c.Request.Context()
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())

	var projects []projectcontract.ProjectResp
	if h.projects != nil {
		if list, perr := h.projects.List(ctx); perr == nil {
			projects = list
		}
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	var growth *orderdto.CustomerGrowthResp
	var errMsg customerMsg
	switch {
	case h.growth == nil:
		errMsg = customerUnavailableLabel.msg()
	case selected == "":

		errMsg = customerMsg{}
	default:
		res, err := h.growth.CustomerGrowthByRange(ctx, &orderdto.CustomerGrowthReq{
			ProjectID: selected,
			From:      rng.From,
			To:        rng.To,
		})
		if err != nil {
			errMsg = customerErrMsg(c, err)
		} else {
			growth = res
		}
	}

	pageview.RenderTemplate(c, CustomerOverviewPageTemplate, customerOverviewPageData(customerLabelText(c, customerOverviewTitleLabel), rng, growth, errMsg))
}

const customerRfmPath = "/admin/customers/rfm"

var customerRfmTitleLabel = userLabel{userenums.KeyCustomerRfmTitle, "RFM 分析"}

type customerRfmRow struct {
	UserID        int64
	Name          string
	Email         string
	LastOrderAt   string
	RecencyDays   int
	Frequency     int64
	MonetaryLabel string
	RScore        int
	FScore        int
	MScore        int
	TotalScore    int
	Segment       string
}

func customerRfmPageData(title string, rng customerOverviewRange,
	res *orderdto.CustomerRfmResp, rows []customerRfmRow,
	segment string, page, limit int, total int64, errMsg customerMsg) *CustomerRfmPageResp {
	view := &CustomerRfmPageResp{
		Title:       title,
		Path:        customerRfmPath,
		Range:       rng,
		Segment:     segment,
		ErrKey:      errMsg.Key,
		ErrFallback: errMsg.Fallback,
		HasErr:      !errMsg.Empty(),
		Rows:        toAnySlice(rows),
		Page:        page,
		Limit:       limit,
		Total:       total,

		RfmReady: res != nil,

		SegmentTabs: []RfmSegmentTabResp{},
	}
	if res == nil {
		return view
	}
	view.Customers = res.Customers
	view.Vip = res.Vip
	view.Potential = res.Potential
	view.LowValue = res.LowValue
	view.SegmentTabs = customerRfmSegmentTabs(res, segment, rng)
	return view
}

func customerRfmSegmentTabs(res *orderdto.CustomerRfmResp, active string, rng customerOverviewRange) []RfmSegmentTabResp {
	if res == nil {
		return nil
	}
	mk := func(key, labelKey, label string, count int64) RfmSegmentTabResp {
		url := customerRfmPath + "?" + customerRangeQuery(rng)
		if key != "" {
			url += "&segment=" + key
		}

		return RfmSegmentTabResp{
			Key: key, LabelKey: labelKey, Label: label,
			Count: count, Active: key == active, URL: url,
		}
	}
	return []RfmSegmentTabResp{
		mk("", userenums.KeyCustomerRfmTabAll, "全部", res.Customers),
		mk("vip", userenums.KeyCustomerRfmSegmentVip, "高价值", res.Vip),
		mk("potential", userenums.KeyCustomerRfmSegmentPotential, "潜力", res.Potential),
		mk("low_value", userenums.KeyCustomerRfmSegmentLowValue, "一般", res.LowValue),
	}
}

func customerRfmPageSegment(v string) string {
	switch v {
	case "vip", "potential", "low_value":
		return v
	}
	return ""
}

func (h *customerPageHandle) CustomerRfmPage(c *gin.Context) {
	ctx := c.Request.Context()
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())
	segment := customerRfmPageSegment(c.Query("segment"))
	page, limit := reqparams.PageParams(c)
	if limit > 0 && limit > rfmMaxPageSize {
		limit = rfmMaxPageSize
	}

	var res *orderdto.CustomerRfmResp
	var errMsg customerMsg
	switch {
	case h.rfm == nil:
		errMsg = customerRfmUnavailableLabel.msg()
	case h.projects == nil:
		errMsg = customerRfmUnavailableLabel.msg()
	default:
		list, perr := h.projects.List(ctx)
		if perr != nil {
			errMsg = customerRfmUnavailableLabel.msg()
			break
		}
		if len(list) == 0 {

			errMsg = customerMsg{}
			break
		}
		out, rerr := h.rfm.CustomerRfmByRange(ctx, &orderdto.CustomerRfmReq{
			ProjectID: list[0].ID,
			From:      rng.From,
			To:        rng.To,
			Segment:   segment,
			Limit:     limit,
			Offset:    utils.PageOffset(page, limit),
		})
		if rerr != nil {
			errMsg = customerRfmUnavailableLabel.msg()
		} else {
			res = out
		}
	}

	rows := h.customerRfmRows(ctx, res)
	var total int64
	if res != nil {
		total = res.Total
	}
	view := customerRfmPageData(customerLabelText(c, customerRfmTitleLabel), rng, res,
		rows, segment, page, limit, total, errMsg)

	rfnBase := customerRfmPath + "?" + customerRangeQuery(rng)
	if segment != "" {
		rfnBase += "&segment=" + segment
	}
	view.Pagination = pagination.Page{Total: total, Page: page, Limit: limit, Base: rfnBase}
	pageview.RenderTemplate(c, CustomerRfmPageTemplate, view)
}

func (h *customerPageHandle) customerRfmRows(ctx context.Context, res *orderdto.CustomerRfmResp) []customerRfmRow {
	if res == nil || len(res.Items) == 0 {
		return nil
	}
	names := map[int64]customerRfmRow{}
	if h.users != nil {
		ids := make([]int64, 0, len(res.Items))
		for _, it := range res.Items {
			ids = append(ids, it.UserID)
		}
		if list, err := h.users.ListCustomers(ctx, &userdto.CustomerListReq{
			UserIDs: ids, Limit: len(ids),
		}); err == nil && list != nil {
			for _, c := range list.List {
				names[int64(c.ID)] = customerRfmRow{Name: c.DisplayName, Email: c.Email}
			}
		}
	}
	rows := make([]customerRfmRow, 0, len(res.Items))
	for _, it := range res.Items {
		row := customerRfmRow{
			UserID:        it.UserID,
			LastOrderAt:   it.LastOrderAt,
			RecencyDays:   it.RecencyDays,
			Frequency:     it.Frequency,
			MonetaryLabel: it.MonetaryLabel,
			RScore:        it.RScore,
			FScore:        it.FScore,
			MScore:        it.MScore,
			TotalScore:    it.TotalScore,
			Segment:       it.Segment,
		}
		if info, ok := names[it.UserID]; ok {
			row.Name = info.Name
			row.Email = info.Email
		}
		rows = append(rows, row)
	}
	return rows
}

const rfmMaxPageSize = 200

var customerRfmUnavailableLabel = userLabel{
	userenums.KeyCustomerRfmUnavailable,
	"RFM 分析暂时不可用（数据没接上），请稍后再试。",
}

func customerListPageData(title string, list *userdto.CustomerListResp, filter customerFilter,
	page, limit int, pageErr customerMsg, capabilityMissing bool) *CustomersListPageResp {
	rows := make([]CustomerListRowResp, 0)
	counters := userdto.CustomerCounters{}
	if list != nil {
		counters = list.Counters
		for _, item := range list.List {
			rows = append(rows, customerRow(item))
		}
	}
	return &CustomersListPageResp{
		Title:    title,
		Menu:     "customers",
		Rows:     rows,
		Total:    customerTotal(list),
		Page:     page,
		Limit:    limit,
		Counters: counters,

		CounterTabs:       customerCounterTabs(counters, filter),
		FilterActive:      customerFilterActive(filter),
		FilterLocked:      filter.Locked,
		FilterKeyword:     filter.Keyword,
		FilterStatus:      filter.Status,
		FilterVerified:    filter.EmailVerified,
		FilterFrom:        filter.RegisteredFrom,
		FilterTo:          filter.RegisteredTo,
		FilterSegment:     filter.Segment,
		FilterSegmentFrom: filter.SegmentFrom,
		FilterSegmentTo:   filter.SegmentTo,
		FilterMinOrders:   customerMinOrdersQueryValue(filter.MinOrders),
		FilterRfm:         filter.RfmSegment,
		FilterTier:        customerTierQueryValue(filter.TierID),

		TierOptions: []MembershipTierOptionResp{},
		ErrKey:      pageErr.Key,
		ErrFallback: pageErr.Fallback,
		HasErr:      !pageErr.Empty(),

		CapabilityMissing: capabilityMissing,
	}
}

func customerDetailPageData(title string, c *gin.Context, detail *userdto.CustomerResp, projects []projectcontract.ProjectResp,
	selected string, summary *ordercontract.CustomerOrderSummaryResp,
	projectsFailed, summaryFailed bool, pageErr customerMsg) *CustomerDetailPageResp {
	view := &CustomerDetailPageResp{
		Title:           title,
		Menu:            "customers",
		ErrKey:          pageErr.Key,
		ErrFallback:     pageErr.Fallback,
		HasErr:          !pageErr.Empty(),
		HasDetail:       detail != nil,
		Projects:        toAnySlice(projects),
		SelectedProject: selected,
		HasProjects:     len(projects) > 0,
		ProjectsFailed:  projectsFailed,
		SummaryFailed:   summaryFailed,
		HasSummary:      summary != nil,
		ListURL:         customerDetailBackURL(c),
	}
	if detail != nil {
		row := customerRow(detail)
		view.Customer = row
		view.ID = detail.ID
		view.Username = detail.Username
		view.Email = detail.Email

		view.DisplayName = row.DisplayName
		view.Nickname = row.Nickname

		view.Status = row.Status

		view.StatusLabelKey = row.StatusLabelKey
		view.StatusLabelFallback = row.StatusLabelFallback
		view.EmailVerified = detail.EmailVerified
		view.EmailVerifiedKey = row.EmailVerifiedKey
		view.EmailVerifiedFallback = row.EmailVerifiedFallback

		view.RegisteredAt = row.RegisteredAt
		view.RegisterIP = row.RegisterIP
		view.RegisterLocation = row.RegisterLocation
		view.LastLoginTime = row.LastLoginTime
		view.LastLoginIP = row.LastLoginIP
		view.LastLoginLocation = row.LastLoginLocation
		view.Locked = detail.Locked
		view.LockedUntil = row.LockedUntil
		view.LoginFailureCount = detail.LoginFailureCount
		view.Actionable = row.Actionable
		view.NextStatus = row.NextStatus

		view.PendingHintKey = row.PendingHintKey
		view.PendingHintFallback = row.PendingHintFallback

		view.StatusActionKey = row.StatusActionKey
		view.StatusActionLabel = row.StatusActionLabel

		view.LockHintKey = row.LockHintKey
		view.LockHintFallback = row.LockHintFallback
	}
	if summary != nil {
		view.OrderCount = summary.OrderCount
		view.PaidOrderCount = summary.PaidOrderCount
		view.TotalAmountLabel = summary.TotalAmountLabel
		view.LastOrderNo = summary.LastOrderNo

		view.LastOrderTime = summary.LastOrderTime
		view.LastOrderID = summary.LastOrderID
		view.HasOrders = summary.HasOrders

		view.LastOrderStatusKey, view.LastOrderStatusFallback = ordercontract.OrderStatusLabel(summary.LastOrderStatus)
	} else {
		view.HasSummary = false
	}

	return view
}

func customerRow(item *userdto.CustomerResp) CustomerListRowResp {

	statusKey, statusFallback := userenums.StatusLabel(item.Status)
	verifyKey, verifyFallback := userenums.EmailVerifiedLabel(item.EmailVerified)
	row := CustomerListRowResp{
		ID:       item.ID,
		Username: customerTextOrEmpty(item.Username),
		Email:    customerTextOrEmpty(item.Email),

		DisplayName:           item.DisplayName,
		Nickname:              item.Nickname,
		Status:                item.Status,
		StatusLabelKey:        statusKey,
		StatusLabelFallback:   statusFallback,
		EmailVerified:         item.EmailVerified,
		EmailVerifiedKey:      verifyKey,
		EmailVerifiedFallback: verifyFallback,

		RegisteredAt:      item.RegisteredAt,
		RegisterIP:        customerTextOrEmpty(item.RegisterIP),
		RegisterLocation:  customerTextOrEmpty(item.RegisterLocation),
		LastLoginTime:     item.LastLoginTime,
		LastLoginIP:       customerTextOrEmpty(item.LastLoginIP),
		LastLoginLocation: customerTextOrEmpty(item.LastLoginLocation),
		Locked:            item.Locked,
		LockedUntil:       item.LockedUntilTime,
		LoginFailureCount: item.LoginFailureCount,
	}
	switch item.Status {
	case customerStatusActive:
		row.Actionable = true
		row.NextStatus = customerStatusDisabled
		row.StatusActionKey = customerActionDisable
		row.StatusActionLabel = "停用"
	case customerStatusDisabled:
		row.Actionable = true
		row.NextStatus = customerStatusActive
		row.StatusActionKey = customerActionEnable
		row.StatusActionLabel = "启用"
	case customerStatusPending:

		row.PendingHintKey = userenums.LabelKeyPendingHint
		row.PendingHintFallback = userenums.LabelPendingHint
	}
	if item.Locked {
		row.LockHintKey = userenums.LabelKeyLockedHint
		row.LockHintFallback = userenums.LabelLockedHint
	} else if item.LoginFailureCount > 0 {
		row.LockHintKey = userenums.LabelKeyFailuresHint
		row.LockHintFallback = userenums.LabelFailuresHint
	}
	return row
}

func customerStatusText(c *gin.Context, status int) string {
	key, fallback := userenums.StatusLabel(status)
	return customerLabelText(c, userLabel{key: key, fallback: fallback})
}

func customerCounterTabs(counters userdto.CustomerCounters, filter customerFilter) []CounterTabResp {
	setStatus := func(v int) func(url.Values) {
		return func(q url.Values) {
			if v == customerStatusAll {
				q.Del("status")
				return
			}
			q.Set("status", strconv.Itoa(v))
		}
	}
	setVerified := func(v int) func(url.Values) {
		return func(q url.Values) {
			if v == userdto.EmailVerifiedAll {
				q.Del("emailVerified")
				return
			}
			q.Set("emailVerified", strconv.Itoa(v))
		}
	}

	allKey, allLabel := userenums.StatusLabel(customerStatusAll)
	activeKey, activeLabel := userenums.StatusLabel(customerStatusActive)
	disabledKey, disabledLabel := userenums.StatusLabel(customerStatusDisabled)
	pendingKey, pendingLabel := userenums.StatusLabel(customerStatusPending)
	lockedKey, lockedLabel := userenums.LabelKeyStatusLocked, userenums.LabelStatusLocked
	verifiedKey, verifiedLabel := userenums.EmailVerifiedLabel(true)
	unverifiedKey, unverifiedLabel := userenums.EmailVerifiedLabel(false)
	tabs := []struct {
		Key      string
		LabelKey string
		Label    string
		Count    int64
		Active   bool
		Apply    func(url.Values)
	}{
		{"all", allKey, allLabel, counters.Total,
			filter.Status == customerStatusAll && filter.EmailVerified == userdto.EmailVerifiedAll && !filter.Locked,
			func(q url.Values) { q.Del("status"); q.Del("emailVerified"); q.Del("locked") }},
		{"active", activeKey, activeLabel, counters.Active,
			filter.Status == customerStatusActive, setStatus(customerStatusActive)},
		{"disabled", disabledKey, disabledLabel, counters.Disabled,
			filter.Status == customerStatusDisabled, setStatus(customerStatusDisabled)},
		{"pending", pendingKey, pendingLabel, counters.Pending,
			filter.Status == customerStatusPending, setStatus(customerStatusPending)},
		{"locked", lockedKey, lockedLabel, counters.Locked,
			filter.Locked, func(q url.Values) { q.Set("locked", "1") }},
		{"verified", verifiedKey, verifiedLabel, counters.Verified,
			filter.EmailVerified == userdto.EmailVerifiedYes, setVerified(userdto.EmailVerifiedYes)},
		{"unverified", unverifiedKey, unverifiedLabel, counters.Unverified,
			filter.EmailVerified == userdto.EmailVerifiedNo, setVerified(userdto.EmailVerifiedNo)},
	}
	out := make([]CounterTabResp, 0, len(tabs))
	for _, t := range tabs {
		q := url.Values{}
		for k, val := range customerFilterValues(filter) {
			if val != "" {
				q.Set(k, val)
			}
		}
		t.Apply(q)
		target := customerListPath
		if len(q) > 0 {
			target += "?" + q.Encode()
		}

		out = append(out, CounterTabResp{
			Key: t.Key, LabelKey: t.LabelKey, Label: t.Label,
			Count: t.Count, Active: t.Active, URL: target,
		})
	}
	return out
}

func customerFilterActive(filter customerFilter) bool {
	return strings.TrimSpace(filter.Keyword) != "" ||
		filter.Status != customerStatusAll ||
		filter.EmailVerified != userdto.EmailVerifiedAll ||
		filter.Locked ||
		strings.TrimSpace(filter.RegisteredFrom) != "" ||
		strings.TrimSpace(filter.RegisteredTo) != ""
}

func customerFilterValues(filter customerFilter) map[string]string {
	return map[string]string{
		"keyword":        filter.Keyword,
		"status":         customerStatusQueryValue(filter.Status),
		"emailVerified":  customerVerifiedQueryValue(filter.EmailVerified),
		"locked":         customerLockedQueryValue(filter.Locked),
		"registeredFrom": filter.RegisteredFrom,
		"registeredTo":   filter.RegisteredTo,

		"segment":     filter.Segment,
		"minOrders":   customerMinOrdersQueryValue(filter.MinOrders),
		"rfm":         filter.RfmSegment,
		"tier":        customerTierQueryValue(filter.TierID),
		"segmentFrom": filter.SegmentFrom,
		"segmentTo":   filter.SegmentTo,
	}
}

func customerTierQueryValue(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func customerMinOrdersQueryValue(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func customerStatusQueryValue(status int) string {
	if status == customerStatusAll {
		return ""
	}
	return strconv.Itoa(status)
}

func customerLockedQueryValue(locked bool) string {
	if !locked {
		return ""
	}
	return "1"
}

func customerVerifiedQueryValue(v int) string {
	if v == userdto.EmailVerifiedAll {
		return ""
	}
	return strconv.Itoa(v)
}

func customerStatusMessage(status int) string {
	if status == customerStatusActive {
		return userenums.MsgCustomerEnabled
	}
	return userenums.MsgCustomerDisabled
}

func customerTotal(list *userdto.CustomerListResp) int64 {
	if list == nil {
		return 0
	}
	return list.Total
}

func customerTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return customerEmptyField
	}
	return value
}

type Handle struct {
	svc *userservice.Service
}

func NewHandle(svc *userservice.Service) *Handle { return &Handle{svc: svc} }

func clientIP(c *gin.Context) string {
	return strings.TrimSpace(c.ClientIP())
}

func locale(c *gin.Context) string {
	if v := strings.TrimSpace(c.Query("lang")); v != "" {
		return v
	}
	return "zh-CN"
}

const (
	ctxUserSession = "user_auth_session"
)

const touchThrottleInterval = 60 * time.Second

var (
	touchMu sync.Mutex

	touchSeen = map[string]time.Time{}
)

func shouldTouch(sessionHash string) bool {
	if strings.TrimSpace(sessionHash) == "" {
		return false
	}
	now := time.Now()
	touchMu.Lock()
	defer touchMu.Unlock()
	if last, ok := touchSeen[sessionHash]; ok && now.Sub(last) < touchThrottleInterval {
		return false
	}
	touchSeen[sessionHash] = now

	if len(touchSeen) > 4096 {
		for id, t := range touchSeen {
			if now.Sub(t) > 10*touchThrottleInterval {
				delete(touchSeen, id)
			}
		}
	}
	return true
}

func attachUserSession(svc *userservice.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := readUserToken(c)
		if token == "" {
			c.Next()
			return
		}
		sess, _ := svc.ResolveSession(c.Request.Context(), token)
		if sess != nil {
			c.Set(ctxUserSession, sess)

			c.Set(userSessionTokenKey, token)
			if shouldTouch(crypto.Sha256(token)) {
				svc.TouchSession(c.Request.Context(), sess)
			}
		}
		c.Next()
	}
}

func currentSession(c *gin.Context) *userservice.UserAuthSession {
	v, ok := c.Get(ctxUserSession)
	if !ok {
		return nil
	}
	s, ok := v.(*userservice.UserAuthSession)
	if !ok {
		return nil
	}
	return s
}

func currentToken(c *gin.Context) string {
	if v, ok := c.Get(userSessionTokenKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return readUserToken(c)
}

const (
	userSessionCookieName = "gowp_user_session"

	userSessionTokenKey = "user_token"

	userCSRFKey = "csrf_token"

	userSessionMaxAge         = 24 * 60 * 60
	userSessionRememberMaxAge = 7 * 24 * 60 * 60
)

var (
	userStoreMu     sync.RWMutex
	userCookieStore sessions.Store
)

func setupUserCookieStore() error {
	store, err := auth.NamedCookieStore(userSessionCookieName)
	if err != nil {
		return err
	}
	userStoreMu.Lock()
	userCookieStore = store
	userStoreMu.Unlock()
	return nil
}

func userSession(c *gin.Context) (*gsessions.Session, error) {
	userStoreMu.RLock()
	store := userCookieStore
	userStoreMu.RUnlock()
	if store == nil {
		return nil, errors.New("访客会话存储未初始化")
	}
	return store.Get(c.Request, userSessionCookieName)
}

func readUserToken(c *gin.Context) string {
	sess, err := userSession(c)
	if err != nil {
		return ""
	}
	raw, ok := sess.Values[userSessionTokenKey].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}

func writeUserToken(c *gin.Context, token string, rememberMe bool) error {
	sess, err := userSession(c)
	if err != nil {
		return err
	}
	sess.Values[userSessionTokenKey] = token
	if rememberMe {
		sess.Options.MaxAge = userSessionRememberMaxAge
	} else {
		sess.Options.MaxAge = userSessionMaxAge
	}
	return sess.Save(c.Request, c.Writer)
}

func VisitorIdentityMiddleware(svc usercontract.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {

		if csrf, err := builtin.EnsureCSRFTokenWith(c, userCSRFStore{}); err == nil && csrf != "" {
			c.Set(usercontract.VisitorCSRFContextKey, csrf)
		}
		if svc == nil {
			c.Next()
			return
		}
		token := readUserToken(c)
		if token == "" {
			c.Next()
			return
		}
		if id, ok := svc.ResolveVisitorID(c.Request.Context(), token); ok && id != 0 {
			c.Set(usercontract.VisitorContextKey, id)
		}

		c.Set(usercontract.VisitorTokenContextKey, token)
		c.Next()
	}
}

func clearUserSession(c *gin.Context) error {
	sess, err := userSession(c)
	if err != nil {

		return nil
	}
	sess.Values = make(map[interface{}]interface{})
	sess.Options.MaxAge = -1
	sess.Options.Path = "/"
	return sess.Save(c.Request, c.Writer)
}

type userCSRFStore struct{}

func (userCSRFStore) Get(c *gin.Context) string {
	sess, err := userSession(c)
	if err != nil {
		return ""
	}
	raw, ok := sess.Values[userCSRFKey].(string)
	if !ok {
		return ""
	}
	return raw
}

func (userCSRFStore) Save(c *gin.Context, token string) error {
	sess, err := userSession(c)
	if err != nil {
		return err
	}
	sess.Values[userCSRFKey] = token
	return sess.Save(c.Request, c.Writer)
}

type customerMembershipView struct {
	HasMembership bool

	TierName string

	IsDefaultTier bool

	FreeShipping bool

	DiscountPercent int64

	Notice customerMsg
}

func (h *customerPageHandle) SetMembershipDisplay(reader membershipcontract.Reader) {
	if h == nil {
		return
	}
	h.membership = reader
}

func (h *customerPageHandle) membershipView(ctx context.Context, c *gin.Context, projectID string, userID uint64) customerMembershipView {
	if h.membership == nil {

		return customerMembershipView{Notice: customerMembershipUnavailableLabel.msg()}
	}
	if userID == 0 || projectID == "" {
		return customerMembershipView{Notice: customerMembershipNoProjectLabel.msg()}
	}
	member, err := h.membership.Resolve(ctx, &membershipdto.ResolveReq{ProjectID: projectID, UserID: userID})
	if err != nil || member == nil {

		return customerMembershipView{Notice: h.membershipFacingMsg(c, err)}
	}
	return customerMembershipView{
		HasMembership:   true,
		TierName:        member.TierName,
		IsDefaultTier:   member.IsDefaultTier,
		FreeShipping:    member.FreeShipping,
		DiscountPercent: member.DiscountPercent,
	}
}

func (h *customerPageHandle) membershipFacingMsg(c *gin.Context, err error) customerMsg {
	if err == nil {
		return customerMembershipFailedLabel.msg()
	}
	if text := strings.TrimSpace(response.MessageOf(c, "user", err)); text != "" {
		return customerMsg{Fallback: text}
	}
	return customerMembershipFailedLabel.msg()
}

func applyCustomerMembership(dst *CustomerDetailPageResp, view customerMembershipView) {
	dst.MembershipNoticeKey = view.Notice.Key
	dst.MembershipNoticeFallback = view.Notice.Fallback
	dst.HasMembershipNotice = !view.Notice.Empty()
	dst.HasMembership = view.HasMembership
	dst.MembershipTierName = view.TierName
	dst.MembershipIsDefault = view.IsDefaultTier
	dst.MembershipFreeShipping = view.FreeShipping
	dst.MembershipDiscountPercent = view.DiscountPercent
}

var (
	customerMembershipUnavailableLabel = userLabel{userenums.KeyCustomerMembershipUnavailable, "会员模块尚未接入，这里看不到等级。"}
	customerMembershipNoProjectLabel   = userLabel{userenums.KeyCustomerMembershipNoProject, "还没有站点工程：会员等级按工程计算，选定工程后才能看到。"}
	customerMembershipFailedLabel      = userLabel{userenums.KeyCustomerMembershipFailed, "会员等级暂时读不出来 —— 客户资料本身不受影响。"}
)

func customerDetailBackURL(c *gin.Context) string {
	q := url.Values{}
	if c != nil {
		for _, key := range []string{"keyword", "status", "emailVerified", "locked", "registeredFrom", "registeredTo", "page", "limit"} {
			if v := strings.TrimSpace(c.Query(key)); v != "" {
				q.Set(key, v)
			}
		}
	}
	if len(q) == 0 {
		return customerListPath
	}
	return customerListPath + "?" + q.Encode()
}

const userErrScene = "user"

func customerBulkIDsText(c *gin.Context, err error) string {
	return adminui.BulkIDsFacingText(c, err)
}

func customerQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func customerPageStatus(raw string) int {
	switch strings.TrimSpace(raw) {
	case "0":
		return customerStatusDisabled
	case "1":
		return customerStatusActive
	case "2":
		return customerStatusPending
	default:
		return customerStatusAll
	}
}

func customerPageLocked(raw string) bool {
	return strings.TrimSpace(raw) == "1"
}

func customerPageEmailVerified(raw string) int {
	switch strings.TrimSpace(raw) {
	case "1":
		return userdto.EmailVerifiedYes
	case "2":
		return userdto.EmailVerifiedNo
	default:
		return userdto.EmailVerifiedAll
	}
}

func customerPageDayStart(raw string) *time.Time {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil
	}

	day, err := time.ParseInLocation("2006-01-02", v, sitetz.Location())
	if err != nil {
		return nil
	}
	return &day
}

func customerPageDayEnd(raw string) *time.Time {
	day := customerPageDayStart(raw)
	if day == nil {
		return nil
	}
	end := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return &end
}

func toAnySlice[T any](in []T) []any {
	out := make([]any, 0, len(in))
	for i := range in {
		out = append(out, in[i])
	}
	return out
}
