package templates

import (
	"os"
	"strings"
	"testing"
)

// readTemplateFile 读源码文件：静态资产走复合 FS，模板与 Go 源码直接读磁盘。
func readTemplateFile(t *testing.T, path string) string {
	t.Helper()
	if strings.HasPrefix(path, "static/") {
		b, err := AssetFile(strings.TrimPrefix(path, "static/"))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		return string(b)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}

func TestWorkbenchBindingFieldHonoursPrefixes(t *testing.T) {
	misc := readTemplateFile(t, "static/js/workbench/methods/controls/misc.js")
	if !strings.Contains(misc, "ctl.prefixes") {
		t.Fatalf("字段绑定控件应读取组件声明的 prefixes")
	}
	if !strings.Contains(misc, "bindingFieldControl(ctx, label, path, ctl)") {
		t.Fatalf("控件分发应把 ctl 传给 bindingFieldControl（否则拿不到 prefixes）")
	}

	if !strings.Contains(misc, "或手动输入字段路径") {
		t.Fatalf("字段绑定控件应保留下拉之外的手动输入兜底")
	}
}

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

func TestWorkbenchSaveDraftFollowsTargetDescriptor(t *testing.T) {
	js := readTemplateFile(t, "static/js/workbench/methods/api.js")
	for _, want := range []string{"meta.target", "target.save.path", "this.saveBody(savedDocument)", "await this.persistDraft(savedDocument)"} {
		if !strings.Contains(js, want) {
			t.Errorf("保存调用链缺少 %q", want)
		}
	}
}

func TestWorkbenchReuseInsertActions(t *testing.T) {
	canvas := readTemplateFile(t, "static/js/workbench/methods/canvas.js")

	if !strings.Contains(canvas, "'core.globalref'") {
		t.Fatalf("工作台应能插入 core.globalref 引用节点")
	}
	if !strings.Contains(canvas, "globalRefItem") {
		t.Fatalf("缺少引用插入的节点描述构造（globalRefItem）")
	}

	if !strings.Contains(canvas, "insertBlockClone") {
		t.Fatalf("工作台应能插入独立副本（insertBlockClone）")
	}
	if !strings.Contains(canvas, "reuseMode") {
		t.Fatalf("插入动作应按块的 reuse_mode 分流（global 引用 / template 复制）")
	}
	if !strings.Contains(canvas, "shiftKey") {
		t.Fatalf("global 块应支持 Shift+点击强制复制（两种语义都要能显式选择）")
	}

	router := readTemplateFile(t, "../module/block/inbound/http/block_router.go")
	if !strings.Contains(router, "\"/clone\"") {
		t.Fatalf("block 路由应注册 /clone（前端 insertBlockClone 的目标接口）")
	}
}
