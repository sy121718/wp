package producthttp

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

func TestProductEditFormFieldsMatchUpdateReq(t *testing.T) {
	raw, err := os.ReadFile("../../../../templates/admin/product/product_edit.html")
	if err != nil {
		t.Fatalf("读编辑页模板失败: %v", err)
	}
	page := string(raw)

	idAt := strings.Index(page, `id="product-edit-main"`)
	if idAt < 0 {
		t.Fatal("编辑页模板里找不到 id=product-edit-main")
	}
	start := strings.LastIndex(page[:idAt], "<form")
	if start < 0 {
		t.Fatal("编辑页模板里找不到 <form 起始标签")
	}
	end := strings.Index(page[start:], "</form>")
	if end < 0 {
		t.Fatal("编辑页模板里找不到主表单的闭合标签")
	}
	form := page[start : start+end]

	for _, want := range []string{`hx-post="/api/product/update"`, `hx-target="#product-edit-result"`, `hx-swap="innerHTML"`, `hx-sync="this:drop"`, `data-write-builder="product-edit"`, `data-write-status`, `id="product-edit-result"`} {
		if !strings.Contains(form, want) {
			t.Errorf("主表单缺少明确交互声明 %s", want)
		}
	}
	for _, old := range []string{"data-write-url", "data-write-json-wrap", "data-write-lines", "data-write-keep-empty", "data-write-reload"} {
		if strings.Contains(form, old) {
			t.Errorf("主表单仍使用退役提交属性 %s", old)
		}
	}
	// 富文本控件由页面专用对象提供器组装为 description 对象，不走通用字段整形。
	renames := map[string]string{"descriptionHtml": "description"}

	allowed := map[string]bool{}
	typ := reflect.TypeOf(productdto.UpdateReq{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name != "" && name != "-" {
			allowed[name] = true
		}
	}

	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(form, -1) {
		names[m[1]] = true
	}
	delete(names, "csrf_token")

	for name := range names {
		key := name
		if target, ok := renames[name]; ok {
			key = target
		}
		if !allowed[key] {
			t.Errorf("表单字段 name=%q 在 UpdateReq 里没有对应的 json 键（严格解码会整条 400）", name)
		}
	}

	for _, want := range []string{"name", "slug", "sku", "status", "defaultPrice", "weight", "attributeIds", "categoryIds", "tagIds", "primaryCategoryId", "brandId"} {
		if !names[want] {
			t.Errorf("主表单缺少协议字段 name=%q", want)
		}
	}

	if !strings.Contains(page, `<script defer src="/static/js/ui/product-edit.js"></script>`) {
		t.Error("主体编辑应按页加载结构化对象提供器")
	}
}
