package producthttp

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

const createFormPath = "../../../../templates/admin/product/product_create_form.html"

func readCreateForm(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(createFormPath)
	if err != nil {
		t.Fatalf("读建表单模板失败: %v", err)
	}
	page := string(raw)

	idAt := strings.Index(page, `id="product-create-form"`)
	if idAt < 0 {
		t.Fatal("建表单模板里找不到 id=product-create-form")
	}
	start := strings.LastIndex(page[:idAt], "<form")
	if start < 0 {
		t.Fatal("建表单模板里找不到 <form 起始标签")
	}
	end := strings.Index(page[start:], "</form>")
	if end < 0 {
		t.Fatal("建表单模板里找不到表单的闭合标签")
	}
	return page[start : start+end]
}

func formFieldNames(form string) map[string]bool {
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(form, -1) {
		names[m[1]] = true
	}
	delete(names, "csrf_token")
	return names
}

func stripJetComments(src string) string {
	re := regexp.MustCompile("(?s)\\{\\*.*?\\*\\}")
	return re.ReplaceAllString(src, "")
}

func TestProductCreateFormFieldsMatchCreateReq(t *testing.T) {
	form := readCreateForm(t)

	if !strings.Contains(form, `hx-post="/api/product/create"`) {
		t.Fatal("建表单必须声明真实 HTMX 写端点")
	}
	if !strings.Contains(form, `data-write-nav="[data-product-created-link]"`) {
		t.Error("导航必须引用 Jet 成功片段中的链接，不是 URL 插值串")
	}

	allowed := map[string]bool{}
	typ := reflect.TypeOf(productdto.CreateReq{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("form")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name != "" && name != "-" {
			allowed[name] = true
		}
	}

	names := formFieldNames(form)
	for name := range names {
		if !allowed[name] {
			t.Errorf("表单字段 name=%q 在 CreateReq 里没有对应的原生 form tag", name)
		}
	}

	for _, want := range []string{
		"projectId", "name", "slug", "sku", "externalSku",
		"defaultPrice", "attributeIds", "warehouseIds", "trackQuantity", "quantity",
	} {
		if !names[want] {
			t.Errorf("建表单缺少协议字段 name=%q", want)
		}
	}

	for _, gone := range []string{"skuSource", "warehouseSku"} {
		if names[gone] {
			t.Errorf("建表单不该再有 name=%q 的控件（从仓库选那条来路已从页面撤掉）", gone)
		}
	}
}

func TestProductCreateFormDeclaresBaseAttributes(t *testing.T) {
	form := readCreateForm(t)

	for _, want := range []string{`hx-post="/api/product/create"`, `hx-target="find [data-write-result]"`, `hx-swap="innerHTML"`, `hx-sync="this:drop"`, `hx-disable="findAll button:not(:disabled)"`, `data-write-status`, `data-write-result`, `data-default-price-switch`, `data-default-price-input`} {
		if !strings.Contains(form, want) {
			t.Errorf("建表单缺少 %q", want)
		}
	}

	for _, gone := range []string{"data-product-create-host", "data-write-url", "product={id}", "data-sku-preview", "data-warehouse-sku"} {
		if strings.Contains(form, gone) {
			t.Errorf("建表单仍含已撤掉的提交机制 %q", gone)
		}
	}

	if !strings.Contains(form, "data-requires-js") {
		t.Errorf("提交按钮缺少 data-requires-js（无 JS 时表单一提交就落到不存在的端点）")
	}
}
