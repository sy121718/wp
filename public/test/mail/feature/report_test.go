package feature

// report_test.go — 活动报表的口径测试（issue #38 P1）。
//
// 报表最容易出的错是**口径混用**：把「事件次数」当成「人数」用，
// 于是打开率能算出 300%。这条测试专门钉住这个区分。

import (
	"context"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// TestCampaignReportDistinguishesPeopleAndEvents 去重人数与事件次数必须分开。
func TestCampaignReportDistinguishesPeopleAndEvents(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 两个订阅者 + 一个活动（直接造数据，不走群发链路）。
	var contacts []*mailmodel.MailContactEntity
	for _, e := range []string{"r1@example.com", "r2@example.com"} {
		c := &mailmodel.MailContactEntity{Email: e, Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed}
		if err := f.m.CreateContact(ctx, c); err != nil {
			t.Fatal(err)
		}
		contacts = append(contacts, c)
	}
	acc, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "营销", Purpose: mailmodel.AccountPurposeMarketing,
		FromEmail: "n@clker.cn", Host: "mail.clker.cn", Port: 587, Password: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "rep", Subject: "s", BodyHTML: "<p>x</p>",
	}); err != nil {
		t.Fatal(err)
	}
	tpls, _ := f.m.ListTemplates(ctx, "rep")
	cp, err := f.svc.SaveCampaign(ctx, &maildto.SaveCampaignReq{
		Name: "报表活动", AccountID: acc.ID, TemplateID: tpls[0].ID, Subject: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.m.UpdateCampaignFields(ctx, cp.ID, map[string]any{
		"status": mailmodel.CampaignStatusSent, "total_count": 2, "sent_count": 2, "failed_count": 0,
	}); err != nil {
		t.Fatal(err)
	}

	// 事件：r1 打开两次 + 点击一次，r2 打开一次。
	seeds := []struct {
		contact uint64
		event   string
		url     string
	}{
		{contacts[0].ID, mailmodel.EventTypeOpen, ""},
		{contacts[0].ID, mailmodel.EventTypeOpen, ""},
		{contacts[0].ID, mailmodel.EventTypeClick, "https://shop.example.com/a"},
		{contacts[1].ID, mailmodel.EventTypeOpen, ""},
	}
	for _, s := range seeds {
		e := &mailmodel.MailCampaignEventEntity{CampaignID: cp.ID, ContactID: s.contact, EventType: s.event}
		if s.url != "" {
			u := s.url
			e.URL = &u
		}
		if err := f.m.CreateEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	report, err := f.svc.CampaignReport(ctx, cp.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	// 打开人数 = 2（r1 / r2 各一人），打开次数 = 3（r1 开了两次）。
	if report.Opened != 2 {
		t.Fatalf("打开人数应为 2（去重），实际 %d", report.Opened)
	}
	if report.OpenEvents != 3 {
		t.Fatalf("打开次数应为 3，实际 %d", report.OpenEvents)
	}
	if report.Clicked != 1 || report.ClickEvents != 1 {
		t.Fatalf("点击人数 / 次数应为 1/1，实际 %d/%d", report.Clicked, report.ClickEvents)
	}
	// 打开率 = 2 / 2 = 100%（不是 150%）。
	if report.OpenRate != 100 {
		t.Fatalf("打开率应为 100%%，实际 %v", report.OpenRate)
	}
	if len(report.Links) != 1 || report.Links[0].Contacts != 1 {
		t.Fatalf("链接排行应有一条且点击人数为 1，实际 %+v", report.Links)
	}

	// 失败人数参与分母：失败 1 人时，送达 1 人、打开 2 人 → 率会被算成 >100%，
	// 这提示的是数据本身有问题（失败的日志不该产生事件），不是口径问题。这里只验证
	// 分母用的是「送达人数」而不是「目标人数」。
	if err = f.m.UpdateCampaignFields(ctx, cp.ID, map[string]any{"sent_count": 1, "failed_count": 1}); err != nil {
		t.Fatal(err)
	}
	report, err = f.svc.CampaignReport(ctx, cp.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if report.OpenRate != 200 {
		t.Fatalf("分母应为送达人数（2-1=1），得到 %v", report.OpenRate)
	}
}
