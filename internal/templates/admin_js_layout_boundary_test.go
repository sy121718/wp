package templates

// admin_js_layout_boundary_test.go — 后台 JS 的落点边界：根目录只减不增。
//
// 规则（AGENTS.md「交互方式（HTMX）」）：JS 只做 HTMX 覆盖不到的控件层，收敛在
// static/js/ui/ 与 rich-editor/（媒体库选择器 media-lib.js、构建器 workbench/）。
// 根目录下这几个文件是**存量散落**，每个都有明确的收敛方向；新写的业务 JS 必须落在
// ui/ 下并接入 partials/ui_scripts.html 的公共入口（TestUIAssetEntry 钉住后者）。
//
// 为什么要有这条判据：散落 JS 的失败形态是「同一个交互有两份实现」（一份在 ui/、一份在
// 页面根目录），改一处而另一处不改，症状是「某个入口正常、另一个入口按了没反应」——
// 不报错、测试也不红。product 的捆绑配置页就踩过：150 行内联脚本与 ui/ 下的控件并存。
//
// 双向失败：根目录出现未登记的文件 → 失败；登记了却已不在 → 也失败（条目不得退化成注释）。
import (
	"os"
	"sort"
	"strings"
	"testing"
)

// adminJSRootAllowance static/js 根目录下允许存在的脚本（相对 static/js 的文件名）与收敛方向。
var adminJSRootAllowance = map[string]string{
	"admin.js":               "后台侧边栏状态 + 上级菜单候选过滤；控件部分已迁 ui/，剩余随页面迁移逐步清零",
	"media-lib.js":           "媒体库公共逻辑（/admin/media 与工作台弹窗共用），被 mediafield.js / media-gallery.js 依赖",
	"media-admin.js":         "媒体库页面的左树右库业务逻辑，依赖 media-lib.js；页面专用，未进公共入口",
	"product-create-form.js": "商品新建表单的仓库候选过滤与主体 SKU 预览；只服务 products.html 与 products_new.html 两页",
	"enhance.js":             "构建产物（访客静态页）的客户端增强，构建期内联，不属于后台控件层",
	"track.js":               "访问归因采集，构建期无条件内联进产物，长期保留",
	"icons.js":               "内置图标数据（与 Go core/icons.go 同源），性质是 vendor 数据表",
}

func TestAdminJSRootDirectoryDoesNotGrow(t *testing.T) {
	const root = "static/js"
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", root, err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		seen[e.Name()] = true
		if _, ok := adminJSRootAllowance[e.Name()]; !ok {
			t.Errorf("static/js/%s 是新的根目录业务脚本：请放到 static/js/ui/ 下并接入 "+
				"partials/ui_scripts.html（形态见 ui/bundle_members.js）；确属页面专用也应在 ui/ 下按页引入",
				e.Name())
		}
	}
	var stale []string
	for name := range adminJSRootAllowance {
		if !seen[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("adminJSRootAllowance 里的 %s 已不存在：删掉这一条", name)
	}
}
