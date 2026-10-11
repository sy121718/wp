package orderhttp

// return_page_test.go — 退货页「状态标签 = 页面事实」的判据。
//
// 这一批把退货页的状态取词从 Go 闭包（shell.TranslateFor + 逐行调用）改成**事实层**：
// Go 只给出「原始状态值 → 已解析的 (key, fallback)」，取词由 Jet 在渲染点用 tr 完成。
// 于是有两条必须钉住的东西，而它们各自都有「看着绿其实没测到」的失败模式：
//
//  1. **事实必须逐字等于枚举真源**。若是页面自己抄一份中文表，改词条时页面上是旧说法、
//     测试也照样绿 —— 所以断言里先取 orderenums 的返回值，再要求事实与它逐字相等。
//  2. **Jet 必须真的按状态值索引到那张表**。只测 Go 侧构造出的 map，等于没验证模板：
//     `returnLabels[r.Status]` 写错键（少了原始空白、或写成小写）在源码扫描里看不出来，
//     症状是那一格变空白。所以下面用**真实 handler + 真实 Jet 渲染**跑出整页字节。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

// returnLabelsService 只实现退货页真正调用的两个读方法（其余接口方法由嵌入的
// nil 接口兜底，被调到即 panic —— 页面多读一次数据会在这里立刻暴露，而不是静默多查一遍库）。
type returnLabelsService struct {
	ordercontract.OrderService
	list      *orderdto.ReturnListResp
	detail    *orderdto.ReturnDetailResp
	listErr   error
	detailErr error
}

func (s *returnLabelsService) ListReturns(_ context.Context, _ *orderdto.ReturnListReq) (*orderdto.ReturnListResp, error) {
	// 真实的 service 永不返回 (nil, nil)：这里补一份空结果，免得用例漏给 list 时
	// 在页面上炸出 nil 解引用（那是测试夹具失真，不是页面缺陷）。
	if s.list == nil && s.listErr == nil {
		return &orderdto.ReturnListResp{}, nil
	}
	return s.list, s.listErr
}

func (s *returnLabelsService) GetReturn(_ context.Context, _ uint64) (*orderdto.ReturnDetailResp, error) {
	return s.detail, s.detailErr
}

// returnPageCapture 转交真实渲染器，同时把渲染错误抓回来。
//
// 为什么不能只断言响应体：整页渲染失败时 gin 会把它压成 500 + 一句归口短句，
// 页面上看不出是哪个键 / 哪行模板坏了（生产里也只进结构化日志）。测试必须直接拿到
// Render 的 error，否则「500」这个症状会一直被当成模板内容有问题来猜。
type returnPageCapture struct {
	delegate ginrender.HTMLRender
	name     string
	data     gin.H
	err      error
}

func (r *returnPageCapture) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data.(gin.H)
	return r
}

func (r *returnPageCapture) Render(w http.ResponseWriter) error {
	r.err = r.delegate.Instance(r.name, r.data).Render(w)
	return r.err
}

func (r *returnPageCapture) WriteContentType(w http.ResponseWriter) {
	r.delegate.Instance(r.name, r.data).WriteContentType(w)
}

// adminTemplatesDir 定位 internal/templates（本文件在 internal/module/order/inbound/http/）。
//
// 用 runtime.Caller 回推而不是写死相对路径：go test 的 cwd 是包目录，写死的层数一旦
// 因为文件挪目录而失配，报错会变成「模板找不到」这种看起来像环境问题的形状。
func adminTemplatesDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 拿不到本文件路径")
	}
	dir := filepath.Join(filepath.Dir(self), "..", "..", "..", "..", "templates")
	if _, err := os.Stat(filepath.Join(dir, "admin", "order", "returns.html")); err != nil {
		t.Fatalf("模板目录定位失败: %v", err)
	}
	return dir
}

// renderReturnsPage 走真实的 ReturnsPage（含 adminui.Prepare 与整页 Jet 渲染），不接数据库。
//
// 渲染器用**磁盘**模板（dev 模式，根为 internal/templates）：整页外壳 admin/layout.html
// 会 include../partials/ai_fab.html —— 那是**包目录顶层**的 partials/，
// 而生产用的 embed loader 只登记了 admin/ 与 static/（见 templates/admin_embed.go 的
// all:admin），于是 embed 模式下这句话取不到模板、整页 500。
// 本批只搬状态标签，不去动那条既有差异；这里显式走磁盘，测的是模板字节本身。
func renderReturnsPage(t *testing.T, query string, svc *returnLabelsService) string {
	t.Helper()
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	cap := &returnPageCapture{delegate: templates.NewJetHTMLRender(adminTemplatesDir(t), true)}
	engine.HTMLRender = cap
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/returns"+query, nil)
	NewReturnPageHandle(svc, orderPaginationProjects{}, nil, nil).ReturnsPage(c)
	c.Writer.WriteHeaderNow()
	if cap.err != nil {
		t.Fatalf("模板渲染失败: %v", cap.err)
	}
	if cap.name != "admin/order/returns.html" {
		t.Fatalf("模板名 = %q", cap.name)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Jet 在某一行中断时 HTTP 仍是 200、后半截整块消失；这两种形状必须显式判掉。
	if strings.Contains(body, "{{") {
		t.Fatalf("输出残留模板语法字面量（渲染中途截断）")
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("页面没有渲染完整（末尾不是 </html>），末尾 = %q", tailOf(body, 120))
	}
	return body
}

func tailOf(s string, n int) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) <= n {
		return trimmed
	}
	return trimmed[len(trimmed)-n:]
}

// returnStatusSampleDetail 一份详情：退货单与摘要订单的状态不同，用来证明两张事实表各自独立。
func returnStatusSampleDetail(returnStatus, orderStatus string) *orderdto.ReturnDetailResp {
	return &orderdto.ReturnDetailResp{
		Return: &orderdto.ReturnResp{
			ID: 7, ReturnNo: "R-7", OrderID: 3, OrderNo: "O-3",
			Status: returnStatus, StatusTone: "info",
		},
		Order: &orderdto.OrderResp{
			ID: 3, OrderNo: "O-3", Status: orderStatus, StatusTone: "ok",
			Currency: "CNY", Total: 12345,
		},
	}
}

// TestReturnStatusFactsMirrorEnumsTruth 事实必须逐字来自枚举真源（不抄第二份中文表）。
func TestReturnStatusFactsMirrorEnumsTruth(t *testing.T) {
	cases := []struct {
		name   string
		status string
	}{
		{"已知：待审核", "requested"},
		{"已知：待收货", "approved"},
		{"已知：已入库待退款", "received"},
		{"已知：已完成", "completed"},
		{"已知：已拒绝", "rejected"},
		{"已知：已撤销", "cancelled"},
		{"空串显示破折号", ""},
		{"纯空白等同空串", " \t\n "},
		{"带空白的已知值仍命中词条", "  received  "},
		{"未知值原样保留（含首尾空白，不 trim）", " unknown "},
		{"未知值里的 HTML 原样交给模板转义", "<b>x</b>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := returnStatusPageFacts([]string{tc.status}, "", nil, nil)
			got, ok := facts[tc.status]
			if !ok {
				t.Fatalf("map 键必须是**原始**状态值 %q，实际键表 = %v", tc.status, keysOf(facts))
			}
			key, fallback := orderenums.ReturnStatusLabel(tc.status)
			if got.Key != key || got.Fallback != fallback {
				t.Fatalf("事实 (%q, %q) 与枚举真源 (%q, %q) 不一致", got.Key, got.Fallback, key, fallback)
			}
		})
	}
}

// TestOrderStatusFactsMirrorEnumsTruth 摘要里的订单状态走另一份真源（订单状态枚举）。
func TestOrderStatusFactsMirrorEnumsTruth(t *testing.T) {
	for _, status := range []string{"pending", "paid", "shipped", "completed", "cancelled", "refunded", "", " \t ", "  shipped  ", " nope "} {
		t.Run(status, func(t *testing.T) {
			detail := &orderdto.ReturnDetailResp{Order: &orderdto.OrderResp{Status: status}}
			got, ok := orderStatusPageFacts(nil, detail)[status]
			if !ok {
				t.Fatalf("订单状态 %q 没进事实表", status)
			}
			key, fallback := orderenums.OrderStatusLabel(status)
			if got.Key != key || got.Fallback != fallback {
				t.Fatalf("事实 (%q, %q) 与枚举真源 (%q, %q) 不一致", got.Key, got.Fallback, key, fallback)
			}
		})
	}
}

// TestStatusPageFactsCollectEveryRenderedStatus 事实表要覆盖模板会索引的**每一个**值：
// 状态取值白名单（含空串，用于筛选徽章）+ 列表每一行 + 详情退货单 / 摘要订单。
// 漏一个的症状是那一格空白（不是报错），所以这里连同 nil 安全性一起判。
func TestStatusPageFactsCollectEveryRenderedStatus(t *testing.T) {
	returns := []*orderdto.ReturnResp{
		nil, // 列表里可能有 nil 行（服务端投影退化）：不能 panic
		{Status: "approved"},
		{Status: "approved"},
		{Status: "rejected"},
	}
	detail := returnStatusSampleDetail("requested", "paid")

	returnFacts := returnStatusPageFacts(returnStatusValues, "rejected", returns, detail)
	for _, want := range append(append([]string{}, returnStatusValues...), "approved", "rejected", "requested") {
		if _, ok := returnFacts[want]; !ok {
			t.Fatalf("退货事实表缺少 %q，键表 = %v", want, keysOf(returnFacts))
		}
	}
	// 空串在表里：筛选徽章那一格拿它索引（模板对空值走「全部」自己的词条，但索引必须命中）。
	if got := returnFacts[""]; got.Key != "" || got.Fallback != "—" {
		t.Fatalf("空状态事实 = %+v", got)
	}

	orderFacts := orderStatusPageFacts(orderStatusValues, detail)
	for _, want := range append(append([]string{}, orderStatusValues...), "paid") {
		if _, ok := orderFacts[want]; !ok {
			t.Fatalf("订单事实表缺少 %q，键表 = %v", want, keysOf(orderFacts))
		}
	}

	// 空列表 / 无详情 / 详情里半截也为空都不能 panic（装载失败与「还没选工程」都会走到）。
	for _, tc := range []struct {
		name   string
		detail *orderdto.ReturnDetailResp
	}{
		{"无详情", nil},
		{"详情里没有退货单", &orderdto.ReturnDetailResp{Order: &orderdto.OrderResp{Status: "paid"}}},
		{"详情里没有订单", &orderdto.ReturnDetailResp{Return: &orderdto.ReturnResp{Status: "approved"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := returnStatusPageFacts(returnStatusValues, "", nil, tc.detail)
			if len(facts) != len(returnStatusValues) {
				t.Fatalf("只应含白名单取值（无筛选、无列表、无详情），实际 %v", keysOf(facts))
			}
			_ = orderStatusPageFacts(orderStatusValues, tc.detail)
		})
	}
}

// TestStatusLabelFactHasNoFunctionField 事实是纯数据：带函数字段就等于把取词又搬回 Go。
func TestStatusLabelFactHasNoFunctionField(t *testing.T) {
	typ := reflect.TypeOf(statusLabelFact{})
	if typ.NumField() != 2 {
		t.Fatalf("事实字段数 = %d，want 2", typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		if kind := typ.Field(i).Type.Kind(); kind == reflect.Func {
			t.Fatalf("字段 %s 是函数类型", typ.Field(i).Name)
		}
	}
}

// TestReturnsPageRendersStatusLabelsFromFacts 真渲染：Jet 按**原始状态值**索引事实表，
// 语言由请求协商（缺词条回落模板内中文原文）。
func TestReturnsPageRendersStatusLabelsFromFacts(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ReturnStatusKeyRequested:      {"en-US": "Awaiting review", "zh-CN": "待审核"},
		orderenums.OrderStatusKeyPrefix + "paid": {"en-US": "Paid", "zh-CN": "已付款"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	svc := func() *returnLabelsService {
		return &returnLabelsService{
			list: &orderdto.ReturnListResp{
				Total: 1,
				List:  []*orderdto.ReturnResp{{ID: 9, ReturnNo: "R-9", Status: "approved", StatusTone: "mute"}},
			},
			detail: returnStatusSampleDetail("requested", "paid"),
		}
	}

	for _, tc := range []struct {
		lang          string
		wantRequested string
		wantOrderPaid string
	}{
		{"zh-CN", "待审核", "已付款"},
		{"en-US", "Awaiting review", "Paid"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			body := renderReturnsPage(t, "?project=p-1&returnId=7&lang="+tc.lang, svc())

			// 详情头的退货状态（事实表 + tr）。
			if !strings.Contains(body, `badge-info">`+tc.wantRequested+`</span>`) {
				t.Fatalf("详情退货状态未按 %s 取词", tc.lang)
			}
			// 摘要订单状态（另一张事实表，与退货状态不能串表）。
			if !strings.Contains(body, `badge-ok">`+tc.wantOrderPaid+`</span>`) {
				t.Fatalf("摘要订单状态未按 %s 取词", tc.lang)
			}
			// 列表行的退货状态：approved 没有注入词条 → 两种语言下都是模板内中文兜底文本。
			if !strings.Contains(body, `badge-mute">待收货</span>`) {
				t.Fatalf("列表行状态未取自退货事实表（缺词条应回落兜底文本）")
			}
		})
	}
}

// TestReturnsPageStatusLabelsHandleUnknownAndEmpty 未知 / 空 / 带空白的状态值：
// 空显示「—」、未知原样显示且经 Jet 自动转义，空列表下筛选提示也要显示原始筛选值。
func TestReturnsPageStatusLabelsHandleUnknownAndEmpty(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	t.Run("未知状态原样且被转义", func(t *testing.T) {
		body := renderReturnsPage(t, "?project=p-1&returnId=7", &returnLabelsService{
			detail: returnStatusSampleDetail("<script>alert(1)</script>", "paid"),
		})
		if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("未知状态没有按原值转义输出")
		}
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Fatalf("未知状态被当成 HTML 直接输出（转义失效）")
		}
	})

	t.Run("纯空白状态显示破折号", func(t *testing.T) {
		body := renderReturnsPage(t, "?project=p-1", &returnLabelsService{
			list: &orderdto.ReturnListResp{
				Total: 1,
				List:  []*orderdto.ReturnResp{{ID: 5, ReturnNo: "R-5", Status: "   ", StatusTone: "mute"}},
			},
		})
		if !strings.Contains(body, `badge-mute">—</span>`) {
			t.Fatalf("空白状态应显示破折号")
		}
	})

	t.Run("空列表下的未知筛选值原样显示", func(t *testing.T) {
		body := renderReturnsPage(t, "?project=p-1&status=unknown-status", &returnLabelsService{
			list: &orderdto.ReturnListResp{Total: 0},
		})
		if !strings.Contains(body, "unknown-status") {
			t.Fatalf("空态筛选提示没有显示原始筛选值")
		}
	})
}

func keysOf(facts map[string]statusLabelFact) []string {
	out := make([]string, 0, len(facts))
	for k := range facts {
		out = append(out, k)
	}
	return out
}
