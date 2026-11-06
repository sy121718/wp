package feature

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/facing"
)

func uintText(id uint64) string { return strconv.FormatUint(id, 10) }

// TestMailBatchDeleteAllOrNothing 覆盖联系人整批删除：
// 合法全成功、混合不存在 id 整批回滚、以及删到第二个目标时 DB 错误导致第一个也回滚。
func TestMailBatchDeleteAllOrNothing(t *testing.T) {
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

	t.Run("合法全部成功", func(t *testing.T) {
		a, b := seed("all-a@example.com"), seed("all-b@example.com")
		deleted, err := f.svc.DeleteContacts(ctx, &maildto.DeleteContactsReq{IDs: []uint64{a, b}})
		if err != nil || deleted != 2 {
			t.Fatalf("合法整批应删 2 条，实际 %d / %v", deleted, err)
		}
		for _, id := range []uint64{a, b} {
			if _, err := f.m.GetContact(ctx, id); err == nil {
				t.Fatalf("id=%d 应已删除", id)
			}
		}
	})

	t.Run("混合不存在 id 整批回滚", func(t *testing.T) {
		a := seed("mix-a@example.com")
		var maxID uint64
		if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			t.Fatal(err)
		}
		deleted, err := f.svc.DeleteContacts(ctx, &maildto.DeleteContactsReq{IDs: []uint64{a, maxID + 999999}})
		if err == nil || deleted != 0 {
			t.Fatalf("含不存在 id 必须整批拒绝且计数为 0，实际 %d / %v", deleted, err)
		}
		var blocked facing.BlockedError
		if !errors.As(err, &blocked) {
			t.Fatalf("应给结构化拒绝事实，实际 %T", err)
		}
		if _, gerr := f.m.GetContact(ctx, a); gerr != nil {
			t.Fatalf("整批回滚后合法行不该被删：%v", gerr)
		}
	})

	t.Run("删到第二个目标失败时第一个也回滚", func(t *testing.T) {
		a, b := seed("dbfail-a@example.com"), seed("dbfail-b@example.com")
		// 单条 DELETE ... IN (...) 的行序由 PostgreSQL 决定；故障注入 b 后整条语句中止，
		// 同事务内已删的行随语句回滚 —— 断言的是「整批零提交」，不假定 a 先被处理。
		failDeletingContact(t, f, b)
		deleted, err := f.svc.DeleteContacts(ctx, &maildto.DeleteContactsReq{IDs: []uint64{a, b}})
		if err == nil || deleted != 0 {
			t.Fatalf("DB 错误必须整批失败且计数为 0，实际 %d / %v", deleted, err)
		}
		for _, id := range []uint64{a, b} {
			if _, gerr := f.m.GetContact(ctx, id); gerr != nil {
				t.Fatalf("DB 错误后整批回滚，id=%d 不该被删：%v", id, gerr)
			}
		}
	})
}

// TestMailTagContactsAllOrNothing 覆盖整批打标签：同一事务、失败零提交、同值 no-op 成功、
// 以及改到第二个联系人失败时第一个人的标签回滚、标签自动化不触发。
func TestMailTagContactsAllOrNothing(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	seed := func(email string, tags ...string) uint64 {
		t.Helper()
		if err := f.m.CreateContact(ctx, &mailmodel.MailContactEntity{
			Email: email, Source: mailmodel.ContactSourceManual,
			Status: mailmodel.ContactStatusPending, Tags: mailmodel.StringArray(tags),
		}); err != nil {
			t.Fatalf("造联系人失败: %v", err)
		}
		row, err := f.m.GetContactByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}

	t.Run("合法全部成功", func(t *testing.T) {
		a, b := seed("tag-a@example.com"), seed("tag-b@example.com")
		changed, err := f.svc.TagContacts(ctx, &maildto.TagContactsReq{IDs: []uint64{a, b}, Add: []string{"vip"}})
		if err != nil || changed != 2 {
			t.Fatalf("合法整批应改 2 条，实际 %d / %v", changed, err)
		}
	})

	t.Run("不存在 id 整批回滚", func(t *testing.T) {
		a := seed("tag-mix@example.com")
		var maxID uint64
		if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id),0)").Scan(&maxID).Error; err != nil {
			t.Fatal(err)
		}
		changed, err := f.svc.TagContacts(ctx, &maildto.TagContactsReq{
			IDs: []uint64{a, maxID + 999999}, Add: []string{"should-not-land"},
		})
		if err == nil || changed != 0 {
			t.Fatalf("含不存在 id 必须整批拒绝且不计数，实际 %d / %v", changed, err)
		}
		row, gerr := f.m.GetContact(ctx, a)
		if gerr != nil {
			t.Fatal(gerr)
		}
		for _, tag := range row.Tags {
			if tag == "should-not-land" {
				t.Fatalf("整批回滚后合法行不该被改：%v", []string(row.Tags))
			}
		}
	})

	t.Run("改到第二个人失败时第一个人回滚且不触发自动化", func(t *testing.T) {
		a, b := seed("tag-dbfail-a@example.com"), seed("tag-dbfail-b@example.com")
		taggedID := seedAutomationWithParams(t, f, "打上 boom 标签跟进", mailmodel.TriggerTagAdded,
			map[string]any{"tag": "boom"})
		// 稳定顺序按 id 升序，故障注入第二个（b），第一个（a）已经写出标签再回滚。
		failUpdatingContact(t, f, b)
		changed, err := f.svc.TagContacts(ctx, &maildto.TagContactsReq{IDs: []uint64{a, b}, Add: []string{"boom"}})
		if err == nil || changed != 0 {
			t.Fatalf("DB 错误必须整批失败且不计数，实际 %d / %v", changed, err)
		}
		for _, id := range []uint64{a, b} {
			row, gerr := f.m.GetContact(ctx, id)
			if gerr != nil {
				t.Fatal(gerr)
			}
			if len(row.Tags) != 0 {
				t.Fatalf("DB 错误后 id=%d 的标签不该改动，实际 %v", id, []string(row.Tags))
			}
		}
		if got := runTotal(t, f, taggedID); got != 0 {
			t.Fatalf("回滚的事务不该触发 tag_added 自动化，实际实例数 %d", got)
		}
	})

	t.Run("同值 no-op 成功", func(t *testing.T) {
		a := seed("tag-noop@example.com", "vip")
		changed, err := f.svc.TagContacts(ctx, &maildto.TagContactsReq{IDs: []uint64{a}, Add: []string{"vip"}})
		if err != nil || changed != 0 {
			t.Fatalf("同值 no-op 应成功且不计改动，实际 %d / %v", changed, err)
		}
	})
}

// TestMailImportRollbackOnLateFailure 覆盖导入多批 SQL 同事务：
// 第二批插入失败时，第一批新增与既有联系人的更新都必须一起回滚，且不触发自动化。
func TestMailImportRollbackOnLateFailure(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if err := f.m.CreateContact(ctx, &mailmodel.MailContactEntity{
		Email: "existing@example.com", Source: mailmodel.ContactSourceManual,
		Status: mailmodel.ContactStatusPending, Tags: mailmodel.StringArray{"old"},
	}); err != nil {
		t.Fatal(err)
	}
	createdID := seedAutomationWithParams(t, f, "导入新联系人", mailmodel.TriggerContactCreated, nil)

	// 前 500 条插入（第一批）+ 既有联系人更新先完成，第二批首行触发异常：
	// 证明已经写过库的第一批新增与更新也回滚。
	const importBatch = 500
	var b strings.Builder
	b.WriteString("existing@example.com\n")
	for i := 0; i < importBatch+1; i++ {
		fmt.Fprintf(&b, "batch%d@example.com\n", i)
	}
	failInsertingEmail(t, f, fmt.Sprintf("batch%d@example.com", importBatch))

	_, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content: []byte(b.String()), UpdateExisting: true, DefaultTags: []string{"new"},
	})
	if err == nil {
		t.Fatal("第二批插入失败必须整批回滚")
	}
	for i := 0; i < importBatch+1; i++ {
		if _, gerr := f.m.GetContactByEmail(ctx, fmt.Sprintf("batch%d@example.com", i)); gerr == nil {
			t.Fatalf("第 %d 条（含第一批）也不该留下", i)
		}
	}
	row, gerr := f.m.GetContactByEmail(ctx, "existing@example.com")
	if gerr != nil {
		t.Fatal(gerr)
	}
	if len(row.Tags) != 1 || row.Tags[0] != "old" {
		t.Fatalf("已有联系人的更新也必须回滚，实际 %v", []string(row.Tags))
	}
	if got := runTotal(t, f, createdID); got != 0 {
		t.Fatalf("回滚的导入不该触发 contact_created 自动化，实际实例数 %d", got)
	}
}

// TestMailBatchDeleteRejectsSendingCampaign 覆盖活动批量删除的拒绝事实与计数归零。
func TestMailBatchDeleteRejectsSendingCampaign(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.svc.SetCipherSecret("mail-batch-test-secret")
	acc, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "营销账号", Purpose: mailmodel.AccountPurposeMarketing,
		FromEmail: "news@clker.cn", Host: "mail.clker.cn", Port: 587, Password: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.svc.SaveCampaign(ctx, &maildto.SaveCampaignReq{
		Name: "发送中", AccountID: acc.ID, TemplateID: 1, Subject: "主题",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.m.UpdateCampaignFields(ctx, item.ID, map[string]any{
		"status": mailmodel.CampaignStatusSending,
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.svc.DeleteCampaigns(ctx, &maildto.DeleteIDsReq{IDs: []uint64{item.ID}})
	if err == nil || deleted != 0 {
		t.Fatalf("发送中的活动必须整批拒绝且计数为 0，实际 %d / %v", deleted, err)
	}
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) || len(blocked.BlockedItems()) != 1 {
		t.Fatalf("应给单条拒绝事实，实际 %T", err)
	}
	if _, gerr := f.m.GetCampaign(ctx, item.ID); gerr != nil {
		t.Fatalf("被拒绝的活动不该被删：%v", gerr)
	}
}

// failDeletingContact 在真实 PG 上装一个只对指定联系人抛错的删除触发器。
func failDeletingContact(t *testing.T, f *fixture, id uint64) {
	t.Helper()
	mustExec(t, f, `
CREATE OR REPLACE FUNCTION mail_test_fail_delete() RETURNS trigger AS $$
BEGIN
    IF OLD.id = `+uintText(id)+` THEN
        RAISE EXCEPTION 'injected delete failure';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER mail_test_fail_delete_trg BEFORE DELETE ON mail_contacts
FOR EACH ROW EXECUTE FUNCTION mail_test_fail_delete();`)
	t.Cleanup(func() { dropMailTestTrigger(f, "mail_test_fail_delete_trg", "mail_test_fail_delete") })
}

// failUpdatingContact 在真实 PG 上装一个只对指定联系人抛错的更新触发器。
func failUpdatingContact(t *testing.T, f *fixture, id uint64) {
	t.Helper()
	mustExec(t, f, `
CREATE OR REPLACE FUNCTION mail_test_fail_update() RETURNS trigger AS $$
BEGIN
    IF NEW.id = `+uintText(id)+` THEN
        RAISE EXCEPTION 'injected update failure';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER mail_test_fail_update_trg BEFORE UPDATE ON mail_contacts
FOR EACH ROW EXECUTE FUNCTION mail_test_fail_update();`)
	t.Cleanup(func() { dropMailTestTrigger(f, "mail_test_fail_update_trg", "mail_test_fail_update") })
}

// failInsertingEmail 在真实 PG 上装一个只拒绝指定邮箱的插入触发器。
func failInsertingEmail(t *testing.T, f *fixture, email string) {
	t.Helper()
	mustExec(t, f, `
CREATE OR REPLACE FUNCTION mail_test_fail_insert() RETURNS trigger AS $$
BEGIN
    IF lower(NEW.email) = lower('`+email+`') THEN
        RAISE EXCEPTION 'injected insert failure';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER mail_test_fail_insert_trg BEFORE INSERT ON mail_contacts
FOR EACH ROW EXECUTE FUNCTION mail_test_fail_insert();`)
	t.Cleanup(func() { dropMailTestTrigger(f, "mail_test_fail_insert_trg", "mail_test_fail_insert") })
}

func mustExec(t *testing.T, f *fixture, sql string) {
	t.Helper()
	if err := f.db.Exec(sql).Error; err != nil {
		t.Fatalf("装测试触发器失败: %v", err)
	}
}

func dropMailTestTrigger(f *fixture, trigger, fn string) {
	_ = f.db.Exec(`DROP TRIGGER IF EXISTS ` + trigger + ` ON mail_contacts`).Error
	_ = f.db.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`).Error
}
