package analyticsmcp

// analytics_tools_test.go — 访问统计工具的元信息、入参边界与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text（Data 只给渲染层），
// 正文里少了哪组数，用户就问不出哪类问题。这里钉的是几件会直接决定回答对错的事：
//   · PV / UV 必须逐字给出（模型复述数字时不能靠猜）；
//   · 零访问必须与「数据取不到」区分开 —— 前者要提醒检查打点是否生效，
//     否则站点刚上线会被汇报成「流量为零」；
//   · 长窗口不能把 90 天逐日铺开（挤掉回答空间），但「哪天最高」必须答得出；
//   · 空维度取值（没有来源 / UA 缺失）要渲染成占位文案，跳过会让「一半流量没有来源」
//     看起来像数据缺失。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	analyticsdto "go_wp/internal/module/analytics/dto"
)

type stubReader struct {
	got    *analyticsdto.SummaryReq
	res    *analyticsdto.SummaryResp
	err    error
	called int
}

func (s *stubReader) Summary(_ context.Context, req *analyticsdto.SummaryReq) (*analyticsdto.SummaryResp, error) {
	s.called++
	s.got = req
	return s.res, s.err
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	return b
}

func mustTrafficTool(t *testing.T, stub *stubReader) mcp.Tool {
	t.Helper()
	tools, err := TrafficTools(stub)
	if err != nil {
		t.Fatalf("取访问统计工具失败: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("访问统计应暴露 1 个工具，实得 %d", len(tools))
	}
	if tools[0].Name() != "traffic_summary" {
		t.Fatalf("工具名应是 traffic_summary，实得 %q", tools[0].Name())
	}
	return tools[0]
}

func TestTrafficToolsRejectsNilReader(t *testing.T) {
	if _, err := TrafficTools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestTrafficSummaryRequiresProject(t *testing.T) {
	stub := &stubReader{}
	tool := mustTrafficTool(t, stub)
	_, err := tool.Invoke(context.Background(), mustRaw(t, map[string]any{}))
	if err == nil {
		t.Fatal("projectId 必填，缺失应在调用期被拦下")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError（可回给模型改参重试），实得 %T（%v）", err, err)
	}
	if stub.called != 0 {
		t.Fatal("参数不合规时不应打到 service（省一次库查询，也避免空 projectId 去查全表）")
	}
	if !strings.Contains(err.Error(), "projectId") {
		t.Fatalf("报错要指明缺的是 projectId：%v", err)
	}
	// 注意这条错误来自 schema 的 required（校验层），不是 handler 里的兜底 ——
	// 所以它不会带上「去 site_projects 查」那句话。那句话在工具描述里。
	if !strings.Contains(tool.Description(), "site_projects") {
		t.Fatal("工具描述里要写明 projectId 从 site_projects 查（模型靠描述选工具）")
	}
}

func TestTrafficSummaryClampsLimits(t *testing.T) {
	cases := []struct {
		name          string
		args          map[string]any
		wantPathLimit int
		wantRankLimit int
	}{
		{"默认", map[string]any{"projectId": "p1"}, trafficDefaultPathLimit, trafficDefaultRankLimit},
		{"负数退回默认", map[string]any{"projectId": "p1", "pathLimit": -5}, trafficDefaultPathLimit, trafficDefaultRankLimit},
		{"显式小值", map[string]any{"projectId": "p1", "pathLimit": 3, "rankLimit": 2}, 3, 2},
		{"超上限夹取", map[string]any{"projectId": "p1", "pathLimit": 999, "rankLimit": 999}, trafficMaxPathLimit, trafficMaxRankLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubReader{res: &analyticsdto.SummaryResp{}}
			tool := mustTrafficTool(t, stub)
			if _, err := tool.Invoke(context.Background(), mustRaw(t, tc.args)); err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if stub.got.PathLimit != tc.wantPathLimit {
				t.Fatalf("pathLimit 应夹取成 %d，实得 %d", tc.wantPathLimit, stub.got.PathLimit)
			}
			if stub.got.RankLimit != tc.wantRankLimit {
				t.Fatalf("rankLimit 应夹取成 %d，实得 %d", tc.wantRankLimit, stub.got.RankLimit)
			}
		})
	}
}

func TestDescribeTrafficGivesAllSixDimensions(t *testing.T) {
	text := describeTraffic(&analyticsdto.SummaryResp{
		ProjectID: "p1", From: "2026-09-06", To: "2026-10-05",
		Total: 1234, Visitors: 321,
		Daily: []analyticsdto.DailyCount{
			{Day: "2026-10-05", Views: 88, Visitors: 60},
			{Day: "2026-10-04", Views: 40, Visitors: 30},
		},
		Paths: []analyticsdto.PathCount{
			{Path: "", Views: 412, Visitors: 200}, // 空 path 就是首页（统计侧存的就是空串）
			{Path: "/products/vit-c", Views: 88, Visitors: 70},
		},
		PathTotal: 12,
		Referrers: []analyticsdto.RankCount{{Value: "google.com", Views: 300}},
		UAClasses: []analyticsdto.RankCount{{Value: "desktop", Views: 800}},
		Langs:     []analyticsdto.RankCount{{Value: "zh-CN", Views: 900}},
	})
	for _, want := range []string{
		"1234", "321", // PV / UV 必须逐字给出
		"10-05:88", // 短窗口逐日
		"/（首页）",    // 空 path 是首页，不能渲染成空
		"/products/vit-c",
		"共 12 个页面有访问",      // 总量
		"google.com 300 次", // 来源域
		"desktop 800 次",    // 设备
		"zh-CN 900 次",      // 语言
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, text)
		}
	}
}

func TestDescribeTrafficZeroIsNotSilentSuccess(t *testing.T) {
	text := describeTraffic(&analyticsdto.SummaryResp{
		ProjectID: "p1", From: "2026-09-06", To: "2026-10-05",
	})
	if !strings.Contains(text, "没有任何访问记录") {
		t.Fatalf("零访问要直说：%s", text)
	}
	// 必须给出「可能是打点没生效」这个替代解释：否则站点刚上线会被汇报成「流量为零」。
	if !strings.Contains(text, "打点") {
		t.Fatalf("零访问要提醒检查打点是否生效：%s", text)
	}
}

func TestDescribeTrafficLongWindowReportsPeaksOnly(t *testing.T) {
	daily := make([]analyticsdto.DailyCount, 0, 90)
	for i := 0; i < 90; i++ {
		views := int64(i)
		if i == 42 {
			views = 999
		}
		daily = append(daily, analyticsdto.DailyCount{Day: "2026-07-01", Views: views})
	}
	text := describeTraffic(&analyticsdto.SummaryResp{
		ProjectID: "p1", From: "2026-07-01", To: "2026-09-28",
		Total: 5000, Visitors: 400, Daily: daily,
	})
	if strings.Count(text, "07-01:") > 1 {
		t.Fatalf("长窗口不应逐日铺开（会挤掉回答空间）：\n%s", text)
	}
	if !strings.Contains(text, "999") {
		t.Fatalf("长窗口仍要答得出「哪天最高」：\n%s", text)
	}
	if !strings.Contains(text, "90 天") {
		t.Fatalf("长窗口要报窗口内有多少天有访问：\n%s", text)
	}
}

func TestWriteRankKeepsEmptyDimensionValue(t *testing.T) {
	var b strings.Builder
	writeRank(&b, "来源域", []analyticsdto.RankCount{
		{Value: "", Views: 500},
		{Value: "google.com", Views: 300},
	})
	got := b.String()
	if !strings.Contains(got, "（未标注） 500 次") {
		t.Fatalf("空取值要渲染成占位文案（跳过会让「一半流量没有来源」看起来像数据缺失）：%s", got)
	}
	if !strings.Contains(got, "google.com 300 次") {
		t.Fatalf("真实取值要原样展示：%s", got)
	}
}

func TestShortDayHandlesUnexpectedShape(t *testing.T) {
	if got := shortDay("2026-10-05"); got != "10-05" {
		t.Fatalf("正常日期应截成 月-日，实得 %q", got)
	}
	// 上游若回了非预期形状，原样返回比切片越界好（正文宁可难看也不能 panic）。
	if got := shortDay("2026"); got != "2026" {
		t.Fatalf("短串应原样返回，实得 %q", got)
	}
}
