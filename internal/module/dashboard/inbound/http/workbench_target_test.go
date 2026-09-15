package dashboardhttp

// workbench_target_test.go — 编辑目标描述符（审计 EDT-017）。
//
// 描述符的意义是「新增目标类型只需注册，前端零改动」。因此这里钉两类事：
// 注册表自洽（每个目标都拿得出手的保存描述），以及各目标的差异点确实在数据里
//（不是靠前端记 if）。

import "testing"

func TestWorkbenchTargetRegistry(t *testing.T) {
	// 1) 三种已迁移的目标都在注册表里，未注册的类型明确返回 false
	//（调用方据此 panic / 报错，不静默降级）。
	for _, want := range []string{EditTargetPage, EditTargetBlock, EditTargetTemplate} {
		target, ok := EditTargetFor(want)
		if !ok {
			t.Fatalf("目标 %s 未注册", want)
		}
		if target.Type != want {
			t.Fatalf("目标 %s 的类型字段应是自身，实际 %q", want, target.Type)
		}
		// 保存端点是描述符的最小可用条件：缺失时前端只能退回默认行为，
		// 而「退回默认」对 block / template 就是错的端点。
		if target.Save.Path == "" {
			t.Fatalf("目标 %s 缺少保存端点", want)
		}
		if target.SaveBody.IDKey == "" || target.SaveBody.DocumentKey == "" {
			t.Fatalf("目标 %s 缺少请求体键名（idKey/documentKey）", want)
		}
	}
	if _, ok := EditTargetFor("nonsense"); ok {
		t.Fatalf("未注册的类型应返回 false")
	}

	// 2) 差异点必须在数据里，而不是前端记的 if。
	page, _ := EditTargetFor(EditTargetPage)
	if page.SaveBody.VersionKey == "" || page.SaveBody.PathKey == "" {
		t.Fatalf("手工页面应带乐观锁版本键与路径键，实际 %+v", page.SaveBody)
	}
	if !page.Caps.Publish || !page.Caps.History {
		t.Fatalf("手工页面应有发布与历史能力，实际 %+v", page.Caps)
	}

	block, _ := EditTargetFor(EditTargetBlock)
	if block.SaveBody.Extras["name"] == "" {
		t.Fatalf("全局块保存需要带 name 字段，实际 %+v", block.SaveBody)
	}
	if block.Caps.Publish || block.Caps.URL {
		t.Fatalf("全局块没有独立发布链与 URL，能力开关应为关，实际 %+v", block.Caps)
	}

	tpl, _ := EditTargetFor(EditTargetTemplate)
	if tpl.SaveBody.VersionKey != "" {
		t.Fatalf("内容模板没有乐观锁版本键，实际 %q", tpl.SaveBody.VersionKey)
	}
	if tpl.SaveBody.DocumentKey == page.SaveBody.DocumentKey {
		// 这两个当前恰好相同（都是 draftDocument），这里只是提醒：
		// 一旦哪个端点改了键名，差异必须体现在描述符里而不是前端分支里。
		t.Logf("模板与页面的文档键名相同（%s）—— 保持时无需额外分支", tpl.SaveBody.DocumentKey)
	}

	// 3) 列表输出的确定性（注册表是 map，直接遍历顺序随机）。
	list := WorkbenchTargets()
	if len(list) != 3 {
		t.Fatalf("应列出 3 个目标，实际 %d", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].Type >= list[i].Type {
			t.Fatalf("目标列表应按类型排序，实际 %v", []string{list[i-1].Type, list[i].Type})
		}
	}
}
