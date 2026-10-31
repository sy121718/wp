package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

func seedAutomationWithParams(t *testing.T, f *fixture, name, trigger string, params map[string]any) uint64 {
	t.Helper()
	def := map[string]any{"entry": "n1", "nodes": []any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "end", nil, ""),
	}}
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.svc.SaveAutomation(context.Background(), &maildto.SaveAutomationReq{
		Name: name, TriggerType: trigger, Definition: raw, TriggerParams: params,
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	if err := f.svc.SetAutomationStatus(context.Background(), &maildto.SetAutomationStatusReq{
		ID: item.ID, Status: mailmodel.AutomationStatusActive,
	}); err != nil {
		t.Fatalf("启用流程失败: %v", err)
	}
	return item.ID
}

func runTotal(t *testing.T, f *fixture, automationID uint64) int64 {
	t.Helper()
	_, total, err := f.m.ListRuns(context.Background(), automationID, "", 0, 50)
	if err != nil {
		t.Fatalf("读实例列表失败: %v", err)
	}
	return total
}

func completeRuns(t *testing.T, f *fixture, automationID uint64) {
	t.Helper()
	list, _, err := f.m.ListRuns(context.Background(), automationID, "", 0, 50)
	if err != nil {
		t.Fatalf("读实例列表失败: %v", err)
	}
	for _, r := range list {
		if err := f.m.UpdateRunFields(context.Background(), r.ID, map[string]any{
			"status": mailmodel.RunStatusCompleted,
		}); err != nil {
			t.Fatalf("收尾实例失败: %v", err)
		}
	}
}

func TestImportContactsFiresCreatedAndTagAdded(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	createdID := seedAutomationWithParams(t, f, "新联系人欢迎", mailmodel.TriggerContactCreated, nil)
	taggedID := seedAutomationWithParams(t, f, "打上 vip 标签跟进", mailmodel.TriggerTagAdded,
		map[string]any{"tag": "vip"})

	res, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content:     []byte("newbie@example.com\n"),
		DefaultTags: []string{"vip"},
	})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("应导入 1 个联系人，实际 %d", res.Imported)
	}
	if got := runTotal(t, f, createdID); got != 1 {
		t.Errorf("导入新联系人应触发 contact_created 流程（实例数 %d，期望 1）", got)
	}
	if got := runTotal(t, f, taggedID); got != 1 {
		t.Errorf("导入带 vip 标签的联系人应触发 tag_added 流程（实例数 %d，期望 1）", got)
	}

	completeRuns(t, f, createdID)
	completeRuns(t, f, taggedID)

	if _, err = f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content:        []byte("newbie@example.com\n"),
		DefaultTags:    []string{"vip"},
		UpdateExisting: true,
	}); err != nil {
		t.Fatalf("二次导入失败: %v", err)
	}
	if got := runTotal(t, f, taggedID); got != 1 {
		t.Errorf("标签没有新增时不该重复触发 tag_added（实例数 %d，期望 1）", got)
	}
	if got := runTotal(t, f, createdID); got != 1 {
		t.Errorf("已存在的联系人再次导入不该触发 contact_created（实例数 %d，期望 1）", got)
	}

	proID := seedAutomationWithParams(t, f, "打上 pro 标签跟进", mailmodel.TriggerTagAdded,
		map[string]any{"tag": "pro"})
	if _, err = f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content:        []byte("newbie@example.com\n"),
		DefaultTags:    []string{"vip", "pro"},
		UpdateExisting: true,
	}); err != nil {
		t.Fatalf("三次导入失败: %v", err)
	}
	if got := runTotal(t, f, proID); got != 1 {
		t.Errorf("导入新增了标签 pro，应触发 tag_added(tag=pro) 流程（实例数 %d，期望 1）", got)
	}

	if got := runTotal(t, f, taggedID); got != 1 {
		t.Errorf("vip 标签没有新增，不该重复触发（实例数 %d，期望 1）", got)
	}
}

func TestImportManyContactsBatchTriggerPerContact(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	createdID := seedAutomationWithParams(t, f, "批量新联系人", mailmodel.TriggerContactCreated, nil)
	taggedID := seedAutomationWithParams(t, f, "批量 vip 跟进", mailmodel.TriggerTagAdded,
		map[string]any{"tag": "vip"})

	res, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content:     []byte("batch1@example.com\nbatch2@example.com\nbatch3@example.com\n"),
		DefaultTags: []string{"vip"},
	})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Imported != 3 {
		t.Fatalf("应导入 3 个联系人，实际 %d", res.Imported)
	}
	if got := runTotal(t, f, createdID); got != 3 {
		t.Errorf("批量导入 3 个新联系人应各建一个 contact_created 实例，实际 %d", got)
	}
	if got := runTotal(t, f, taggedID); got != 3 {
		t.Errorf("批量导入 3 个带 vip 标签的联系人应各建一个 tag_added 实例，实际 %d", got)
	}
}
