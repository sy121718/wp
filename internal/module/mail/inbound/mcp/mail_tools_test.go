package mailmcp

// mail_tools_test.go — 营销三个只读工具的元信息、入参边界与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text（Data 只给渲染层），
// 正文里少了哪个数，用户就看不到哪个数。这里钉的是几件会直接决定回答对错的事：
//   · campaignId 必须出现在活动列表里 —— 它是接着调 campaign_stats 的唯一钥匙；
//   · 投递进度（目标 / 已发 / 失败）要在列表里就给全，「发完了没」不该要再调一次；
//   · bounced（投递失败）与 unsubscribed（主动退订）必须是两个说法 ——
//     混起来会让运营去追问一个根本没点过退订的人；
//   · 打开率是**估算**（图片预加载会抬高），正文里要带这句提醒。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	maildto "go_wp/internal/module/mail/dto"
)

type stubQuery struct {
	gotContacts  *maildto.ContactFilterReq
	gotCampaigns *maildto.CampaignListReq
	gotReportID  uint64
	gotReportLim int

	contactRes  *maildto.ContactListResp
	campaignRes *maildto.CampaignListResp
	reportRes   *maildto.CampaignReport
	err         error
}

func (s *stubQuery) ListContacts(_ context.Context, req *maildto.ContactFilterReq) (*maildto.ContactListResp, error) {
	s.gotContacts = req
	return s.contactRes, s.err
}

func (s *stubQuery) ListCampaigns(_ context.Context, req *maildto.CampaignListReq) (*maildto.CampaignListResp, error) {
	s.gotCampaigns = req
	return s.campaignRes, s.err
}

func (s *stubQuery) CampaignReport(_ context.Context, id uint64, _, pageSize int) (*maildto.CampaignReport, error) {
	s.gotReportID = id
	s.gotReportLim = pageSize
	return s.reportRes, s.err
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	return b
}

func mustQueryTools(t *testing.T, stub *stubQuery) map[string]mcp.Tool {
	t.Helper()
	tools, err := QueryTools(stub)
	if err != nil {
		t.Fatalf("取营销工具集失败: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("营销工具应暴露 3 个，实得 %d", len(tools))
	}
	byName := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name()] = tool
	}
	return byName
}

func TestQueryToolsRejectsNilReader(t *testing.T) {
	if _, err := QueryTools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestToolNamesAreStable(t *testing.T) {
	tools := mustQueryTools(t, &stubQuery{})
	for _, name := range []string{"contact_find", "campaign_list", "campaign_stats"} {
		if _, ok := tools[name]; !ok {
			t.Fatalf("缺少工具 %s", name)
		}
	}
}

func TestContactFindPassesFuzzyKeywordAndTags(t *testing.T) {
	stub := &stubQuery{contactRes: &maildto.ContactListResp{}}
	tools := mustQueryTools(t, stub)
	_, err := tools["contact_find"].Invoke(context.Background(), mustRaw(t, map[string]any{
		"keyword":  "z@example",
		"status":   "subscribed",
		"tags":     []string{"vip", "2026"},
		"pageSize": 5,
	}))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotContacts.Keyword != "z@example" {
		t.Fatalf("keyword 应原样透传，实得 %q", stub.gotContacts.Keyword)
	}
	if len(stub.gotContacts.Tags) != 2 {
		t.Fatalf("tags 应原样透传，实得 %v", stub.gotContacts.Tags)
	}
	if stub.gotContacts.PageSize != 5 {
		t.Fatalf("pageSize 应透传，实得 %d", stub.gotContacts.PageSize)
	}
	if stub.gotContacts.Page != 1 {
		t.Fatalf("工具是「取一批」而不是翻页页面，Page 恒为 1，实得 %d", stub.gotContacts.Page)
	}
}

func TestContactFindRejectsUnknownStatus(t *testing.T) {
	tools := mustQueryTools(t, &stubQuery{contactRes: &maildto.ContactListResp{}})
	_, err := tools["contact_find"].Invoke(context.Background(), mustRaw(t, map[string]any{"status": "bogus"}))
	if err == nil {
		t.Fatal("白名单之外的取值应被拦下")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError，实得 %T（%v）", err, err)
	}
}

func TestContactFindClampsPageSize(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, mailDefaultPageSize},
		{-1, mailDefaultPageSize},
		{3, 3},
		{999, mailMaxPageSize},
	}
	for _, tc := range cases {
		stub := &stubQuery{contactRes: &maildto.ContactListResp{}}
		tools := mustQueryTools(t, stub)
		if _, err := tools["contact_find"].Invoke(context.Background(), mustRaw(t, map[string]any{"pageSize": tc.in})); err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if stub.gotContacts.PageSize != tc.want {
			t.Fatalf("pageSize %d 应夹取成 %d，实得 %d", tc.in, tc.want, stub.gotContacts.PageSize)
		}
	}
}

func TestContactStatusTextDistinguishesBouncedFromUnsubscribed(t *testing.T) {
	// 这两个状态对用户的含义完全不同：投递失败通常是邮箱不存在，
	// 而主动退订是人不想收了。混起来会让运营去追问一个没点过退订的人。
	bounced := contactStatusText("bounced")
	unsub := contactStatusText("unsubscribed")
	if bounced == unsub {
		t.Fatalf("投递失败与已退订必须分开说，实得都是 %q", bounced)
	}
	if contactStatusText("") == "" {
		t.Fatal("空状态也要有说法（否则正文里会缺一格）")
	}
}

func TestContactListTextCarriesIDAndStatus(t *testing.T) {
	text := contactListText(&maildto.ContactListResp{
		Total: 3,
		Items: []maildto.ContactItem{
			{ID: 11, Email: "a@example.com", Name: "甲", Status: "subscribed", Tags: []string{"vip"}, Source: "checkout", CreateTime: "2026-09-01"},
			{ID: 12, Email: "b@example.com", Status: "bounced"},
			{ID: 13, Email: "c@example.com", Status: "unsubscribed"},
		},
	})
	for _, want := range []string{"id=11", "a@example.com", "（甲）", "已订阅", "标签 vip", "投递失败", "已退订", "共 3 位"} {
		if !strings.Contains(text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, text)
		}
	}
}

func TestContactListTextEmpty(t *testing.T) {
	if got := contactListText(&maildto.ContactListResp{}); !strings.Contains(got, "没有符合条件") {
		t.Fatalf("空结果应直说：%s", got)
	}
}

func TestCampaignListTextCarriesProgressAndID(t *testing.T) {
	text := campaignListText(&maildto.CampaignListResp{
		Total: 2,
		Items: []maildto.CampaignItem{
			{ID: 5, Name: "九月大促", Status: "sent", TotalCount: 100, SentCount: 98, FailedCount: 2, StartedAt: "2026-09-01 10:00", FinishedAt: "2026-09-01 10:30"},
			{ID: 6, Name: "草稿", Status: "draft", CreateTime: "2026-10-01 09:00"},
		},
	})
	for _, want := range []string{
		"id=5", "九月大促", "已发送", "目标 100 人、已发 98、失败 2", // 「发完了没」要靠这三个数
		"开始 2026-09-01 10:00", "结束 2026-09-01 10:30",
		"id=6", "草稿", "创建 2026-10-01 09:00", // 没开始的活动回落到创建时间
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, text)
		}
	}
}

func TestCampaignListRejectsUnknownStatus(t *testing.T) {
	tools := mustQueryTools(t, &stubQuery{campaignRes: &maildto.CampaignListResp{}})
	if _, err := tools["campaign_list"].Invoke(context.Background(), mustRaw(t, map[string]any{"status": "done"})); err == nil {
		t.Fatal("白名单之外的取值应被拦下（真实取值是 sent，不是 done）")
	}
}

func TestCampaignStatsRequiresID(t *testing.T) {
	stub := &stubQuery{}
	tools := mustQueryTools(t, stub)
	_, err := tools["campaign_stats"].Invoke(context.Background(), mustRaw(t, map[string]any{}))
	if err == nil {
		t.Fatal("campaignId 为 0 应在调用期被拦下")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError，实得 %T（%v）", err, err)
	}
	if stub.gotReportID != 0 {
		t.Fatal("参数不合规时不应打到 service")
	}
}

func TestCampaignReportTextCarriesRatesAndEstimateCaveat(t *testing.T) {
	stub := &stubQuery{reportRes: &maildto.CampaignReport{
		Campaign:     maildto.CampaignItem{ID: 5, Name: "九月大促", Status: "sent"},
		Target:       100,
		Sent:         98,
		Failed:       2,
		Opened:       60,
		OpenEvents:   90,
		Clicked:      12,
		ClickEvents:  15,
		Unsubscribed: 3,
		Complained:   1,
		Bounced:      2,
		OpenRate:     61.2,
		ClickRate:    12.2,
		Links: []maildto.LinkStat{
			{URL: "https://shop.example.com/sale", Total: 10, Contacts: 8},
		},
	}}
	tools := mustQueryTools(t, stub)
	out, err := tools["campaign_stats"].Invoke(context.Background(), mustRaw(t, map[string]any{"campaignId": 5}))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotReportID != 5 {
		t.Fatalf("id 应原样透传，实得 %d", stub.gotReportID)
	}
	for _, want := range []string{
		"九月大促", "目标 100 人", "送达 98", "失败 2",
		"打开：60 人（90 次），打开率 61.2%",
		"点击：12 人（15 次），点击率 12.2%",
		"退订 3、投诉 1、投递失败 2",
		"10 次点击、8 人", // 次数与人数都要（一人点 5 次 ≠ 5 人各 1 次）
		"估算",         // 打开率是估算值，必须提醒
	} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, out.Text)
		}
	}
}

func TestCampaignReportTextOmitsNegativeLineWhenClean(t *testing.T) {
	text := campaignReportText(&maildto.CampaignReport{
		Campaign: maildto.CampaignItem{ID: 1, Name: "x", Status: "sent"},
		Target:   10, Sent: 10,
	})
	if strings.Contains(text, "负向") {
		t.Fatalf("没有退订/投诉/退信时不该出现负向那一行（零值堆在正文里会挤掉真信息）：\n%s", text)
	}
}
