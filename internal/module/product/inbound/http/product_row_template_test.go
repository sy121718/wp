package producthttp

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// product_row_template_test.go — 行形状（dto.ProductRowResp）与三个商品模板的字段对账。
//
// 行装配下沉进读模型（service/product_row.go）之后，控制器不再拼 []gin.H —— 行是结构体，
// 而模板读 `p.X` 时 struct **缺字段是渲染期报错**（`there is no field or method 'X'`），
// 与 map「缺键只是 nil」不同：Jet 会把整个响应判失败，htmx 又对 5xx 不 swap ——
// 用户看到的是「点了没反应」。所以模板与结构体必须双向对齐，两个方向都判失败：
//
//	· 模板读了字段、结构体没有 → 渲染期 500（本用例红）；
//	· 结构体有字段、没有任何模板读 → 也红（契约里多出来的字段就是又一份「谁都不知道该不该删」的东西）。
//
// 豁免表**当前为空**：三个模板读的字段与 ProductRowResp 的导出字段正好一一对应（含 products.html
// 那个 `{{range _, p := .Projects}}` 循环 —— 它只读 p.ID / p.Name，两者都在 ProductRowResp 上）。
// 真要加豁免，必须写明「谁在读它」；空着的豁免表能一直空着才是好契约。
//
// 先 stripJetComments 再扫：注释里的字段名（例如「原先是 p.SEOTitle」）不该带偏判据。
func TestProductRowMatchesTemplates(t *testing.T) {
	fields := map[string]bool{}
	rt := reflect.TypeOf(productdto.ProductRowResp{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			t.Fatalf("ProductRowResp.%s 未导出：模板读不到它", f.Name)
		}
		fields[f.Name] = false
	}
	if len(fields) == 0 {
		t.Fatal("ProductRowResp 没有任何导出字段 —— 判据失效（结构体读错了？）")
	}

	scanned := 0
	for _, name := range rowTemplates {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "templates", "admin", "product", name))
		if err != nil {
			t.Fatalf("读模板 %s：%v", name, err)
		}
		src := stripJetComments(string(body))
		if len(src) == 0 {
			t.Fatalf("模板 %s 被扫成了空串 —— 判据失效", name)
		}
		scanned++
		for _, re := range []*regexp.Regexp{rowFieldRef, productRowFieldRef} {
			for _, m := range re.FindAllStringSubmatch(src, -1) {
				used, ok := fields[m[1]]
				if !ok {
					t.Errorf("%s 读了 %s，但 productdto.ProductRowResp 没有这个导出字段 —— "+
						"struct 缺字段是渲染期 500（htmx 对 5xx 不 swap，表现是「点了没反应」）", name, m[0])
					continue
				}
				if !used {
					fields[m[1]] = true
				}
			}
		}
	}
	if scanned != len(rowTemplates) {
		t.Fatalf("只扫到 %d 个模板，应为 %d 个", scanned, len(rowTemplates))
	}

	var unused []string
	for name, used := range fields {
		if !used {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("productdto.ProductRowResp 这些字段没有任何模板读：%v —— 删掉，或把消费方与理由写进本用例", unused)
	}
}

// rowTemplates 读模型产出的行（ProductRowResp）直接喂给这三个模板：列表页、详情页、编辑页。
var rowTemplates = []string{"products.html", "product_detail.html", "product_edit.html"}

var (
	// 行变量固定叫 p（`{{range _, p := .Products}}` / `{{p := .Product}}`）。
	rowFieldRef = regexp.MustCompile("\\bp\\.([A-Z][A-Za-z0-9_]*)")
	// 详情页 / 编辑页另有 `{{.Product.X}}` 形态（标题、状态徽章）。
	productRowFieldRef = regexp.MustCompile("\\.Product\\.([A-Z][A-Za-z0-9_]*)")
)

// TestPageViewsCarryTypedRows 三个页面视图携带的行必须是 dto.ProductRowResp（不是 map）。
//
// 为什么单列一条：上面那份对账判据只在「行是结构体」时才有意义 —— 一旦有人把
// ProductListPageResp.Products 改回 []map[string]any（旧形态），字段名就又变成运行期字符串，
// 模板少读一个字段、控制器少写一个键都不会有人报错（这正是下沉要治的病）。
//
// 顺带钉住 Flat 的消失：分类扁平序列**只为行装配存在**（行里的分类下拉按层级缩进读它），
// 行下沉之后它没有任何消费方 —— 视图字段留着就会再长出第二个读取点。
func TestPageViewsCarryTypedRows(t *testing.T) {
	rowType := reflect.TypeOf(productdto.ProductRowResp{})
	rowSlice := reflect.SliceOf(rowType)
	rowPtr := reflect.PtrTo(rowType)
	listT := reflect.TypeOf(productdto.ProductListPageResp{})
	detailT := reflect.TypeOf(productdto.ProductDetailPageResp{})
	editT := reflect.TypeOf(productdto.ProductEditPageResp{})

	fieldType := func(owner reflect.Type, name string) reflect.Type {
		f, ok := owner.FieldByName(name)
		if !ok {
			t.Fatalf("%s 没有 %s 字段", owner, name)
		}
		return f.Type
	}
	if got := fieldType(listT, "Products"); got != rowSlice {
		t.Errorf("ProductListPageResp.Products 应是 []dto.ProductRowResp，实际 %s —— 行装配在读模型（service/product_row.go），控制器不再拼 []map[string]any", got)
	}
	if got := fieldType(detailT, "Product"); got != rowPtr {
		t.Errorf("ProductDetailPageResp.Product 应是 *dto.ProductRowResp，实际 %s", got)
	}
	if got := fieldType(editT, "Product"); got != rowPtr {
		t.Errorf("ProductEditPageResp.Product 应是 *dto.ProductRowResp，实际 %s", got)
	}
	for _, owner := range []reflect.Type{listT, detailT, editT} {
		if _, ok := owner.FieldByName("Flat"); ok {
			t.Errorf("%s 还留着 Flat —— 分类扁平序列只为行装配存在，已随行下沉进读模型", owner)
		}
	}
}
