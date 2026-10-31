package mailservice

import (
	"testing"

	mailmodel "go_wp/internal/module/mail/model"
)

func TestAutomationsForTriggerMatching(t *testing.T) {
	list := []*mailmodel.MailAutomationEntity{
		{ID: 1, TriggerType: mailmodel.TriggerTagAdded, TriggerParams: mailmodel.JSONMap{"tag": "vip"}},
		{ID: 2, TriggerType: mailmodel.TriggerTagAdded},
		{ID: 3, TriggerType: mailmodel.TriggerContactCreated},
		nil,
	}

	got := automationsForTrigger(list, mailmodel.TriggerTagAdded, nil)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("extra=nil 时只应匹配无条件的 tag_added 流程（实际 %d 条）", len(got))
	}

	got = automationsForTrigger(list, mailmodel.TriggerTagAdded, map[string]any{"tag": "vip"})
	if len(got) != 2 {
		t.Fatalf("tag=vip 应匹配 2 条（带条件的那条 + 无条件通配），实际 %d", len(got))
	}

	got = automationsForTrigger(list, mailmodel.TriggerContactCreated, nil)
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("contact_created 应匹配 1 条，实际 %d", len(got))
	}

	if got := automationsForTrigger(nil, mailmodel.TriggerTagAdded, nil); len(got) != 0 {
		t.Fatalf("空流程集不该匹配出任何流程，实际 %d", len(got))
	}
}

func TestHasTriggerTypeIgnoresParams(t *testing.T) {
	list := []*mailmodel.MailAutomationEntity{

		{ID: 1, TriggerType: mailmodel.TriggerTagAdded, TriggerParams: mailmodel.JSONMap{"tag": "vip"}},
	}
	if !hasTriggerType(list, mailmodel.TriggerTagAdded) {
		t.Fatal("带 tag 条件的流程也算「有 tag_added 流程」—— 粗判不能走参数匹配")
	}
	if hasTriggerType(list, mailmodel.TriggerContactCreated) {
		t.Fatal("没有该类流程时不该判为存在")
	}
	if hasTriggerType(nil, mailmodel.TriggerTagAdded) {
		t.Fatal("空流程集不该判为存在")
	}
}
