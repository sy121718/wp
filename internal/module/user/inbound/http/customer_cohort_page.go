package userhttp

// customer_cohort_page.go — 群组留存（GET /admin/customers/cohort）。
//
// 这一页回答的是 RFM 回答不了的问题：**这段时间来的新人，之后还回不回来**。
// RFM 是横截面（此刻谁值多少），群组留存是纵向的（同一批人随时间怎么衰减）。
//
// 矩阵的形状（行 = 首单所在月，列 = 相对月序号）与每一格的取值全在订单模块
// （ordercontract.CustomerCohortReader）。本页不重算任何比例 —— 页头「共 N 个新客户」
// 必须与客户概览页的「新客」是同一个数，各算一次就会分叉。
//
// **空格的语义要在页面上说清楚**：还没到的月份是空白，不是 0%。两者混在一起时
// 最新几个月的留存率会显示成一片 0%，读起来像断崖式流失，而真实原因是时间还没到。

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

// customerCohortPath 页面路径（菜单 seed 与导航高亮都用它）。
const customerCohortPath = "/admin/customers/cohort"

// customerCohortTitleLabel 页标题（词条 key + 兜底文案）。
//
// 必须在 Go 侧设进 data：layout.html 的 `{{.title}}` 是**无条件读**的，
// shell.Prepare 的 injectI18n 只翻译已有非空字符串、不注入 —— 漏设的结果是
// 整页 500，而错误信息里只有模板行号，看不出是「少了这个键」。
var customerCohortTitleLabel = userLabel{"admin.customer.cohort.title", "群组留存"}

// customerCohortUnavailableLabel 端口缺席时的说明（不是「没有客户」——那是两种不同的空）。
var customerCohortUnavailableLabel = userLabel{
	"admin.customer.cohort.unavailable",
	"群组留存暂时不可用（数据没接上）。",
}

// CustomerCohortPage 群组留存（GET /admin/customers/cohort）。
func (h *customerPageHandle) CustomerCohortPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())

	// 工程：与客户概览页逐字同规则 —— 先看 ?project=，没有就用第一个；
	// 本页同样不渲染工程切换器（这一页的工程由从列表页带过来的上下文决定）。
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
	var errText string
	switch {
	case h.cohort == nil:
		errText = userLabelOf(tr, customerCohortUnavailableLabel)
	case selected == "":
		// 没有工程就没有订单可算 —— 「还没建站点工程」是正常状态，不是错误。
		errText = ""
	default:
		got, err := h.cohort.CustomerCohortByRange(ctx, &orderdto.CustomerCohortReq{
			ProjectID: selected,
			From:      rng.From,
			To:        rng.To,
		})
		if err != nil {
			errText = customerFacingError(c, err)
		} else {
			res = got
		}
	}

	data := customerOverviewPageData(tr, rng, nil, "")
	data["Path"] = customerCohortPath
	data["title"] = userLabelOf(tr, customerCohortTitleLabel)
	data["Err"] = errText
	data["CohortReady"] = res != nil
	// 列头在 Go 侧定（Jet 的 `{{range}}` 只能遍历集合，没有「遍历 0..N」这种写法）。
	// 列头用**相对月序号**而不是年月：每一行的首单月不同，同一列在各行是不同月份，
	// 把某一行的月份当列头会让其它行的格子对不上号。
	//
	// **三个键无论有没有数据都要设**（空切片 / 零值）：模板里会 `len()` 它们，
	// 而 Jet 对缺席的键（nil）求 len 是运行时错误 —— 症状是整页 500，
	// 而错误信息只有模板行号，看不出是「少了这个键」。colspan 同理，
	// 在 Go 侧算好（Jet 的算术是浮点，`{{len(x) + 2}}` 会渲染成「2」这种怪值）。
	cols := []gin.H{}
	rows := res
	if rows != nil {
		for k := 0; k < rows.Months; k++ {
			cols = append(cols, gin.H{"Index": k, "Text": strconv.Itoa(k)})
		}
	}
	data["Columns"] = cols
	data["Colspan"] = len(cols) + 2
	if res != nil {
		data["Cohorts"] = res.Cohorts
		data["Customers"] = res.Customers
		data["Months"] = res.Months
		data["Rows"] = res.Rows
	} else {
		data["Rows"] = []orderdto.CustomerCohortRow{}
	}
	c.HTML(200, "admin/user/customer_cohort", shell.Prepare(c, data))
}
