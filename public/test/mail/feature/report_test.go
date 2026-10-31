package feature

import (
	"context"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

func TestCampaignReportDistinguishesPeopleAndEvents(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

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

	if report.Opened != 2 {
		t.Fatalf("打开人数应为 2（去重），实际 %d", report.Opened)
	}
	if report.OpenEvents != 3 {
		t.Fatalf("打开次数应为 3，实际 %d", report.OpenEvents)
	}
	if report.Clicked != 1 || report.ClickEvents != 1 {
		t.Fatalf("点击人数 / 次数应为 1/1，实际 %d/%d", report.Clicked, report.ClickEvents)
	}

	if report.OpenRate != 100 {
		t.Fatalf("打开率应为 100%%，实际 %v", report.OpenRate)
	}
	if len(report.Links) != 1 || report.Links[0].Contacts != 1 {
		t.Fatalf("链接排行应有一条且点击人数为 1，实际 %+v", report.Links)
	}

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
