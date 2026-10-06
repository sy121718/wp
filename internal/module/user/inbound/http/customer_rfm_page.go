package userhttp

// customer_rfm_page.go — RFM 分析页（GET /admin/customers/rfm）。
//
// 这一页回答「这些人各自值多少」：把区间内下过单的人按 R（多久没来）/ F（来了几次）/
// M（花了多少）各打 1-5 分，总分决定分段（高价值 / 潜力 / 一般）。
//
// **打分是相对的**：每一维的五分位都按当次查询的那批人算，所以页面上的分数必须
// 与「分位」两个字一起读 —— 一个人在这个区间是 R=5，换一个区间可能只有 R=3。
// 这一点写在页头说明里，而不是只留在代码注释里（否则它一定会被读成绝对等级）。
//
// 客户名来自客户模块（订单模块只交 id）：RFM 的口径在订单侧、客户的资料在客户侧，
// 页面负责把两边拼起来 —— 这也是这一页属于「客户模块的页面」而不是订单模块的原因。

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	userdto "go_wp/internal/module/user/dto"
	"go_wp/internal/web/shell"
)

// customerRfmPath 页面路径（菜单 seed 与导航高亮都用它）。
const customerRfmPath = "/admin/customers/rfm"

// customerRfmTitleLabel 页标题（词条 key + 兜底文案）。
var customerRfmTitleLabel = userLabel{"admin.customer.rfm.title", "RFM 分析"}

// customerRfmSegmentLabel 分段 → 词条 key 与兜底文案。
//
// 与订单侧的白名单同字面量（那边是唯一的判定处，这里只做展示映射）：
// 认不出的分段原样显示，而不是渲染成空白 —— 口径变了而这里没跟上时，
// 页面上出现一个陌生的英文词比出现一个空单元格更容易被发现。
func customerRfmSegmentText(tr func(key, fallback string) string, segment string) string {
	switch segment {
	case "vip":
		return tr("admin.customer.rfm.segment.vip", "高价值")
	case "potential":
		return tr("admin.customer.rfm.segment.potential", "潜力")
	case "low_value":
		return tr("admin.customer.rfm.segment.lowValue", "一般")
	}
	return segment
}

// customerRfmRow 明细表的一行（客户资料 + RFM 分数）。
type customerRfmRow struct {
	UserID        int64
	Name          string
	Email         string
	DetailURL     string
	LastOrderAt   string
	RecencyDays   int
	Frequency     int64
	MonetaryLabel string
	RScore        int
	FScore        int
	MScore        int
	TotalScore    int
	Segment       string
	SegmentLabel  string
}

// customerRfmPageData 组装渲染数据（纯函数：不取数、不依赖 gin.Context）。
func customerRfmPageData(tr func(key, fallback string) string, rng customerOverviewRange,
	res *orderdto.CustomerRfmResp, rows []customerRfmRow,
	segment string, page, limit int, total int64, errText string) gin.H {
	data := gin.H{
		"title":   userLabelOf(tr, customerRfmTitleLabel),
		"Path":    customerRfmPath,
		"Range":   rng,
		"Segment": segment,
		"Err":     errText,
		"Rows":    rows,
		"Page":    page,
		"Limit":   limit,
		"Total":   total,
		// RfmReady 与「有数据」是两件事：未接线时给一句人话，而不是三格 0
		//（0 会被读成「这段时间一个客户都没有」）。同其它页面的降级口径。
		"RfmReady": res != nil,
	}
	if res == nil {
		return data
	}
	data["Customers"] = res.Customers
	data["Vip"] = res.Vip
	data["Potential"] = res.Potential
	data["LowValue"] = res.LowValue
	return data
}

// customerRfmSegmentTabs 分段筛选徽章（与客户列表的计数徽章同一条口径：
// 一个徽章一个链接，URL 由服务端拼好，条件之间可叠加）。
func customerRfmSegmentTabs(tr func(key, fallback string) string, res *orderdto.CustomerRfmResp, active string, rng customerOverviewRange) []gin.H {
	if res == nil {
		return nil
	}
	mk := func(key, labelKey, label string, count int64) gin.H {
		url := customerRfmPath + "?" + customerRangeQuery(rng)
		if key != "" {
			url += "&segment=" + key
		}
		return gin.H{
			"Key": key, "LabelKey": labelKey, "Label": label,
			"Count": count, "Active": key == active,
			"Badge": customerRfmBadge(key), "URL": url,
		}
	}
	return []gin.H{
		mk("", "admin.customer.rfm.tab.all", "全部", res.Customers),
		mk("vip", "admin.customer.rfm.segment.vip", "高价值", res.Vip),
		mk("potential", "admin.customer.rfm.segment.potential", "潜力", res.Potential),
		mk("low_value", "admin.customer.rfm.segment.lowValue", "一般", res.LowValue),
	}
}

// customerRfmBadge 分段 → 徽章色调（与列表页的计数徽章同一套类名）。
func customerRfmBadge(key string) string {
	switch key {
	case "vip":
		return "badge-success"
	case "potential":
		return "badge-info"
	case "low_value":
		return "badge-mute"
	}
	return ""
}

// customerRfmPageSegment 分段查询值 → 已知分段（认不出一律回落「全部」）。
func customerRfmPageSegment(v string) string {
	switch v {
	case "vip", "potential", "low_value":
		return v
	}
	return ""
}

// CustomerRfmPage RFM 分析（GET /admin/customers/rfm）。
func (h *customerPageHandle) CustomerRfmPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())
	segment := customerRfmPageSegment(c.Query("segment"))
	page, limit := shell.PageParams(c)
	if limit > 0 && limit > rfmMaxPageSize {
		limit = rfmMaxPageSize
	}

	var res *orderdto.CustomerRfmResp
	var errText string
	switch {
	case h.rfm == nil:
		errText = customerRfmUnavailableText
	case h.projects == nil:
		errText = customerRfmUnavailableText
	default:
		list, perr := h.projects.List(ctx)
		if perr != nil {
			errText = customerRfmUnavailableText
			break
		}
		if len(list) == 0 {
			// 还没建站点工程：没有订单可算 —— 正常状态，结果是空报表。
			errText = ""
			break
		}
		out, rerr := h.rfm.CustomerRfmByRange(ctx, &orderdto.CustomerRfmReq{
			ProjectID: list[0].ID,
			From:      rng.From,
			To:        rng.To,
			Segment:   segment,
			Limit:     limit,
			Offset:    (page - 1) * limit,
		})
		if rerr != nil {
			errText = customerRfmUnavailableText
		} else {
			res = out
		}
	}

	rows := h.customerRfmRows(ctx, tr, res)
	var total int64
	if res != nil {
		total = res.Total
	}
	data := shell.Prepare(c, customerRfmPageData(tr, rng, res,
		rows, segment, page, limit, total, errText))
	data["SegmentTabs"] = customerRfmSegmentTabs(tr, res, segment, rng)
	// 分页链接保留区间与分段：不带的话翻页会静默变成「全部区间 + 全部分段」。
	base := customerRfmPath + "?" + customerRangeQuery(rng)
	if segment != "" {
		base += "&segment=" + segment
	}
	for k, v := range shell.BuildPagination(total, page, limit, base, tr).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/user/customer_rfm", data)
}

// customerRfmRows 把 RFM 明细补上客户资料（姓名 / 邮箱 / 详情链接）。
//
// 取不到资料时**保留这一行并显示 id**：整行丢掉会让表格少人而没有任何提示，
// 而「订单侧有这个人、客户侧查不到」正是需要被看见的异常（账号被注销）。
func (h *customerPageHandle) customerRfmRows(ctx context.Context, tr func(key, fallback string) string, res *orderdto.CustomerRfmResp) []customerRfmRow {
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
			SegmentLabel:  customerRfmSegmentText(tr, it.Segment),
			DetailURL:     customerListPath + "/detail?id=" + strconv.FormatInt(it.UserID, 10),
		}
		if info, ok := names[it.UserID]; ok {
			row.Name = info.Name
			row.Email = info.Email
		}
		rows = append(rows, row)
	}
	return rows
}

// rfmMaxPageSize 明细分页上限（与订单侧的上限同值）。
const rfmMaxPageSize = 200

// customerRfmUnavailableText 取数不可用时的提示（归口文案，不外泄内部错误）。
const customerRfmUnavailableText = "RFM 分析暂时不可用（数据没接上），请稍后再试。"
