package templates

// workbench_reuse_insert_test.go — 复用资产的两种插入动作（审计 VIS-013）。
//
// 后端两条路径早就完备：引用 = core.globalref 节点（构建期经 BlockResolver 展开，
// 跟随原块变化），复制 = /api/block/clone 生成独立 AST（此后与源块互不影响）。
// 缺的一直是前端把「这次插入要哪一种」暴露给编辑者。
//
// 这类「后端就绪、前端未接线」的问题不会在 Go 侧的功能测试里暴露（服务端两条路径
// 都能跑通），因此这里直接对工作台源码断言契约 —— 断言的是**分支存在**，
// 而不是按钮长什么样：少一条分支就意味着某一类块无法按预期插入。

import (
	"os"
	"strings"
	"testing"
)

func readTemplateFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(data)
}

// TestWorkbenchBindingFieldHonoursPrefixes 字段绑定下拉按组件声明的 prefixes 过滤（审计 EDT-006）。
//
// 组件用 ct tag 的 prefixes 声明自己能接受哪一类字段前缀（详情页组件只收 product.*，
// 卡片 / 列表收 item.* 与 product.*）。前端必须真的读它并过滤 —— 声明了不读，
// 编辑器里就照样会列出用不了的选项，而选错的代价要等构建期才暴露。
func TestWorkbenchBindingFieldHonoursPrefixes(t *testing.T) {
	misc := readTemplateFile(t, "static/js/workbench/methods/controls/misc.js")
	if !strings.Contains(misc, "ctl.prefixes") {
		t.Fatalf("字段绑定控件应读取组件声明的 prefixes")
	}
	if !strings.Contains(misc, "bindingFieldControl(ctx, label, path, ctl)") {
		t.Fatalf("控件分发应把 ctl 传给 bindingFieldControl（否则拿不到 prefixes）")
	}
	// 手动输入兜底：数据源未注册或元数据接口不可用时下拉是空的，
	// 没有兜底就等于「组件配不出来」；白名单由服务端校验守住，下拉只是便利。
	if !strings.Contains(misc, "或手动输入字段路径") {
		t.Fatalf("字段绑定控件应保留下拉之外的手动输入兜底")
	}
}

// TestWorkbenchStructuredConfigControls 结构化配置控件的双侧到位（审计 EDT-007）。
//
// 服务端渲染 HTML、客户端按 data-wb-kind 取值回写 —— 只有一侧到位时控件照样显示，
// 但改完提交不上去（面板看起来正常，值就是不落库），这类问题在 Go 侧测试里看不见。
//
// 同时钉住**值格式**：勾选与行编辑都必须拼回既有的逗号分隔串，
// 换了格式意味着既存文档要迁移、构建期解析要改 —— 那是另一件事，不该夹带在这条里。
func TestWorkbenchStructuredConfigControls(t *testing.T) {
	tpl := readTemplateFile(t, "fragments/inspector_panel.html")
	js := readTemplateFile(t, "static/js/workbench/methods/inspector.js")
	for _, kind := range []string{"multientityref", "rangelist"} {
		if !strings.Contains(tpl, "data-wb-kind=\""+kind+"\"") {
			t.Fatalf("检查器模板缺少 %s 控件分支", kind)
		}
		if !strings.Contains(js, "kind === '"+kind+"'") {
			t.Fatalf("客户端缺少 %s 的取值分支（控件会显示但提交不了）", kind)
		}
	}
	if !strings.Contains(js, "picked.join(',')") {
		t.Fatalf("多选实体应拼回逗号分隔 id 串")
	}
	if !strings.Contains(js, "ranges.join(',')") {
		t.Fatalf("区间列表应拼回逗号分隔档位串")
	}
}

// TestWorkbenchSaveDraftFollowsTargetDescriptor 保存按后端下发的目标描述符走（审计 EDT-017）。
//
// 此前 saveDraft 里是 page / block / template 三段 if：接入一种新文档类型要在保存、
// 预览、校验、历史四处各加一条分支，而分派散在 JS 里 —— 漏改一处不会编译失败，
// 只会在用户点保存时表现为「什么都没发生」。
//
// 这里只看 saveDraft 这一段：api() 仍按 saveBase 拼前缀（手工页面的路径语义），
// 那是另一件事，不该被这条断言牵连。
func TestWorkbenchSaveDraftFollowsTargetDescriptor(t *testing.T) {
	js := readTemplateFile(t, "static/js/workbench/methods/api.js")
	start := strings.Index(js, "saveDraft() {")
	if start < 0 {
		t.Fatalf("未找到 saveDraft 实现")
	}
	rest := js[start:]
	end := strings.Index(rest, "publishFlow() {")
	if end < 0 {
		t.Fatalf("未找到 saveDraft 的结束边界（publishFlow）")
	}
	body := rest[:end]
	if !strings.Contains(body, "meta.target") {
		t.Fatalf("saveDraft 应读后端下发的目标描述符")
	}
	if !strings.Contains(body, "saveBody") {
		t.Fatalf("saveDraft 应按描述符构造请求体（键名由后端给）")
	}
	if strings.Contains(body, "saveBase") {
		t.Fatalf("saveDraft 不应再按 saveBase 分派（描述符就是用来取代它的）")
	}
}

// TestWorkbenchReuseInsertActions 引用 / 复制两条插入路径都已接线。
func TestWorkbenchReuseInsertActions(t *testing.T) {
	canvas := readTemplateFile(t, "static/js/workbench/methods/canvas.js")

	// 引用：插入只带 blockId 的节点（由构建期展开，原块改动会传播过来）。
	if !strings.Contains(canvas, "'core.globalref'") {
		t.Fatalf("工作台应能插入 core.globalref 引用节点")
	}
	if !strings.Contains(canvas, "globalRefItem") {
		t.Fatalf("缺少引用插入的节点描述构造（globalRefItem）")
	}

	// 复制：走 /api/block/clone 生成独立副本。
	if !strings.Contains(canvas, "insertBlockClone") {
		t.Fatalf("工作台应能插入独立副本（insertBlockClone）")
	}
	if !strings.Contains(canvas, "reuseMode") {
		t.Fatalf("插入动作应按块的 reuse_mode 分流（global 引用 / template 复制）")
	}
	if !strings.Contains(canvas, "shiftKey") {
		t.Fatalf("global 块应支持 Shift+点击强制复制（两种语义都要能显式选择）")
	}

	// 后端入口必须真的存在：前端调的接口没有路由时，错误只在浏览器控制台里出现。
	router := readTemplateFile(t, "../module/block/inbound/http/block_http.go")
	if !strings.Contains(router, "\"/clone\"") {
		t.Fatalf("block 路由应注册 /clone（前端 insertBlockClone 的目标接口）")
	}
}
