package feature

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/facing"
)

// TestMailContactsStatusAllOrNothing 覆盖整批改状态：退订路径的抑制名单回滚，
// 以及订阅路径第二个人写库失败时两个人的状态、订阅时刻与同意来源一起回滚且不触发订阅自动化。
func TestMailContactsStatusAllOrNothing(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	seed := func(email string) uint64 {
		t.Helper()
		if err := f.m.CreateContact(ctx, &mailmodel.MailContactEntity{
			Email: email, Source: mailmodel.ContactSourceManual,
			Status: mailmodel.ContactStatusPending,
		}); err != nil {
			t.Fatalf("造联系人失败: %v", err)
		}
		row, err := f.m.GetContactByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}

	t.Run("混合不存在 id 整批拒绝且状态为 404", func(t *testing.T) {
		a := seed("st-mix@example.com")
		var maxID uint64
		if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			t.Fatal(err)
		}
		err := f.svc.UpdateContactsStatus(ctx, &maildto.UpdateContactsStatusReq{
			IDs: []uint64{a, maxID + 999999}, Status: mailmodel.ContactStatusUnsubscribed,
		})
		if err == nil {
			t.Fatal("含不存在 id 必须整批拒绝")
		}
		if status, _ := facing.StatusOf(err); status != 404 {
			t.Fatalf("缺失项应按 404 声明，实际 %d（%v）", status, err)
		}
		row, gerr := f.m.GetContact(ctx, a)
		if gerr != nil {
			t.Fatal(gerr)
		}
		if row.Status != mailmodel.ContactStatusPending {
			t.Fatalf("整批回滚后合法行不该被改，实际 %q", row.Status)
		}
	})

	t.Run("退订到第二个人失败时第一个人的状态与抑制名单都回滚", func(t *testing.T) {
		a, b := seed("st-dbfail-a@example.com"), seed("st-dbfail-b@example.com")
		failUpdatingContact(t, f, b)

		err := f.svc.UpdateContactsStatus(ctx, &maildto.UpdateContactsStatusReq{
			IDs: []uint64{a, b}, Status: mailmodel.ContactStatusUnsubscribed,
		})
		if err == nil {
			t.Fatal("DB 错误必须整批失败")
		}
		if _, ok := facing.StatusOf(err); ok {
			t.Fatalf("原始 DB 错误不能伪装成业务状态，实际 %v", err)
		}
		for _, id := range []uint64{a, b} {
			row, gerr := f.m.GetContact(ctx, id)
			if gerr != nil {
				t.Fatal(gerr)
			}
			if row.Status != mailmodel.ContactStatusPending {
				t.Fatalf("DB 错误后 id=%d 的状态不该改动，实际 %q", id, row.Status)
			}
		}
		var sup int64
		if err := f.db.Table("mail_suppressions").
			Where("lower(email) IN ?", []string{"st-dbfail-a@example.com", "st-dbfail-b@example.com"}).
			Count(&sup).Error; err != nil {
			t.Fatal(err)
		}
		if sup != 0 {
			t.Fatalf("回滚的批次不该留下抑制名单记录，实际 %d 行", sup)
		}
	})

	t.Run("订阅到第二个人失败时两个人的状态与订阅留痕都回滚", func(t *testing.T) {
		a, b := seed("st-sub-fail-a@example.com"), seed("st-sub-fail-b@example.com")
		subscribedID := seedAutomationWithParams(t, f, "订阅后跟进", mailmodel.TriggerContactSubscribed, nil)
		// 批量按 id 升序处理：a 先写入订阅留痕，随后 b 的更新被注入故障中止，
		// 断言 a 已写出的 status / subscribed_at / consent_source 也随整批回滚。
		failUpdatingContact(t, f, b)

		err := f.svc.UpdateContactsStatus(ctx, &maildto.UpdateContactsStatusReq{
			IDs: []uint64{a, b}, Status: mailmodel.ContactStatusSubscribed, Note: "批次同意来源",
		})
		if err == nil {
			t.Fatal("DB 错误必须整批失败")
		}
		if _, ok := facing.StatusOf(err); ok {
			t.Fatalf("原始 DB 错误不能伪装成业务状态，实际 %v", err)
		}
		for _, id := range []uint64{a, b} {
			row, gerr := f.m.GetContact(ctx, id)
			if gerr != nil {
				t.Fatal(gerr)
			}
			if row.Status != mailmodel.ContactStatusPending {
				t.Fatalf("DB 错误后 id=%d 的状态不该改动，实际 %q", id, row.Status)
			}
			if row.SubscribedAt != nil {
				t.Fatalf("DB 错误后 id=%d 不该留下 subscribed_at，实际 %v", id, row.SubscribedAt)
			}
			if row.ConsentSource != nil {
				t.Fatalf("DB 错误后 id=%d 不该留下 consent_source，实际 %v", id, row.ConsentSource)
			}
		}
		if got := runTotal(t, f, subscribedID); got != 0 {
			t.Fatalf("回滚的批次不该触发订阅自动化，实际实例数 %d", got)
		}

		// 成功对照：同一订阅自动化在后两人提交成功后必须各触发一次。
		c, d := seed("st-sub-ok-a@example.com"), seed("st-sub-ok-b@example.com")
		if err := f.svc.UpdateContactsStatus(ctx, &maildto.UpdateContactsStatusReq{
			IDs: []uint64{c, d}, Status: mailmodel.ContactStatusSubscribed, Note: "批次同意来源",
		}); err != nil {
			t.Fatalf("提交成功的批量订阅不该失败: %v", err)
		}
		for _, id := range []uint64{c, d} {
			row, gerr := f.m.GetContact(ctx, id)
			if gerr != nil {
				t.Fatal(gerr)
			}
			if row.Status != mailmodel.ContactStatusSubscribed || row.SubscribedAt == nil {
				t.Fatalf("id=%d 提交后应为 subscribed 且有 subscribed_at，实际 %q / %v", id, row.Status, row.SubscribedAt)
			}
			if row.ConsentSource == nil || *row.ConsentSource != "批次同意来源" {
				t.Fatalf("id=%d 的同意来源没落库，实际 %v", id, row.ConsentSource)
			}
		}
		if got := runTotal(t, f, subscribedID); got != 2 {
			t.Fatalf("提交成功的订阅应各建一个实例，实际 %d", got)
		}

		// 同值 no-op：状态已经是 subscribed，再提交不能重复触发。
		if err := f.svc.UpdateContactsStatus(ctx, &maildto.UpdateContactsStatusReq{
			IDs: []uint64{c, d}, Status: mailmodel.ContactStatusSubscribed, Note: "批次同意来源",
		}); err != nil {
			t.Fatalf("同值订阅应成功: %v", err)
		}
		if got := runTotal(t, f, subscribedID); got != 2 {
			t.Fatalf("状态未变化的同值提交不该重复触发，实际 %d", got)
		}
	})
}

// TestMailTagContactsReverseOrderNoDeadlock 覆盖两批反向 id 并发打标签：
// 稳定升序加锁后不应死锁，且两组标签都不能丢。
func TestMailTagContactsReverseOrderNoDeadlock(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	seed := func(email string) uint64 {
		t.Helper()
		if err := f.m.CreateContact(ctx, &mailmodel.MailContactEntity{
			Email: email, Source: mailmodel.ContactSourceManual, Status: mailmodel.ContactStatusPending,
		}); err != nil {
			t.Fatalf("造联系人失败: %v", err)
		}
		row, err := f.m.GetContactByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	a, b := seed("tag-conc-a@example.com"), seed("tag-conc-b@example.com")

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	run := func(ids []uint64, tag string) {
		defer wg.Done()
		changed, err := f.svc.TagContacts(ctx, &maildto.TagContactsReq{IDs: ids, Add: []string{tag}})
		if err == nil && changed != 2 {
			err = fmt.Errorf("tag %s 应改 2 条，实际 %d", tag, changed)
		}
		errs <- err
	}
	wg.Add(2)
	go run([]uint64{a, b}, "x")
	go run([]uint64{b, a}, "y")

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("反向 id 的并发打标签超时：加锁顺序不稳定会造成死锁")
	}
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发打标签失败: %v", err)
		}
	}
	for _, id := range []uint64{a, b} {
		row, err := f.m.GetContact(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, tag := range row.Tags {
			got[tag] = true
		}
		if !got["x"] || !got["y"] {
			t.Fatalf("id=%d 应同时有 x 和 y，实际 %v", id, []string(row.Tags))
		}
	}
}

// TestMailAccountTemplateCampaignBatchAtomic 覆盖账号 / 模板 / 活动的批量删除：
// 混合缺失项整批拒绝；多行 DELETE 中途抛错时整条语句及其事务一并回滚。
func TestMailAccountTemplateCampaignBatchAtomic(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.svc.SetCipherSecret("batch-atomic-secret")

	seedAccount := func(name, email string) uint64 {
		t.Helper()
		acc, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
			Name: name, Purpose: mailmodel.AccountPurposeTransactional,
			FromEmail: email, Host: "smtp.example.com", Port: 587, Password: "p",
		})
		if err != nil {
			t.Fatalf("造发信账号失败: %v", err)
		}
		return acc.ID
	}
	seedTemplate := func(key string) uint64 {
		t.Helper()
		item, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
			TemplateKey: key, Locale: "zh-CN", Name: key, Subject: "主题",
		})
		if err != nil {
			t.Fatalf("造模板失败: %v", err)
		}
		return item.ID
	}
	seedCampaign := func(name string, accountID, templateID uint64) uint64 {
		t.Helper()
		item, err := f.svc.SaveCampaign(ctx, &maildto.SaveCampaignReq{
			Name: name, AccountID: accountID, TemplateID: templateID, Subject: "主题",
		})
		if err != nil {
			t.Fatalf("造活动失败: %v", err)
		}
		return item.ID
	}

	t.Run("账号混合缺失整批拒绝且计数为 0", func(t *testing.T) {
		a := seedAccount("账号甲", "acc-a@example.com")
		var maxID uint64
		if err := f.db.Table("mail_accounts").Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			t.Fatal(err)
		}
		deleted, err := f.svc.DeleteAccounts(ctx, &maildto.DeleteIDsReq{IDs: []uint64{a, maxID + 999999}})
		if err == nil || deleted != 0 {
			t.Fatalf("混合缺失必须整批拒绝且计数为 0，实际 %d / %v", deleted, err)
		}
		if _, gerr := f.m.GetAccount(ctx, a); gerr != nil {
			t.Fatalf("整批回滚后账号不该被删：%v", gerr)
		}
	})

	t.Run("账号删到第二个失败时第一个也回滚", func(t *testing.T) {
		a, b := seedAccount("账号乙", "acc-b@example.com"), seedAccount("账号丙", "acc-c@example.com")
		failDeletingRow(t, f, "mail_accounts", b)
		deleted, err := f.svc.DeleteAccounts(ctx, &maildto.DeleteIDsReq{IDs: []uint64{a, b}})
		if err == nil || deleted != 0 {
			t.Fatalf("DB 错误必须整批失败且计数为 0，实际 %d / %v", deleted, err)
		}
		if _, ok := facing.StatusOf(err); ok {
			t.Fatalf("原始 DB 错误不能伪装成业务状态，实际 %v", err)
		}
		for _, id := range []uint64{a, b} {
			if _, gerr := f.m.GetAccount(ctx, id); gerr != nil {
				t.Fatalf("DB 错误后账号 id=%d 不该被删：%v", id, gerr)
			}
		}
	})

	t.Run("模板混合缺失整批拒绝", func(t *testing.T) {
		a := seedTemplate("atomic_tpl_a")
		var maxID uint64
		if err := f.db.Table("mail_templates").Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			t.Fatal(err)
		}
		deleted, err := f.svc.DeleteTemplates(ctx, &maildto.DeleteIDsReq{IDs: []uint64{a, maxID + 999999}})
		if err == nil || deleted != 0 {
			t.Fatalf("混合缺失必须整批拒绝且计数为 0，实际 %d / %v", deleted, err)
		}
		if _, gerr := f.m.GetTemplateByID(ctx, a); gerr != nil {
			t.Fatalf("整批回滚后模板不该被删：%v", gerr)
		}
	})

	t.Run("模板删到第二个失败时第一个也回滚", func(t *testing.T) {
		a, b := seedTemplate("atomic_tpl_b"), seedTemplate("atomic_tpl_c")
		failDeletingRow(t, f, "mail_templates", b)
		deleted, err := f.svc.DeleteTemplates(ctx, &maildto.DeleteIDsReq{IDs: []uint64{a, b}})
		if err == nil || deleted != 0 {
			t.Fatalf("DB 错误必须整批失败且计数为 0，实际 %d / %v", deleted, err)
		}
		for _, id := range []uint64{a, b} {
			if _, gerr := f.m.GetTemplateByID(ctx, id); gerr != nil {
				t.Fatalf("DB 错误后模板 id=%d 不该被删：%v", id, gerr)
			}
		}
	})

	t.Run("活动混合缺失与发送中整批拒绝", func(t *testing.T) {
		acc := seedAccount("账号丁", "acc-d@example.com")
		tpl := seedTemplate("atomic_tpl_d")
		draft := seedCampaign("草稿活动", acc, tpl)
		sending := seedCampaign("发送中活动", acc, tpl)
		if err := f.m.UpdateCampaignFields(ctx, sending, map[string]any{
			"status": mailmodel.CampaignStatusSending,
		}); err != nil {
			t.Fatal(err)
		}
		var maxID uint64
		if err := f.db.Table("mail_campaigns").Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			t.Fatal(err)
		}
		deleted, err := f.svc.DeleteCampaigns(ctx, &maildto.DeleteIDsReq{
			IDs: []uint64{draft, sending, maxID + 999999},
		})
		if err == nil || deleted != 0 {
			t.Fatalf("混合缺失与发送中必须整批拒绝且计数为 0，实际 %d / %v", deleted, err)
		}
		if _, gerr := f.m.GetCampaign(ctx, draft); gerr != nil {
			t.Fatalf("被拒绝的草稿活动不该被删：%v", gerr)
		}
		if _, gerr := f.m.GetCampaign(ctx, sending); gerr != nil {
			t.Fatalf("发送中的活动不该被删：%v", gerr)
		}
	})

	t.Run("活动删到第二个失败时第一个也回滚", func(t *testing.T) {
		acc := seedAccount("账号戊", "acc-e@example.com")
		tpl := seedTemplate("atomic_tpl_e")
		a := seedCampaign("活动甲", acc, tpl)
		b := seedCampaign("活动乙", acc, tpl)
		failDeletingRow(t, f, "mail_campaigns", b)
		deleted, err := f.svc.DeleteCampaigns(ctx, &maildto.DeleteIDsReq{IDs: []uint64{a, b}})
		if err == nil || deleted != 0 {
			t.Fatalf("DB 错误必须整批失败且计数为 0，实际 %d / %v", deleted, err)
		}
		for _, id := range []uint64{a, b} {
			if _, gerr := f.m.GetCampaign(ctx, id); gerr != nil {
				t.Fatalf("DB 错误后活动 id=%d 不该被删：%v", id, gerr)
			}
		}
	})
}

// TestMailAutomationDeleteStopsRunsAtomically 覆盖自动化批量删除：
// 停运行实例与删流程同一事务；第二个删除失败时，第一个流程的实例状态也必须回到 running。
func TestMailAutomationDeleteStopsRunsAtomically(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	seed := func(name string) (uint64, uint64) {
		t.Helper()
		automationID := seedAutomationWithParams(t, f, name, mailmodel.TriggerManual, nil)
		run := &mailmodel.MailAutomationRunEntity{
			AutomationID: automationID, AutomationVersion: 1, ContactID: 1,
			Status: mailmodel.RunStatusRunning,
		}
		if err := f.m.CreateRun(ctx, run); err != nil {
			t.Fatalf("造运行实例失败: %v", err)
		}
		return automationID, run.ID
	}
	runStatus := func(id uint64) string {
		t.Helper()
		row, err := f.m.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return row.Status
	}

	t.Run("混合缺失整批拒绝且实例仍运行", func(t *testing.T) {
		automationID, runID := seed("原子流程甲")
		var maxID uint64
		if err := f.db.Table("mail_automations").Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			t.Fatal(err)
		}
		deleted, err := f.svc.DeleteAutomations(ctx, &maildto.DeleteIDsReq{
			IDs: []uint64{automationID, maxID + 999999},
		})
		if err == nil || deleted != 0 {
			t.Fatalf("混合缺失必须整批拒绝且计数为 0，实际 %d / %v", deleted, err)
		}
		if _, gerr := f.m.GetAutomation(ctx, automationID); gerr != nil {
			t.Fatalf("整批回滚后流程不该被删：%v", gerr)
		}
		if got := runStatus(runID); got != mailmodel.RunStatusRunning {
			t.Fatalf("整批回滚后实例应保持 running，实际 %q", got)
		}
	})

	t.Run("第二个流程删除失败时第一个流程的实例状态回滚", func(t *testing.T) {
		a, runA := seed("原子流程乙")
		b, runB := seed("原子流程丙")
		failDeletingRow(t, f, "mail_automations", b)
		deleted, err := f.svc.DeleteAutomations(ctx, &maildto.DeleteIDsReq{IDs: []uint64{a, b}})
		if err == nil || deleted != 0 {
			t.Fatalf("DB 错误必须整批失败且计数为 0，实际 %d / %v", deleted, err)
		}
		for _, id := range []uint64{a, b} {
			if _, gerr := f.m.GetAutomation(ctx, id); gerr != nil {
				t.Fatalf("DB 错误后流程 id=%d 不该被删：%v", id, gerr)
			}
		}
		if got := runStatus(runA); got != mailmodel.RunStatusRunning {
			t.Fatalf("第一个流程的实例应随事务回滚到 running，实际 %q", got)
		}
		if got := runStatus(runB); got != mailmodel.RunStatusRunning {
			t.Fatalf("第二个流程的实例也不该被停，实际 %q", got)
		}
	})
}

// failDeletingRow 在真实 PG 上给指定表装一个只对该 id 抛错的删除触发器。
func failDeletingRow(t *testing.T, f *fixture, table string, id uint64) {
	t.Helper()
	fn := "mail_test_fail_del_" + table
	trg := fn + "_trg"
	mustExec(t, f, `
CREATE OR REPLACE FUNCTION `+fn+`() RETURNS trigger AS $$
BEGIN
    IF OLD.id = `+uintText(id)+` THEN
        RAISE EXCEPTION 'injected delete failure';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER `+trg+` BEFORE DELETE ON `+table+`
FOR EACH ROW EXECUTE FUNCTION `+fn+`();`)
	t.Cleanup(func() {
		_ = f.db.Exec(`DROP TRIGGER IF EXISTS ` + trg + ` ON ` + table).Error
		_ = f.db.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`).Error
	})
}
