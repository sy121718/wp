package feature

// automation_layout_test.go — 画布位置保存（issue #38 P4）。
//
// 位置是编辑器的布局数据，不是流程语义。三条不能错的：
//  1. 保存位置**不推进版本号** —— 否则挪一下节点，所有在跑的实例都「版本落后」，排障页会误导人；
//  2. 保存位置**不动结构** —— 节点与连线必须原样保留；
//  3. 不认识的 key 忽略（可能来自另一个标签页的旧画布），不报错也不写脏数据。

import (
	"context"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// TestSaveLayoutKeepsVersionAndStructure 保存位置：版本不变、结构不变、位置真的写进去了。
func TestSaveLayoutKeepsVersionAndStructure(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := seedAutomation(t, f, "画布流程", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "delay", map[string]any{"minutes": 30}, "n3"),
		newNode("n3", "end", nil, ""),
	}, mailmodel.TriggerManual)

	before, err := f.svc.GetAutomation(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version == 0 {
		t.Fatal("版本号应从 1 起")
	}

	// 挪 n1 与 n2 的位置。
	if err = f.svc.SaveAutomationLayout(ctx, &maildto.SaveAutomationLayoutReq{
		ID: id,
		Positions: map[string]maildto.AutomationPosition{
			"n1": {X: 120, Y: 40},
			"n2": {X: 120, Y: 200},
		},
	}); err != nil {
		t.Fatalf("保存位置失败: %v", err)
	}

	after, err := f.svc.GetAutomation(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// 1. 版本号不动。
	if after.Version != before.Version {
		t.Fatalf("保存位置不该推进版本号（%d → %d）—— 否则在跑的实例会全部「版本落后」", before.Version, after.Version)
	}
	// 2. 结构不动（节点数、类型、连线都还在）。
	if len(after.Nodes) != 3 {
		t.Fatalf("节点数应保持 3，实际 %d", len(after.Nodes))
	}
	byKey := map[string]maildto.AutomationNodeItem{}
	for _, n := range after.Nodes {
		byKey[n.Key] = n
	}
	if byKey["n1"].Next != "n2" || byKey["n2"].Next != "n3" {
		t.Fatalf("连线应原样保留: %+v", after.Nodes)
	}
	if byKey["n2"].Type != "delay" {
		t.Fatalf("节点类型应原样保留: %+v", byKey["n2"])
	}
	// 3. 位置写进去了。
	if byKey["n1"].X != 120 || byKey["n1"].Y != 40 {
		t.Fatalf("n1 位置没保存: %+v", byKey["n1"])
	}
	if byKey["n2"].Y != 200 {
		t.Fatalf("n2 位置没保存: %+v", byKey["n2"])
	}
	// n3 没传位置：保持 0，不该被清成别的值，也不该报错。
	if byKey["n3"].X != 0 || byKey["n3"].Y != 0 {
		t.Fatalf("未传位置的节点应保持 0: %+v", byKey["n3"])
	}
}

// TestSaveLayoutIgnoresUnknownKeys 不认识的节点 key 被忽略，不影响已有结构。
func TestSaveLayoutIgnoresUnknownKeys(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := seedAutomation(t, f, "忽略未知 key", "n1", []map[string]any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "end", nil, ""),
	}, mailmodel.TriggerManual)
	if err := f.svc.SaveAutomationLayout(ctx, &maildto.SaveAutomationLayoutReq{
		ID: id,
		Positions: map[string]maildto.AutomationPosition{
			"ghost": {X: 999, Y: 999},
			"n1":    {X: 10, Y: 20},
		},
	}); err != nil {
		t.Fatalf("未知 key 应被忽略而不是报错: %v", err)
	}
	item, err := f.svc.GetAutomation(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Nodes) != 2 {
		t.Fatalf("结构不该被改变，实际 %d 个节点", len(item.Nodes))
	}

	for _, n := range item.Nodes {
		if n.Key == "n1" && (n.X != 10 || n.Y != 20) {
			t.Fatalf("已知 key 的位置应写入: %+v", n)
		}
	}

}

// TestSaveLayoutValidation 参数校验。
func TestSaveLayoutValidation(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	if err := f.svc.SaveAutomationLayout(context.Background(), nil); err == nil {
		t.Fatal("nil 请求应报错")
	}
	if err := f.svc.SaveAutomationLayout(context.Background(), &maildto.SaveAutomationLayoutReq{ID: 0}); err == nil {
		t.Fatal("id 为 0 应报错")
	}
	if err := f.svc.SaveAutomationLayout(context.Background(), &maildto.SaveAutomationLayoutReq{ID: 999999,
		Positions: map[string]maildto.AutomationPosition{"n1": {X: 1, Y: 1}}}); err == nil {
		t.Fatal("不存在的流程应报错")
	}
}
