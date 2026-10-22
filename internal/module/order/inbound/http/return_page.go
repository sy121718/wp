package orderhttp

// return_page.go — 退货入库页（列表 / 详情 / 审核 / 收货 / 补退款 / 批量）的控制器。
//
// 分工与订单页、优惠码页一致（样板见 coupon_page.go）：控制器只做「绑定 → 调 service →
// 渲染」，文案 / 格式化 / 显示判断在模板里，dto 直接交给模板。
//
// 流程只有一条（页面上的说明也这么写）：**客户申请 → 审核 → 先入库、后退款**。
// 顺序不能反 —— 反过来就是「钱退了、货没回来」。所以这个页面没有「只退款」的按钮，
// 入库与退款由服务端在同一条用例里按顺序完成（幂等可重入：status=received 时再点
// 只会补做退款，不会重复加库存）。
//
// 显示投影（模板按它们决定渲染哪些表单，服务端仍是唯一裁决者）：
//
//   - 待审核：可「同意」（可勾 autoReceive 一步到底）或「拒绝」（理由必填）；
//   - 已同意待收货：可「确认收货并退货」；
//   - 已入库待退款：可「补退款」（同一个入口，服务端识别出入库已完成）；
//   - 其余（已完成 / 已拒绝 / 已撤销）：终态，没有可执行的操作。

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
)

// returnPageHandle 退货入库页处理器。
type returnPageHandle struct {
	orders     ordercontract.OrderService
	projects   projectcontract.ProjectService
	warehouses ordercontract.ReturnWarehouseSource
	dict       sysconfigcontract.DictReader
}

// NewReturnPageHandle 构造。
func NewReturnPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	warehouses ordercontract.ReturnWarehouseSource, dict sysconfigcontract.DictReader) *returnPageHandle {
	return &returnPageHandle{orders: orders, projects: projects, warehouses: warehouses, dict: dict}
}

// returnFilter 页面筛选条件（GET 参数，全部可选）。
type returnFilter struct {
	// Status 状态精确筛选（空 = 全部）。
	Status string
	// Keyword 关键词（退货单号 / 订单号 / 客户邮箱，服务端决定匹配哪些列）。
	Keyword string
	// OrderID 非 0 时只列该订单的退货申请（从订单详情跳过来时带）。
	OrderID uint64
	// ReturnID 非 0 时展开该退货单的详情（同一个页面靠查询参数切换，不新开路由）。
	ReturnID uint64
}

// returnStatusValues 状态维度的取值白名单（含空串 = 「全部」）。
//
// 顺序 = 徽章行里的顺序；文案由 Jet 用已解析的状态词条事实取词
// （真源是 orderenums.ReturnStatusLabel 的 `admin.returns.status.*` 词条）。
var returnStatusValues = []string{
	"", "requested", "approved", "received", "completed", "rejected", "cancelled",
}

type statusLabelFact struct {
	Key      string
	Fallback string
}

func statusLabelFacts(values []string, resolve func(string) (string, string)) map[string]statusLabelFact {
	facts := make(map[string]statusLabelFact, len(values))
	for _, value := range values {
		key, fallback := resolve(value)
		facts[value] = statusLabelFact{Key: key, Fallback: fallback}
	}
	return facts
}

// returnListQueryKeys 列表上下文的白名单键（渲染时拼 action / POST 回来时读回，两处同一份）。
//
// **含 returnId**：审核完停在原来那张单的详情上是用户要的（状态刚变，正好看结果）；
// 而状态徽章与翻页链接用不含 returnId 的基础串（「换一批单看」时把展开态带过去只会让人以为页面坏了）。
var returnListQueryKeys = []string{"project", "keyword", "orderId", "page", "limit", "returnId"}

// ReturnsPage 退货入库页（GET /admin/returns）。
func (h *returnPageHandle) ReturnsPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**：空列表 + 归口提示 + HTTP 200，
	// 页头 / 筛选器 / 批量条 / 分页壳与侧栏全部保留。
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
	filter := returnFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		OrderID:  orderQueryID(c.Query("orderId")),
		ReturnID: orderQueryID(c.Query("returnId")),
	}

	var returns []*orderdto.ReturnResp
	// 计数表**一开始就补齐零值**：装载失败 / 未查询时它是 nil，而模板会拿计数做数值比较
	// （`{{if pendingCount > 0}}`），nil 索引出来的值参与比较会让 Jet 报错、整页 500 ——
	// 「这一页没读出来」的降级路径反而打不开页面，是最不该发生的一种 500。
	counts := countsFilled(nil, returnStatusValues)
	var total int64
	if selected != "" {
		list, lerr := h.orders.ListReturns(ctx, &orderdto.ReturnListReq{
			ProjectID: selected,
			Status:    filter.Status,
			Keyword:   filter.Keyword,
			OrderID:   filter.OrderID,
			Offset:    (page - 1) * limit,
			Limit:     limit,
		})
		if lerr != nil {
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, lerr))
		} else {
			returns, counts, total = list.List, countsFilled(list.Counts, returnStatusValues), list.Total
		}
	}

	var detail *orderdto.ReturnDetailResp
	if selected != "" && filter.ReturnID > 0 {
		det, derr := h.orders.GetReturn(ctx, filter.ReturnID)
		if derr != nil {
			loadErrText = firstNonEmpty(loadErrText, returnFacingError(c, derr))
		} else {
			detail = det
		}
	}

	data := adminui.Prepare(c, gin.H{
		"title":           orderenums.ReturnsHeading,
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		// 取数结果：dto 原样交给模板。
		"Returns": returns,
		"Counts":  counts,
		"Total":   total,
		// 「全部」徽章的计数（各状态之和）：模板里跨类型累加（int + int64）不值得押注，
		// 求和放这里一行，模板只取结果。
		"CountsAll": countsAll(counts),
		"Detail":    detail,
		// 状态徽章行的「取值 + 分档」（含空串 = 全部）+ 当前生效值。
		"StatusTabs":         returnStatusTabs(),
		"FilterStatus":       filter.Status,
		"ReturnStatusLabels": returnStatusPageFacts(returnStatusValues, filter.Status, returns, detail),
		"OrderStatusLabels":  orderStatusPageFacts(orderStatusValues, detail),
		"FilterKeyword":      filter.Keyword,
		"FilterOrderID":      filter.OrderID,
		// 这一次没读出来的原因（空串 = 正常）：模板据此把「装载失败」与「还没有退货申请」分开。
		"LoadErr": loadErrText,
		// 列表上下文（拼进表单 action / 徽章链接；不含 status，见 returnListQueryKeys）。
		"ListQuery": returnListQuery(selected, filter, page, limit),
		// 入库仓下拉：只给启用的仓（停用的仓历史流水仍按 id 可读，但新入库不该再选它）。
		"Warehouses": h.enabledWarehouses(ctx, selected),
		// 状态 → 当前语言标签。enums 的 (key, fallback) 是两返回值，模板接不住，
		// 所以这里包一层单值函数进数据：**取词仍在模板调用点发生**，Go 不预先拼行视图。
		// 订单摘要里的收货地址要显示国家**标签**（模板调 .CountryLabel(o.ShipCountry)）。
		// 这一项必须显式给：模板是在 `{{if o.ShipAddress}}` 分支里调它的，而 Jet 对
		// 「数据里没有这个键就当作函数调用」的处理是渲染失败 → 整页 500 换成归口提示页。
		// 换句话说，只要选中一张**订单有地址**的退货单，审核入口就整个打不开 ——
		// 这是用 task-119 的链式证据（真渲染 + 真数据）才照出来的：只渲染 returnId=不存在的
		// 那类页时，detail 分支根本不进，缺陷一直是隐形的。
		"CountryLabel": countryLabelFunc(c, h.dict),
		"Page":         page,
		"Limit":        limit,
	})
	// 分页事实：基址由 backpath 拼（带白名单参数），故 path 传空 —— AttachPagination 不动 Base。
	adminui.AttachPagination(data, "", nil, pagination.Page{
		Total: total, Page: page, Limit: limit,
		Base: backpath.WithParams("/admin/returns", map[string]string{
			"project": selected, "status": filter.Status, "keyword": filter.Keyword,
		}),
	})
	c.HTML(http.StatusOK, "admin/order/returns.html", data)
}

// —— 请求工具 ——
//
// 写动作（同意 / 拒绝 / 收货 / 批量同意 / 批量拒绝）已全部迁到前端直调 JSON（task-98 S3）：
// 端点就是 order_router.go 里的 /api/order/return/{approve,reject,receive}，批量靠请求体形状
// 分流（见 return_bulk_write.go），页面不再有自己的写路径 —— 于是这个文件里不再需要
// returnDone / returnFail / returnFacing / returnFormBool / returnBulkRejectReasonRequired
// 这一整套「提示页 + 表单回跳」的机关，也不再需要页面对应的常量。
//
// 本页（GET）仍然只做「绑定 → 调 service → 渲染」。
// returnListQuery 列表上下文 → 查询串（拼进表单 action / 徽章链接；空值不拼）。
//
// 刻意**不含 status**：徽章链接要覆盖它、写表单要保留它（表单里那个隐藏域）。
func returnListQuery(projectID string, filter returnFilter, page, limit int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("keyword", filter.Keyword)
	if filter.OrderID > 0 {
		set("orderId", strconv.FormatUint(filter.OrderID, 10))
	}
	if page > 0 {
		set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

// returnStatusText 状态 → 展示文案：按状态**值**取词（不拿 service 给的中文标签反查）。
//
// 仅供 JSON 接口 localizeReturnLabels：响应里的 StatusLabel 按请求语言取词。
// 页面使用 statusLabelFacts 交给 Jet；未知状态保持原始值，空白状态显示「—」。
func returnStatusText(tr translate, status string) string {
	key, fallback := orderenums.ReturnStatusLabel(status)
	return orderLabelOf(tr, orderLabel{key, fallback})
}

// returnStatusPageFacts 退货状态事实表：取值白名单 + 生效筛选值 + 列表每一行 + 详情退货单。
//
// **生效筛选值必须收进来**：空列表时页面上唯一会显示的状态就是它（空态那行
// 「当前还带着状态筛选「…」」），漏掉它的症状是——筛选值合法时一切正常，
// 而筛选值是空的/未知的那几种情况下整页渲染报错（模板索引到缺键）。
func returnStatusPageFacts(values []string, filterStatus string, returns []*orderdto.ReturnResp, detail *orderdto.ReturnDetailResp) map[string]statusLabelFact {
	statuses := append([]string(nil), values...)
	statuses = append(statuses, filterStatus)
	for _, item := range returns {
		if item != nil {
			statuses = append(statuses, item.Status)
		}
	}
	if detail != nil {
		if detail.Return != nil {
			statuses = append(statuses, detail.Return.Status)
		}
	}
	return statusLabelFacts(statuses, orderenums.ReturnStatusLabel)
}

// orderStatusPageFacts 摘要订单状态事实表：取值白名单 + 详情里的订单状态。
//
// **两张表刻意分开**：退货状态与订单状态是两套枚举（key 前缀都不同），
// 合成一张表看着省事，但模板里 `orderLabels[o.Status]` 与 `returnLabels[r.Status]`
// 就只靠人眼区分了 —— 串表时一行状态会显示另一套词条的文本，而且不报错。
func orderStatusPageFacts(values []string, detail *orderdto.ReturnDetailResp) map[string]statusLabelFact {
	statuses := append([]string(nil), values...)
	if detail != nil && detail.Order != nil {
		statuses = append(statuses, detail.Order.Status)
	}
	return statusLabelFacts(statuses, orderenums.OrderStatusLabel)
}

// countsAll 各状态计数之和（「全部」徽章的那个数）。
func countsAll(counts map[string]int64) int64 {
	var all int64
	for _, v := range counts {
		all += v
	}
	return all
}

// orderStatusLabelFunc 订单状态 → 当前语言标签（订单列表与销售概览仍使用）。
//
// **Go 侧取词的遗留出口**：这两个页面把闭包当 data 交给模板
// （orders.html 的 `{{statusLabel := .OrderStatusLabel}}`、sales_overview.html 的
// `{{ .OrderStatusLabel(sv) }}`），于是取词发生时模板还看不到 key。
// 退货页那一批已经改成「状态 → 词条事实」交给 Jet 取词（见 statusLabelFacts），
// 这两处等模板槽位一起改（A 项遗留清单）。取词入口统一走 orderTr。
func orderStatusLabelFunc(c *gin.Context) func(string) string {
	tr := orderTr(c)
	return func(status string) string {
		key, fallback := orderenums.OrderStatusLabel(status)
		return tr(key, fallback)
	}
}

// enabledWarehouses 入库仓下拉的候选：只保留启用的仓（默认仓在最前，由库存侧排序保证）。
func (h *returnPageHandle) enabledWarehouses(ctx context.Context, projectID string) []ordercontract.ReturnWarehouse {
	if h.warehouses == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	list, err := h.warehouses.ListReturnWarehouses(ctx, projectID)
	if err != nil {
		// 仓列表读不出来只降级（下拉里没有选项，留空即默认仓）：审核本身不受影响。
		return nil
	}
	out := make([]ordercontract.ReturnWarehouse, 0, len(list))
	for _, w := range list {
		if strings.TrimSpace(w.ID) == "" {
			continue
		}
		if w.Status != "" && w.Status != "enabled" {
			continue
		}
		out = append(out, w)
	}
	return out
}

// —— 退货页自有的常量与文案白名单 ——

// returnFacingExtras 本页允许原样显示的**模块外**文案。
//
// orderenums.UserFacingMessages 是结算链路的白名单（那些文案会显示给访客），
// 后台审核侧的文案不在其中：同意 / 拒绝 / 入库成功的提示，以及几条只在后台出现的
// 状态错误 —— 不补这一层，运营点下去只会看到「系统内部错误」，
// 而真正的原因（「该申请不在待审核状态」）就丢了。
var returnFacingExtras = []string{
	orderenums.MsgReturnApproved, orderenums.MsgReturnRejected, orderenums.MsgReturnReceived,
	orderenums.ErrReturnRejectReasonRequired, orderenums.ErrReturnNotReviewable, orderenums.ErrReturnNotReceivable,
	orderenums.ErrReturnNotFound,
}

// returnFacingError 把 service 的错误收敛成可展示文案：命中白名单取词展示，
// 未命中记结构化日志并给归口文案（原文只进日志）。
func returnFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := returnFacingText(err.Error()); msg != "" {
		return i18n.TranslateFunc(response.RequestLanguage(c))(msg, msg)
	}
	logger.Scene("order-return").With("path", c.Request.URL.Path).Error(err, "退货页读取失败")
	return adminui.PageInternalText(c)
}

// returnFacingText 白名单判定：命中返回原文，未命中返回空串。
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
