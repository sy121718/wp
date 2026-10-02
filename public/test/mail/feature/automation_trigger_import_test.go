package feature

// automation_trigger_import_test.go — 「新联系人产生 / 被打上某个标签」这两个触发器的
// **真实入口**验证（issue #38 P3）。
//
// 为什么单独一个文件：这两个触发器此前在生产代码里**零调用方**（只有定义与注释），
// 而后台 UI 一直提供这两项、同族的 opened / clicked / subscribed 都已接好 ——
// 表现是「后台把流程设成这两个触发器 → 流程永不启动」，导入联系人与手工打标签
// 都不产生实例，列表页看不出任何异常。这里把「导入 → 实例产生」这条链钉死。

import (
	"context"
	"encoding/json"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// seedAutomationWithParams 建流程 + 启用，并带上触发条件（tag_added 需要它）。
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

// runTotal 当前流程的实例条数。
func runTotal(t *testing.T, f *fixture, automationID uint64) int64 {
	t.Helper()
	_, total, err := f.m.ListRuns(context.Background(), automationID, "", 0, 50)
	if err != nil {
		t.Fatalf("读实例列表失败: %v", err)
	}
	return total
}

// completeRuns 把该流程的实例全部置为完成态。
//
// 需要它是因为引擎对「同一人同流程只允许一个进行中实例」做了幂等（部分唯一索引）：
// 不把实例收尾，「重复导入没有再次触发」与「触发了但被幂等拦住」在计数上完全同形，
// 断言就失去了区分力。
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

// TestImportContactsFiresCreatedAndTagAdded 导入联系人触发两个此前死掉的触发器；
// 重复导入同一个标签不重复触发，导入带来的**新增**标签才触发。
func TestImportContactsFiresCreatedAndTagAdded(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	createdID := seedAutomationWithParams(t, f, "新联系人欢迎", mailmodel.TriggerContactCreated, nil)
	taggedID := seedAutomationWithParams(t, f, "打上 vip 标签跟进", mailmodel.TriggerTagAdded,
		map[string]any{"tag": "vip"})

	// 首次导入：新增一个带 vip 标签的联系人。
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

	// 收尾，便于下面区分「没触发」与「触发了被幂等拦住」。
	completeRuns(t, f, createdID)
	completeRuns(t, f, taggedID)

	// 再次导入同一个联系人、同一个标签（标签没有新增）：
	// tag_added 不该再触发（差集为空），contact_created 也不该（不是新联系人）。
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

	// 再导入一次、这次多带一个标签 pro：新增的那个才是触发源（差集正向验证）。
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
	// vip 没有变化：即便流程条件匹配的是 vip，也不该因为它再次触发。
	if got := runTotal(t, f, taggedID); got != 1 {
		t.Errorf("vip 标签没有新增，不该重复触发（实例数 %d，期望 1）", got)
	}
}

// TestImportManyContactsBatchTriggerPerContact 多行导入（走**批量**入口）时，
// 每个联系人都必须建到实例 —— 批量改造不能以「少建几个」为代价换语句数。
//
// 这条与 TestImportWriteStatementProfile 是一对：那条守「语句数不退化」，
// 这条守「批量路径的实例数一个不少」。批量入口内部仍是逐联系人建实例，本用例把它钉住。
func TestImportManyContactsBatchTriggerPerContact(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	createdID := seedAutomationWithParams(t, f, "批量新联系人", mailmodel.TriggerContactCreated, nil)
	taggedID := seedAutomationWithParams(t, f, "批量 vip 跟进", mailmodel.TriggerTagAdded,
		map[string]any{"tag": "vip"})

	// 3 行 → 走批量入口（单行才走单条入口）。
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
