package runtimefragment

// orders.go — 访客订单片段（BIZ-1 访问面）。
//
// 为什么这件事只能走片段：站点是**已编译的静态产物**，同一份 HTML 发给所有访客，
// 而「我下过哪些单」每个人都不同 —— 静态页面给不了这个。
//
// 与购物车的关键差别：购物车是「还没决定买什么」，可以放在客户端 cookie 里；
// 订单是**已发生的事实**，只能从服务端按身份取，而身份只能来自访客会话。
// 未登录时片段**不报 401**，而是渲染一句引导：「登录后就看到了」——
// 401 会让 HTMX 静默不替换目标节点，访客看到的是一个毫无变化的页面。
//
// 归属校验不在这里：片段把 userID 交给 order 模块的 VisitorOrderReader，
// 由它在 SQL 条件里收口。本层唯一的责任是「把身份如实传下去」。

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/templates"

	"github.com/google/uuid"
)

// visitorOrders 访客订单查询能力（装配期注入）。
//
// 装配自检（审计 CQ-019）：判为 required-port —— 实现（orderSvc）在 routes.go 里
// 恒定可得，为空只可能是装配被改坏。nil 分支仅服务单测，表现为订单列表 / 详情
// 永远只显示「服务暂不可用」，与「今天订单服务挂了」在页面上无法区分。
var visitorOrders ordercontract.VisitorOrderReader

// SetVisitorOrderReader 注入访客订单查询能力（装配期调用；**必须注入**，见字段注释）。
func SetVisitorOrderReader(r ordercontract.VisitorOrderReader) { visitorOrders = r }

func init() {
	Register(Spec{Type: "ordersList", Method: "GET", Auth: AuthAnonymous, Render: renderOrdersList})
	Register(Spec{Type: "orderDetail", Method: "GET", Auth: AuthAnonymous, Render: renderOrderDetail})
}

const (
	// msgOrdersUnavailable 装配缺失时的提示（与 cartenums 的文案区分：这不是业务结果）。
	msgOrdersUnavailable = "订单查询尚未接入"
	// defaultOrdersPageSize 一屏订单数；maxOrdersPageSize 上限（分页参数来自 URL）。
	defaultOrdersPageSize = 10
	maxOrdersPageSize     = 50
)

// orderStatusFilter 状态筛选页签的取值（顺序即展示顺序）。
var orderStatusFilter = []string{"", "pending", "paid", "shipped", "completed", "cancelled", "refunded"}

// orderListItem 列表里的一行（模板直接读这些字段，不做任何算术）。
type orderListItem struct {
	ID          uint64
	OrderNo     string
	Status      string
	StatusLabel string
	TotalLabel  string
	TimeLabel   string
	ItemURL     string // 展开详情的片段地址（HTMX）
}

// orderStatusTab 状态筛选页签。
type orderStatusTab struct {
	Value  string
	Label  string
	Active bool
	// URL 无 JS 时的落点：订单页的线上路径 + 查询参数（真实可点，不是伪链接）。
	URL string
	// FragmentURL 有 HTMX 时的落点：同一份条件的**片段**地址（局部替换，不整页跳）。
	// 两个地址必须带同一组条件，否则「点一下」与「刷新一次」会看到不同的列表。
	FragmentURL string
}

// ordersFragmentData 订单列表片段数据。
type ordersFragmentData struct {
	FragmentType    string
	ProjectID       string
	Notice          string // 非空时只渲染这句话（未接入 / 业务错误）
	NeedLogin       bool
	LoginURL        string
	ShopURL         string
	Items           []orderListItem
	Status          string
	Tabs            []orderStatusTab
	Total           int64
	HasPrev         bool
	HasNext         bool
	PrevURL         string
	PrevFragmentURL string
	NextURL         string
	NextFragmentURL string
	PageSize        int
	TotalCountLabel string
	Labels          orderListLabels
}

// orderDetailFragmentData 订单详情片段数据。
type orderDetailFragmentData struct {
	FragmentType string
	// ProjectID / OrderID 退货表单要回传它们（片段端点据此定位工程与订单）。
	ProjectID     string
	OrderID       uint64
	Notice        string
	NeedLogin     bool
	LoginURL      string
	OrderNo       string
	Status        string
	StatusLabel   string
	TotalLabel    string
	DiscountLabel string
	ShippingLabel string
	TimeLabel     string
	PayMethod     string
	PaidAtLabel   string
	Address       string
	Remark        string
	Items         []orderDetailItem
	Logs          []orderDetailLog

	// ── 退货（BIZ-1）──────────────────────────────────────────────
	// CanRequestReturn 这单现在还能不能申请退货（有可退数量 且 订单状态允许）。
	CanRequestReturn bool
	// ReturnRequestID 提交表单的一次性幂等键：页面渲染时生成，重复提交只落一张申请单。
	ReturnRequestID string
	// ReturnActionURL 退货申请片段的地址（表单 action 与 hx-post 共用）。
	ReturnActionURL string
	// ReturnableTotal 整单还能退的总件数（0 时不给表单，只给说明）。
	ReturnableTotal int
	// Returns 已有的退货申请（含状态），按时间倒序列出。
	Returns []orderReturnSummary
	// NoticeOK 非空时是一句成功提示（提交成功后原地重渲染详情页，用户不必刷新）。
	NoticeOK string
	Labels   orderDetailLabels
}

// orderReturnSummary 订单详情里的一条退货申请摘要。
type orderReturnSummary struct {
	ReturnNo    string
	StatusLabel string
	Reason      string
	RefundLabel string
	TimeLabel   string
}

type orderDetailItem struct {
	// OrderItemID 退货表单要用它标识「退哪一行」（服务端据此校验行属于本单）。
	OrderItemID uint64
	// Returnable 该行当前可退数量（输入框的 max；0 表示这行不能再退）。
	Returnable    int
	ReturnQtyHint string
	ProductName   string
	VariantLabel  string
	SKU           string
	UnitPrice     string
	Quantity      int
	LineTotal     string
}

type orderDetailLog struct {
	FromStatus   string
	FromLabel    string
	ToStatus     string
	ToLabel      string
	OperatorName string
	Remark       string
	TimeLabel    string
}

// renderOrdersList 访客订单列表。
func renderOrdersList(ctx context.Context, r *Request) (string, error) {
	projectID := paramOf(r, "projectId")
	slots := cartSitePages(r, projectID)
	labels := orderListLabelsOf(r)
	data := ordersFragmentData{
		FragmentType: r.Type,
		ProjectID:    projectID,
		LoginURL:     slots[pageenums.SiteSlotLogin],
		ShopURL:      slots[pageenums.SiteSlotShop],
		Status:       strings.TrimSpace(paramOf(r, "status")),
		PageSize:     ordersPageSizeOf(r),
		Labels:       labels,
	}
	uid, ok := visitorIDOf(r)
	if !ok {
		data.NeedLogin = true
		return templates.RenderFragment("order_list", data)
	}
	if visitorOrders == nil {
		data.Notice = labels.ReaderUnavailable
		return templates.RenderFragment("order_list", data)
	}
	offset := ordersOffsetOf(r)
	res, err := visitorOrders.ListVisitorOrders(ctx, &orderdto.VisitorOrderListReq{
		ProjectID: projectID,
		Status:    data.Status,
		Offset:    offset,
		Limit:     data.PageSize,
		UserID:    uid,
	})
	if err != nil {
		data.Notice = orderUserMessage(r, err)
		return templates.RenderFragment("order_list", data)
	}
	if res != nil {
		data.Total = res.Total
		for _, o := range res.List {
			data.Items = append(data.Items, orderListItem{
				ID:          o.ID,
				OrderNo:     o.OrderNo,
				Status:      o.Status,
				StatusLabel: orderStatusLabelOf(r, o.Status),
				TotalLabel:  formatCentsLabel(r, o.Total),
				TimeLabel:   o.CreateTime.Format("2006-01-02 15:04"),
				ItemURL:     orderDetailURL(r, projectID, o.ID),
			})
		}
	}
	base := slots[pageenums.SiteSlotOrders]
	data.Tabs = orderStatusTabs(r, projectID, base, data.Status)
	data.HasPrev = offset > 0
	prev := offset - data.PageSize
	if prev < 0 {
		prev = 0
	}
	data.PrevURL = orderListPageURL(base, data.Status, prev)
	data.PrevFragmentURL = orderListFragmentURL(r, projectID, data.Status, prev, data.PageSize)
	data.HasNext = offset+data.PageSize < int(data.Total)
	data.NextURL = orderListPageURL(base, data.Status, offset+data.PageSize)
	data.NextFragmentURL = orderListFragmentURL(r, projectID, data.Status, offset+data.PageSize, data.PageSize)
	data.TotalCountLabel = fmt.Sprintf(labels.TotalCount, data.Total)
	return templates.RenderFragment("order_list", data)
}

// renderOrderDetail 访客订单详情（订单项 + 状态流转链 + 退货区）。
func renderOrderDetail(ctx context.Context, r *Request) (string, error) {
	return renderOrderDetailWith(ctx, r, "")
}

// renderOrderDetailWith 渲染订单详情；noticeOK 非空时额外交给模板一句成功提示。
//
// 退货提交成功后**原地重渲染详情**（而不是返回一个小提示片段）：用户刚申请完，
// 最想看的是「这单现在什么状态」。多一次查询换掉一次「自己刷新一下」。
func renderOrderDetailWith(ctx context.Context, r *Request, noticeOK string) (string, error) {
	projectID := paramOf(r, "projectId")
	slots := cartSitePages(r, projectID)
	labels := orderDetailLabelsOf(r)
	data := orderDetailFragmentData{
		FragmentType:    r.Type,
		ProjectID:       projectID,
		LoginURL:        slots[pageenums.SiteSlotLogin],
		NoticeOK:        noticeOK,
		ReturnRequestID: uuid.NewString(),
		ReturnActionURL: "returnRequest",
		Labels:          labels,
	}
	uid, ok := visitorIDOf(r)
	if !ok {
		data.NeedLogin = true
		return templates.RenderFragment("order_detail", data)
	}
	if visitorOrders == nil {
		data.Notice = labels.ReaderUnavailable
		return templates.RenderFragment("order_detail", data)
	}
	orderID, perr := strconv.ParseUint(strings.TrimSpace(paramOf(r, "orderId")), 10, 64)
	if perr != nil || orderID == 0 {
		data.Notice = fragmentUserMessage(r, orderenums.ErrInvalidParam)
		return templates.RenderFragment("order_detail", data)
	}
	res, err := visitorOrders.GetVisitorOrder(ctx, &orderdto.VisitorOrderDetailReq{
		OrderID:   orderID,
		ProjectID: projectID,
		UserID:    uid,
	})
	if err != nil {
		data.Notice = orderUserMessage(r, err)
		return templates.RenderFragment("order_detail", data)
	}
	if res == nil || res.Head == nil {
		data.Notice = fragmentUserMessage(r, orderenums.ErrOrderNotFound)
		return templates.RenderFragment("order_detail", data)
	}
	head := res.Head
	data.OrderID = head.ID
	data.OrderNo = head.OrderNo
	data.Status = head.Status
	data.StatusLabel = orderStatusLabelOf(r, head.Status)
	data.TotalLabel = formatCentsLabel(r, head.Total)
	data.DiscountLabel = formatCentsLabel(r, head.DiscountTotal)
	data.ShippingLabel = formatCentsLabel(r, head.ShippingTotal)
	data.TimeLabel = head.CreateTime.Format("2006-01-02 15:04")
	data.PayMethod = head.PaymentMethodTitle
	if head.PaidAt != nil {
		data.PaidAtLabel = head.PaidAt.Format("2006-01-02 15:04")
	}
	data.Address = orderAddressOf(head)
	data.Remark = head.Remark
	for _, it := range res.Items {
		data.Items = append(data.Items, orderDetailItem{
			OrderItemID:  it.ID,
			ProductName:  it.ProductName,
			VariantLabel: it.VariantLabel,
			SKU:          it.SKU,
			UnitPrice:    formatCentsLabel(r, it.UnitPrice),
			Quantity:     it.Quantity,
			LineTotal:    formatCentsLabel(r, it.LineTotal),
		})
	}
	// 退货区（BIZ-1）：可退数量按行取，已有申请按状态列出。
	// 读不到就当作「不可退」—— 页面少一个区块，胜过整段详情 500。
	if visitorReturns != nil {
		if rb, rerr := visitorReturns.ReturnableOfOrder(ctx, &orderdto.VisitorOrderDetailReq{
			OrderID: orderID, ProjectID: projectID, UserID: uid,
		}); rerr == nil && rb != nil {
			byItem := make(map[uint64]*orderdto.ReturnableItem, len(rb.Items))
			for _, x := range rb.Items {
				byItem[x.OrderItemID] = x
			}
			for i := range data.Items {
				if x, hit := byItem[data.Items[i].OrderItemID]; hit {
					data.Items[i].Returnable = x.Returnable
					data.Items[i].ReturnQtyHint = fmt.Sprintf(labels.ReturnQtyHint, data.Items[i].Quantity, x.Returnable)
				}
			}
			data.ReturnableTotal = rb.ReturnableTotal
		}
		if list, lerr := visitorReturns.ListVisitorReturns(ctx, &orderdto.VisitorReturnListReq{
			ProjectID: projectID, OrderID: orderID, UserID: uid, Limit: 20,
		}); lerr == nil && list != nil {
			for _, rt := range list.List {
				data.Returns = append(data.Returns, orderReturnSummary{
					ReturnNo:    rt.ReturnNo,
					StatusLabel: rt.StatusLabel,
					Reason:      rt.Reason,
					RefundLabel: rt.RefundLabel,
					TimeLabel:   rt.CreateTime.Format("2006-01-02 15:04"),
				})
			}
		}
	}
	// 能申请退货 = 有可退数量 且 订单状态允许（与 order 模块同一口径）。
	data.CanRequestReturn = data.ReturnableTotal > 0 && orderReturnableStatus(head.Status)
	for _, lg := range res.Logs {
		data.Logs = append(data.Logs, orderDetailLog{
			FromStatus:   lg.FromStatus,
			FromLabel:    orderStatusLabelOf(r, lg.FromStatus),
			ToStatus:     lg.ToStatus,
			ToLabel:      orderStatusLabelOf(r, lg.ToStatus),
			OperatorName: lg.OperatorName,
			Remark:       lg.Remark,
			TimeLabel:    lg.CreateTime.Format("2006-01-02 15:04"),
		})
	}
	return templates.RenderFragment("order_detail", data)
}

// visitorIDOf 取本次请求的访客身份（未登录返回 false）。
func visitorIDOf(r *Request) (uint64, bool) {
	if r == nil {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(r.UserID), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// ordersPageSizeOf 一屏订单数（参数来自 URL，越界即回落默认值）。
func ordersPageSizeOf(r *Request) int {
	n, err := strconv.Atoi(paramOf(r, "limit"))
	if err != nil || n <= 0 {
		return defaultOrdersPageSize
	}
	if n > maxOrdersPageSize {
		return maxOrdersPageSize
	}
	return n
}

// ordersOffsetOf 分页偏移（负数按 0 处理）。
func ordersOffsetOf(r *Request) int {
	n, err := strconv.Atoi(paramOf(r, "offset"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// orderReturnableStatus 允许申请退货的订单状态。
//
// 与 order 模块同一口径（见 return_request.go 的 returnableOrderStatuses）：
// 片段层不能 import 订单模块的 model，所以这里再写一份 —— 代价是新增状态要改两处，
// 收益是这条依赖方向不会被打穿（与状态标签同一取舍）。
func orderReturnableStatus(status string) bool {
	return status == "paid" || status == "shipped" || status == "completed"
}

// orderStatusTabs 状态筛选页签（含「全部」）。
//
// 每个页签给两个地址：页面地址（无 JS 时整页跳）与片段地址（HTMX 局部替换）。
// 两者带同一组条件 —— 只给其中一个，会出现「点一下看到的是筛选后的、刷新一下又回到全部」。
func orderStatusTabs(r *Request, projectID, baseURL, current string) []orderStatusTab {
	tabAll := orderListLabelsOf(r).TabAll
	tabs := make([]orderStatusTab, 0, len(orderStatusFilter))
	for _, v := range orderStatusFilter {
		label := tabAll
		if v != "" {
			label = orderStatusLabelOf(r, v)
		}
		tabs = append(tabs, orderStatusTab{
			Value:       v,
			Label:       label,
			Active:      v == current,
			URL:         orderListPageURL(baseURL, v, 0),
			FragmentURL: orderListFragmentURL(r, projectID, v, 0, 0),
		})
	}
	return tabs
}

// orderListFragmentURL 订单列表片段的地址（HTMX 局部刷新用）。
//
// pageSize 传 0 表示「不覆盖」：页签切换时不该把当前的每页条数重置成默认值，
// 而分页按钮则必须带上它，否则翻到第二页会变回默认条数。
func orderListFragmentURL(r *Request, projectID, status string, offset, pageSize int) string {
	url := "/_fragments/ordersList?projectId=" + projectID
	if status != "" {
		url += "&status=" + status
	}
	if offset > 0 {
		url += "&offset=" + strconv.Itoa(offset)
	}
	if pageSize > 0 {
		url += "&limit=" + strconv.Itoa(pageSize)
	}
	if lang := strings.TrimSpace(paramOf(r, "lang")); lang != "" {
		url += "&lang=" + lang
	}
	return url
}

// orderListPageURL 订单页 + 查询参数。baseURL 为空（站点还没指定订单页）时返回空串，
// 模板据此不输出链接 —— 不猜路径：猜错的链接比没有链接难查得多。
func orderListPageURL(baseURL, status string, offset int) string {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return ""
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	url := base + sep + "offset=" + strconv.Itoa(offset)
	if status != "" {
		url += "&status=" + status
	}
	return url
}

// orderDetailURL 详情片段的地址（HTMX 展开用）。
func orderDetailURL(r *Request, projectID string, orderID uint64) string {
	url := "/_fragments/orderDetail?projectId=" + projectID + "&orderId=" + strconv.FormatUint(orderID, 10)
	if lang := strings.TrimSpace(paramOf(r, "lang")); lang != "" {
		url += "&lang=" + lang
	}
	return url
}

// orderAddressOf 收货地址一行展示（空字段跳过，不留一串逗号）。
func orderAddressOf(o *orderdto.OrderResp) string {
	if o == nil {
		return ""
	}
	parts := make([]string, 0, 6)
	for _, p := range []string{o.ShipProvince, o.ShipCity, o.ShipDistrict, o.ShipAddress, o.ShipZip} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// orderUserMessage 把订单域错误映射成可原样给访客看的文案。
func orderUserMessage(r *Request, err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	for _, m := range orderenums.UserFacingMessages {
		if m == msg {
			return fragmentUserMessage(r, msg)
		}
	}
	return fragmentUserMessage(r, orderenums.ErrInternal)
}
