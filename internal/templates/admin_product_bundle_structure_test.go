package templates

// admin_product_bundle_structure_test.go — 捆绑配置页与 ui/bundle_members.js 的契约对账。
//
// 这一页原来把「按来源解析并追加成员」的 150 行脚本内联在模板里，迁到
// static/js/ui/bundle_members.js 之后，模板与脚本之间只剩 data-* 契约 —— 契约漂移的症状是
// 「按钮点了没反应 / 追加的行字段是空的」，两种都不报错。所以这里逐项对账：
//
//  1. 模板按外链引入脚本，且解析按钮带 data-requires-js hidden disabled（无 JS 时不可点）；
//  2. 脚本里出现的每一个 data-bundle-* / data-member-* 选择器，两个模板文件里都有对应属性；
//  3. 脚本按 name 取的行内字段，表单里都真的用这个名字（改名只改一侧 = 静默丢值）。
import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	bundlePageTemplate = "admin/product/product_bundle.html"
	bundleFormTemplate = "admin/product/product_bundle_form.html"
	bundleMembersJS    = "static/js/ui/bundle_members.js"
)

func readBundleTemplateFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", rel, err)
	}
	return string(raw)
}

func TestBundlePageLoadsMembersScriptExternally(t *testing.T) {
	page := readBundleTemplateFile(t, bundlePageTemplate)

	// 脚本走分partials/ui_scripts.html 的公共入口（ui/*.js 必须全部登记在那里，
	// 由 TestUIAssetEntry 钉住）；模板自己再引一次会让事件绑两遍、点一次发两个请求。
	entry := readBundleTemplateFile(t, "partials/ui_scripts.html")
	if !strings.Contains(entry, `src="/static/js/ui/bundle_members.js"`) {
		t.Fatalf("ui_scripts.html 没有引入 ui/bundle_members.js")
	}
	if strings.Contains(page, `src="/static/js/ui/bundle_members.js"`) {
		t.Fatalf("捆绑配置页重复引入 ui/bundle_members.js：公共入口已经加载过，重复加载会把事件绑两遍")
	}
	if n := inlineScripts(t, bundlePageTemplate); n != 0 {
		t.Fatalf("捆绑配置页仍有 %d 处内联脚本：成员解析交互应在 ui/bundle_members.js 里", n)
	}

	// 解析按钮：无 JS 时必须不可用（它是 fetch 取数 + 追加行，不是原生提交）。
	btn := regexp.MustCompile(`(?s)<button[^>]*data-bundle-resolve[^>]*>`).FindString(page)
	if btn == "" {
		t.Fatalf("找不到 data-bundle-resolve 按钮")
	}
	for _, attr := range []string{"data-requires-js", " hidden", " disabled"} {
		if !strings.Contains(btn, attr) {
			t.Errorf("解析按钮缺少 %q：无 JS 时它会变成按了没反应的死按钮", strings.TrimSpace(attr))
		}
	}
}

// JS 里的选择器契约必须能在模板里找到落点。
func TestBundleMembersSelectorsExistInTemplates(t *testing.T) {
	js := readBundleTemplateFile(t, bundleMembersJS)
	both := readBundleTemplateFile(t, bundlePageTemplate) + readBundleTemplateFile(t, bundleFormTemplate)

	selRe := regexp.MustCompile(`data-(?:bundle|member)-[a-z-]+`)
	found := map[string]bool{}
	for _, sel := range selRe.FindAllString(js, -1) {
		found[sel] = true
	}
	var missing []string
	for sel := range found {
		if !strings.Contains(both, sel) {
			missing = append(missing, sel)
		}
	}
	sort.Strings(missing)
	for _, sel := range missing {
		t.Errorf("ui/bundle_members.js 引用了 %s，但两个模板里都没有这个属性（脚本会静默找不到节点）", sel)
	}
}

// 脚本按 name 取的行内字段，必须真的存在于表单里。
func TestBundleMembersRowFieldNamesExist(t *testing.T) {
	js := readBundleTemplateFile(t, bundleMembersJS)
	form := readBundleTemplateFile(t, bundleFormTemplate)

	fields := []string{"variantId", "sourceKind", "memberWarehouseId", "memberWarehouseSku", "memberExternalSku"}
	for _, name := range fields {
		if !strings.Contains(js, name) {
			t.Fatalf("bundle_members.js 未再处理字段 %s —— 若确已删除，请同步收掉这条断言", name)
		}
		if !strings.Contains(form, `name="`+name+`"`) && !strings.Contains(form, ";"+name+",") && !strings.Contains(form, ","+name+",") {
			t.Errorf("表单里既没有 name=%q 控件，也没有在 data-write-rows 的字段清单里出现 %s", name, name)
		}
	}
}
