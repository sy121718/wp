package producthttp

// product_edit_form_protocol_test.go — 编辑页保存表单与 /api/product/update 的协议对账。
//
// 页面保存已迁到 ui/write-base.js：基座把表单里**每个具名字段**原样发成 UpdateReq 的 JSON。
// 端点是严格解码（未知成员整条 400），所以「表单里多一个 dto 不认识的字段」= 保存直接失败，
// 而症状只是「点了没反应」。模板侧的用例只能断言声明存在，对不上字段名它们照样全绿 ——
// 这条用例把两侧真的对一遍：模板里出现的每个 name 必须能在 dto.UpdateReq 的 json 标签里找到。
//
// 唯一的改名点是富文本：控件名 descriptionHtml，端点键 description（data-write-html 负责包成
// {"html": …}）—— 这里按同一条映射还原，映射本身写在模板的 data-write-html 里。

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
	// 从 `<form` 起截，不是从 id= 起：data-write / data-write-url / data-write-json-wrap
	// 都写在 id 之前，从 id 起会把它们切掉（迁到新契约后属性顺序变了，实测踩到）。
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

	// 声明侧：端点 + 富文本改名映射（data-write-json-wrap="源:目标"）。
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

	// dto 侧：json 标签集合（"-" 与无标签字段不是出口）。
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

	// csrf_token 不进 body（基座放请求头），这里出现只会变成未知字段。
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
	// 反向：表单承载的是「商品自身字段」，这几个是协议主干，少一个就等于那个字段不再可编辑。
	for _, want := range []string{"name", "slug", "sku", "status", "defaultPrice", "weight", "attributeIds", "categoryIds", "tagIds", "primaryCategoryId", "brandId"} {
		if !names[want] {
			t.Errorf("主表单缺少协议字段 name=%q", want)
		}
	}
	// 富文本控件由 richEditor 片段渲染（name 不在本文件里），所以按改名映射反查：
	// 声明一旦丢了，描述就会以 descriptionHtml 这个未知键发出去。
	if renames["descriptionHtml"] != "description" {
		t.Errorf("主表单应声明 data-write-json-wrap=descriptionHtml:description，实际 %v", renames)
	}
	// 图集两个字段同样出自 mediaGallery 片段，靠 data-write-lines 把它们按行切（否则发字符串，
	// 端点的 []string 解码直接失败）。
	if lines := regexp.MustCompile(`data-write-lines="([^"]*)"`).FindStringSubmatch(form); len(lines) != 2 ||
		!strings.Contains(lines[1], "images") || !strings.Contains(lines[1], "imageAlts") {
		t.Errorf("主表单应声明 data-write-lines=images,imageAlts，实际 %v", lines)
	}
}
