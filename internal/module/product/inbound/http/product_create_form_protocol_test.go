package producthttp

// product_create_form_protocol_test.go — 建表单与 /api/product/create 的协议对账。
//
// 页面提交已迁到 ui/json_write.js：基座把表单里**每个具名字段**原样发成 dto.CreateReq 的 JSON。
// 端点是严格解码（未知成员整条 400），所以两侧任一处写错都是「点了没反应」：
//   - 表单里多一个 dto 不认识的字段 → 整条请求被拒；
//   - 表单里少一个字段（或 name 拼错）→ 该字段静默丢失，商品照样建出来、只是那一格是空的。
// 模板侧用例只能断言声明存在，对不上字段名它们照样全绿 —— 这条把两侧真的对一遍。
//
// 页面**不再提交** skuSource / warehouseSku：那条「从仓库选」的来路在 UI 上从来没有可操作的入口
// （控件一直是 hidden），撤掉之后服务端把空值归一成「自己创建」。两个字段仍在 dto.CreateReq 上，
// 接口面 / 脚本路径继续可用，所以这里是**负向**断言：它们不该再出现在页面的提交面里。

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

const createFormPath = "../../../../templates/admin/product/product_create_form.html"

// readCreateForm 取出建表单片段（从 form 开标签到它的闭合标签）。
func readCreateForm(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(createFormPath)
	if err != nil {
		t.Fatalf("读建表单模板失败: %v", err)
	}
	page := string(raw)
	start := strings.Index(page, `id="product-create-form"`)
	if start < 0 {
		t.Fatal("建表单模板里找不到 id=product-create-form")
	}
	end := strings.Index(page[start:], "</form>")
	if end < 0 {
		t.Fatal("建表单模板里找不到表单的闭合标签")
	}
	return page[start : start+end]
}

// formFieldNames 收集表单里所有具名控件（csrf_token 不进 body：基座放请求头）。
func formFieldNames(form string) map[string]bool {
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(form, -1) {
		names[m[1]] = true
	}
	delete(names, "csrf_token")
	return names
}

// stripJetComments 去掉 Jet 注释（{* … *}），返回剥离后的文本。
//
// 行模板的字段断言要先剥注释再扫：注释里举的字段名（例如「原先是 p.SEOTitle」）不该带偏判据。
// 本包原先另有一份同名 helper，随建表单的 htmx 回填测试一起删掉了，这里收成包内唯一一份。
func stripJetComments(src string) string {
	re := regexp.MustCompile("(?s)\\{\\*.*?\\*\\}")
	return re.ReplaceAllString(src, "")
}

// TestProductCreateFormFieldsMatchCreateReq 表单字段名 <-> dto.CreateReq 的 json 标签。
func TestProductCreateFormFieldsMatchCreateReq(t *testing.T) {
	form := readCreateForm(t)

	if !strings.Contains(form, `hx-post="/api/product/create"`) {
		t.Fatalf(`建表单缺少 hx-post="/api/product/create"（按钮会退化成原生提交）`)
	}
	if !strings.Contains(form, `hx-swap="none"`) {
		t.Fatalf("建表单缺少 hx-swap=none（写动作没有要换进 DOM 的内容）")
	}
	// 成功后的落点：商品编辑页（新建页只填得下几个字段，全量录入在编辑页）。
	if !strings.Contains(form, `data-write-nav="/admin/products/edit?project=`) {
		t.Errorf("建表单缺少 data-write-nav（成功后应去商品编辑页）")
	}

	// dto 侧：json 标签集合（"-" 与无标签字段不是出口）。
	allowed := map[string]bool{}
	typ := reflect.TypeOf(productdto.CreateReq{})
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

	names := formFieldNames(form)
	for name := range names {
		if !allowed[name] {
			t.Errorf("表单字段 name=%q 在 CreateReq 里没有对应的 json 键（严格解码会整条 400）", name)
		}
	}
	// 反向：这几个是协议主干，少一个就等于那个字段不再能在新建时填。
	for _, want := range []string{
		"projectId", "name", "type", "slug", "sku", "externalSku",
		"defaultPrice", "attributeIds", "warehouseIds", "trackQuantity", "quantity",
	} {
		if !names[want] {
			t.Errorf("建表单缺少协议字段 name=%q", want)
		}
	}
	// 页面上已撤掉的两条来路：仍在 dto 上（接口面用），但不能再从表单发出去 ——
	// 发出去意味着页面上又出现了「用户能选、但选完必然失败」的入口。
	for _, gone := range []string{"skuSource", "warehouseSku"} {
		if names[gone] {
			t.Errorf("建表单不该再有 name=%q 的控件（从仓库选那条来路已从页面撤掉）", gone)
		}
	}
}

// TestProductCreateFormDeclaresBaseAttributes 基座声明的四档形状。
//
// 缺一条的症状都不是报错，而是「某个字段按错误的形状发出去」：
//   - 多选 checkbox 组不收成数组 → 勾了三个只存最后一个；
//   - 数字字段按字符串发 → 端点的 *int / *float64 解码 400；
//   - 留空字段照发 → 空串进 *float64 是 400，进 slug / sku 则被当成「显式清空」。
func TestProductCreateFormDeclaresBaseAttributes(t *testing.T) {
	form := readCreateForm(t)

	// 提交已迁到 htmx（formdata）。原来那四档声明（data-write-array / -number / -bool /
	// -float）与 data-write-skip-empty 都不再需要，它们各自对应的事现在由
	// htmx_form_body.js 或 formdata 本身承担：
	//   · 多选 checkbox：未勾选时浏览器根本不提交该键 →「一个都不勾 = 不发」天然成立；
	//   · 数字/布尔/浮点：formdata 里是字符串，后端 *int / *bool / *float64 能直接绑（实测）；
	//   · 留空即不设置：htmx_form_body.js 全局删空值键。
	//
	// 判据仍是「提交必须声明端点」—— 少了它按钮会退化成原生提交（页面 POST 的 action
	// 早已删除 ⇒ 点了没反应）。data-write-nav 是唯一还需声明的（按响应 id 跳编辑页）。
	if !strings.Contains(form, `hx-post="/api/product/create"`) {
		t.Errorf("建表单缺少 hx-post=\"/api/product/create\"")
	}
	if !strings.Contains(form, `data-write-nav="/admin/products/edit?`) {
		t.Errorf("建表单缺少 data-write-nav（成功后要跳编辑页）")
	}
	// 分组容器与页面端点都该彻底消失。
	for _, gone := range []string{"data-product-create-host", "action=\"/admin/products/create\"", "data-write-api="} {
		if strings.Contains(form, gone) {
			t.Errorf("建表单仍含已撤掉的提交机制 %q", gone)
		}
	}
	// 无 JS 时表单没有可用端点：按钮必须默认 disabled（由基座接管后启用）。
	if !strings.Contains(form, "data-requires-js") {
		t.Errorf("提交按钮缺少 data-requires-js（无 JS 时表单一提交就落到不存在的端点）")
	}
}
