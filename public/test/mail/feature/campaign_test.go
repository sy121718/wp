package feature

// campaign_test.go — 群发活动的 feature 测试（issue #37）。
//
// 盯三件事：
//  1. **只发给已订阅的人**（pending 未确认同意的一律不发）—— 这是合规底线；
//  2. 启动是「受理」而不是「同步发完」：请求只统计人数、改状态、入队展开任务；
//  3. 展开按主键游标推进，日志条数 = 目标人数。

import (
	"context"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

func TestCampaignOnlySendsToSubscribed(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 发信账号 + 模板
	acc, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "营销账号", Purpose: mailmodel.AccountPurposeMarketing,
		FromEmail: "news@clker.cn", Host: "mail.clker.cn", Port: 587, Username: "u", Password: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "welcome", Locale: "", Name: "欢迎邮件",
		Subject: "你好 {{.name}}", BodyHTML: "<p>欢迎 {{.name}}</p>",
	}); err != nil {
		t.Fatal(err)
	}
	tpls, err := f.m.ListTemplates(ctx, "welcome")
	if err != nil || len(tpls) == 0 {
		t.Fatalf("模板未建成功: %v", err)
	}
	tplID := tpls[0].ID

	// 3 个联系人：2 已订阅 + 1 待确认
	name := "张三"
	for _, c := range []struct {
		email, status string
	}{
		{"a@example.com", mailmodel.ContactStatusSubscribed},
		{"b@example.com", mailmodel.ContactStatusSubscribed},
		{"pending@example.com", mailmodel.ContactStatusPending},
	} {
		if err = f.m.CreateContact(ctx, &mailmodel.MailContactEntity{
			Email: c.email, Name: &name, Source: mailmodel.ContactSourceImport, Status: c.status,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 建活动（目标标签为空 = 全部已订阅）
	item, err := f.svc.SaveCampaign(ctx, &maildto.SaveCampaignReq{
		Name: "九月活动", AccountID: acc.ID, TemplateID: tplID, Subject: "九月上新",
	})
	if err != nil {
		t.Fatalf("建活动失败: %v", err)
	}
	if item.Status != mailmodel.CampaignStatusDraft {
		t.Fatalf("新活动应为草稿，实际 %s", item.Status)
	}

	// 启动：只受理，统计到 2 人（pending 不算）
	start, err := f.svc.StartCampaign(ctx, &maildto.StartCampaignReq{CampaignID: item.ID})
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if start.Total != 2 {
		t.Fatalf("目标人数应为 2（pending 不发），实际 %d", start.Total)
	}
	// 队列可用时状态是 sending（展开在后台分批进行）；队列未启用时走**同步降级**，
	// 此刻已经展开完了 → sent。两种都合法，这里断言「已进入发送流程且不再是草稿」。
	row, _ := f.m.GetCampaign(ctx, item.ID)
	if row.Status != mailmodel.CampaignStatusSending && row.Status != mailmodel.CampaignStatusSent {
		t.Fatalf("启动后状态应为 sending 或 sent，实际 %s", row.Status)
	}

	// 展开一段（队列未启用时入队失败被忽略，日志照写 —— 这里验证的是**目标人群**与计数）
	if err = f.svc.DispatchCampaign(ctx, item.ID, 0); err != nil {
		t.Fatalf("展开失败: %v", err)
	}
	logs, total, err := f.m.ListLogs(ctx, "", mailmodel.LogStatusPending, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("应恰好展开 2 条日志，实际 %d", total)
	}
	for _, l := range logs {
		if l.CampaignID == nil || *l.CampaignID != item.ID {
			t.Fatal("日志未关联到活动")
		}
		if l.ContactID == nil {
			t.Fatal("日志未关联到联系人")
		}
		if l.ToEmail == "pending@example.com" {
			t.Fatal("未确认同意的联系人被发送了 —— 这是合规底线")
		}
	}

	// 展开完毕 → 状态收尾为 sent
	row, _ = f.m.GetCampaign(ctx, item.ID)
	if row.Status != mailmodel.CampaignStatusSent {
		t.Fatalf("展开完成后状态应为 sent，实际 %s", row.Status)
	}
	if row.FinishedAt == nil {
		t.Fatal("完成后应有 finished_at")
	}
}

func TestCampaignNoRecipientRejected(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	acc, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "营销账号", Purpose: mailmodel.AccountPurposeMarketing,
		FromEmail: "news@clker.cn", Host: "mail.clker.cn", Port: 587, Password: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "t1", Subject: "s", BodyHTML: "<p>x</p>",
	}); err != nil {
		t.Fatal(err)
	}
	tpls, _ := f.m.ListTemplates(ctx, "t1")
	item, err := f.svc.SaveCampaign(ctx, &maildto.SaveCampaignReq{
		Name: "空活动", AccountID: acc.ID, TemplateID: tpls[0].ID, Subject: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 一个订阅者都没有 → 拒绝启动（发一场没有收件人的活动只会留下脏状态）
	if _, err = f.svc.StartCampaign(ctx, &maildto.StartCampaignReq{CampaignID: item.ID}); err == nil {
		t.Fatal("目标为 0 人时应拒绝启动")
	}
	row, _ := f.m.GetCampaign(ctx, item.ID)
	if row.Status != mailmodel.CampaignStatusDraft {
		t.Fatalf("拒绝启动后应保持草稿，实际 %s", row.Status)
	}
}
