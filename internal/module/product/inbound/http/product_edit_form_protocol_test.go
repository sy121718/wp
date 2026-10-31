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

	if !strings.Contains(form, `data-write-url="/api/product/update"`) {
		t.Fatalf("主表单缺少 data-write-url=/api/product/update")
	}
	renames := map[string]string{}
	htmlSpec := regexp.MustCompile(`data-write-json-wrap="([^"]*)"`).FindStringSubmatch(form)
	if len(htmlSpec) == 2 {
		for _, pair := range strings.Split(htmlSpec[1], ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				renames[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

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

	if renames["descriptionHtml"] != "description" {
		t.Errorf("主表单应声明 data-write-json-wrap=descriptionHtml:description，实际 %v", renames)
	}

	if lines := regexp.MustCompile(`data-write-lines="([^"]*)"`).FindStringSubmatch(form); len(lines) != 2 ||
		!strings.Contains(lines[1], "images") || !strings.Contains(lines[1], "imageAlts") {
		t.Errorf("主表单应声明 data-write-lines=images,imageAlts，实际 %v", lines)
	}
}
