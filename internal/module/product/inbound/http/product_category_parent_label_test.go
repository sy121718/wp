package producthttp

// product_category_parent_label_test.go — 编辑抽屉里「父级不在候选列表」那一项显示什么（FIX-04）。
//
// 模板 product_category_form.html 为这种父级单独渲染一行：
//
//	<option value="{{parent}}" selected>{{if isset(.ParentLabel)}}{{.ParentLabel}}{{else}}{{parent}}{{end}}</option>
//
// 而 Go 侧曾经**从不写 ParentLabel**（全仓零出现）→ isset 恒假 → 永远走 else →
// 下拉里显示 pr1cat9 这种裸 ID，用户无从判断自己在改谁的子级。
//
// 这里钉三件事：① 缺失父级的挑选是纯逻辑（missingParentIDs）；② categoryTreeRows →
// categoryDrawerData 的接线真的把名称放进了 EditForm；③ 真实渲染出来的那一项是**分类名**
//（跨工程父级还要带上「不属于本工程」），且名称取不到时退回裸 ID 是有意降级。
//
// 可达性前提（2026-10 实测，别因为「本地构造不出场景」就删掉本文件）：
// 模板那条 `isset(.ParentLabel)` 分支在「孤儿分类丢行」未修前是**不可达**的 —— 悬空
// parent_id 的分类根本不进列表树，跨工程父级又被 ListCategories 一并带进候选、使
// ParentMissing 恒为 false。现在它的真实入口是**跨工程父级**（feature 用例
// TestCategoryOrphanParentVisibleInList），本文件负责钉它的两端：数据形状（handler 写入）
// 与渲染（模板那一项显示什么）。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"
)

const (
	parentLabelProjectID = "prj-parent-label"
	parentLabelParentID  = "pr1cat9"
)

// categoryParentOption 抓「父级缺失」那一项的 option 文本（模板里它是唯一带 selected 的该项）。
func categoryParentOption(parentID string) *regexp.Regexp {
	return regexp.MustCompile(`<option value="` + regexp.QuoteMeta(parentID) + `" selected>([^<]*)</option>`)
}

func parentLabelOptions() []productdto.PickOptionResp {
	return []productdto.PickOptionResp{{Value: "pr1cat1", Label: "女装"}, {Value: "pr1cat2", Label: "连衣裙"}}
}

func parentLabelRow() *productdto.CategoryResp {
	return &productdto.CategoryResp{
		ID: "pr1cat3", ProjectID: parentLabelProjectID, Name: "半身裙", Slug: "skirts", ParentID: parentLabelParentID,
	}
}

// renderCategoryDrawer 用**真实 Jet 模板**渲染一次编辑抽屉（parentLabels 由调用点给）。
func renderCategoryDrawer(t *testing.T, parentLabels map[string]productdto.CategoryParentInfoResp) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/category-drawer", func(c *gin.Context) {
		// 抽屉形状由读模型装（service.CategoryDrawerForm）；取词函数由渲染层注入。
		data := productservice.CategoryDrawerForm("update", parentLabelProjectID, parentLabelRow(), parentLabelOptions(), parentLabels)
		c.HTML(http.StatusOK, "admin/product/product_category_form.html", text.Inject(c, data))
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/category-drawer", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "data-category-form-host") {
		t.Fatalf("抽屉渲染失败：status=%d body=%s", rec.Code, body)
	}
	return body
}

// TestCategoryDrawerParentLabelRendered 父级不在候选列表时，那一项显示分类名。
func TestCategoryDrawerParentLabelRendered(t *testing.T) {
	body := renderCategoryDrawer(t, map[string]productdto.CategoryParentInfoResp{parentLabelParentID: {Name: "男装"}})
	match := categoryParentOption(parentLabelParentID).FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("抽屉里找不到「父级缺失」的那一项（value=%s selected）", parentLabelParentID)
	}
	if got := strings.TrimSpace(match[1]); got != "男装" {
		t.Fatalf("父级缺失项应显示分类名 %q，实际 %q（裸 ID 说明 ParentLabel 又没写进 data）", "男装", got)
	}
}

// TestCategoryDrawerParentLabelNoNameFallsBackToID 名称取不到时退回原始 ID —— 这是**有意降级**：
// 父级行真的不存在（或不在本工程作用域内）时，显示一个编造的父级名比显示 ID 更危险。
func TestCategoryDrawerParentLabelNoNameFallsBackToID(t *testing.T) {
	for name, labels := range map[string]map[string]productdto.CategoryParentInfoResp{"nil": nil, "空名": {parentLabelParentID: {}}} {
		t.Run(name, func(t *testing.T) {
			body := renderCategoryDrawer(t, labels)
			match := categoryParentOption(parentLabelParentID).FindStringSubmatch(body)
			if match == nil {
				t.Fatalf("抽屉里找不到「父级缺失」的那一项（value=%s selected）", parentLabelParentID)
			}
			if got := strings.TrimSpace(match[1]); got != parentLabelParentID {
				t.Fatalf("没有名称时应退回原始 ID %q，实际 %q", parentLabelParentID, got)
			}
		})
	}
}

// TestCategoryTreeRowsFlagsOutOfScopeParent 列表行徽章（product_categories.html）的判据：
// 父级不在本工程的候选列表里。
//
// parentLabels 传 nil 是刻意的 —— 判据必须取 missingParentIDs（父级不在候选列表），
// 不能用 parentLabels：后者只收「名称读得到」的父级，RLS 切非超级角色后跨工程父级读不到
// 名称会被它漏掉，而那时恰恰最该让操作者看见异常。改成用 parentLabels 判，第一条断言直接红。
func TestCategoryTreeRowsFlagsOutOfScopeParent(t *testing.T) {
	flagged := productservice.CategoryTreeRows(parentLabelProjectID,
		[]*productdto.CategoryResp{parentLabelRow()}, parentLabelOptions(), false, nil)
	if len(flagged) != 1 {
		t.Fatalf("应有一行，实际 %d", len(flagged))
	}
	if !flagged[0].ParentOutOfScope {
		t.Fatalf("父级 %s 不在候选列表里，该行应标记 ParentOutOfScope", parentLabelParentID)
	}

	// 父级确实在候选列表里 → 不标。
	inScope := parentLabelRow()
	inScope.ParentID = "pr1cat1"
	clean := productservice.CategoryTreeRows(parentLabelProjectID,
		[]*productdto.CategoryResp{inScope}, parentLabelOptions(), false, nil)
	if clean[0].ParentOutOfScope {
		t.Fatalf("父级在本工程候选列表里，不该标记 ParentOutOfScope")
	}

	// 顶级分类（无父级）→ 不标。
	root := parentLabelRow()
	root.ParentID = ""
	top := productservice.CategoryTreeRows(parentLabelProjectID,
		[]*productdto.CategoryResp{root}, parentLabelOptions(), false, nil)
	if top[0].ParentOutOfScope {
		t.Fatalf("顶级分类没有父级，不该标记 ParentOutOfScope")
	}
}

// TestCategoryRowOutOfScopeBadgeRendered 徽章要真的渲出来 —— 数据层标了脏而模板没渲染
// （或渲染在别的行上）都是静默无效：页面照常 200、日志干净，操作者依然看不出这一行有问题。
// 断言落在「那一行的 HTML 里」而不是整页含不含某个类名，避免被页面别处的同类样式骗过。
func TestCategoryRowOutOfScopeBadgeRendered(t *testing.T) {
	render := func(t *testing.T, outOfScope bool) string {
		t.Helper()
		return renderAdminTemplate(t, "admin/product/product_categories.html", productPageLayoutData(gin.H{
			"title": "商品分类", "menu": "product-categories",
			"Projects": []gin.H{}, "SelectedProject": parentLabelProjectID, "Options": []gin.H{},
			"Categories": []gin.H{{
				"ID": "c1", "Name": "夹克", "Slug": "jackets", "Label": "夹克", "Sort": 0,
				"SEOTitle": "", "SEODescription": "", "Description": "", "Image": "",
				"ParentID": parentLabelParentID, "Depth": 0, "SearchMode": false,
				"ParentOutOfScope": outOfScope,
			}},
			"Buttons": map[string]any{},
			"Err":     "",
		}))
	}
	rowRE := regexp.MustCompile(`(?s)<tr data-category-row data-category-id="c1".*?</tr>`)

	flagged := rowRE.FindString(render(t, true))
	if flagged == "" {
		t.Fatal("渲染结果里找不到那一行（data-category-id=\"c1\"）")
	}
	if !strings.Contains(flagged, "badge badge-warning") {
		t.Fatalf("父级不在本工程时该行应渲出徽章，实际那一行：%s", flagged)
	}
	if !strings.Contains(flagged, "夹克") {
		t.Fatalf("徽章不该把分类名顶掉，实际那一行：%s", flagged)
	}

	clean := rowRE.FindString(render(t, false))
	if strings.Contains(clean, "badge badge-warning") {
		t.Fatalf("父级正常时不该出现徽章，实际那一行：%s", clean)
	}
}

// TestCategoryTreeRowsThreadsParentLabels categoryTreeRows → categoryDrawerData 的接线：
// 名称必须真的进到每一行的 EditForm（模板读的就是 EditForm 里的 ParentLabel）。
func TestCategoryTreeRowsThreadsParentLabels(t *testing.T) {
	rows := productservice.CategoryTreeRows(parentLabelProjectID,
		[]*productdto.CategoryResp{parentLabelRow()}, parentLabelOptions(), false,
		map[string]productdto.CategoryParentInfoResp{parentLabelParentID: {Name: "男装"}})
	if len(rows) != 1 {
		t.Fatalf("应有一行，实际 %d", len(rows))
	}
	editForm := rows[0].EditForm
	if editForm == nil {
		t.Fatal("EditForm 为空：抽屉会渲染不出父级回显")
	}
	if editForm["ParentMissing"] != true {
		t.Errorf("父级不在候选列表时应标记 ParentMissing=true，实际 %v", editForm["ParentMissing"])
	}
	if editForm["ParentLabel"] != "男装" {
		t.Errorf("EditForm 应带上父级名称，实际 %v", editForm["ParentLabel"])
	}
}

// TestMissingParentIDs 纯逻辑：只挑「父级不在候选里」的 id，去重、保持顺序、跳过空父级。
func TestMissingParentIDs(t *testing.T) {
	nodes := []*productdto.CategoryResp{
		{ID: "a", ParentID: "p1"},
		{ID: "b", ParentID: "p1"}, // 与上一行同一个父级 → 去重
		{ID: "c", ParentID: "p2"},
		{ID: "d"},                      // 顶级分类：没有父级
		{ID: "e", ParentID: "pr1cat1"}, // 父级在候选列表里 → 不算缺失
	}
	got := productservice.MissingParentIDs(nodes, productservice.CategoryOptionIDs(parentLabelOptions()))
	if len(got) != 2 || got[0] != "p1" || got[1] != "p2" {
		t.Fatalf("缺失父级应去重且保序为 [p1 p2]，实际 %v", got)
	}
	if ids := productservice.MissingParentIDs(nodes, nil); len(ids) != 3 || ids[0] != "p1" || ids[1] != "p2" || ids[2] != "pr1cat1" {
		t.Fatalf("候选为空时所有非空父级都算缺失（去重后 3 个）：%v", ids)
	}
}

// TestFilterCategoriesInProject 候选只收本工程分类（判据是行自己的 ProjectID，不依赖 RLS）。
func TestFilterCategoriesInProject(t *testing.T) {
	flat := []*productdto.CategoryResp{
		{ID: "a", ProjectID: "p1", Name: "男装"},
		{ID: "b", ProjectID: "p2", Name: "别的工程的分类"},
		{ID: "c", ProjectID: "p1", Name: "女装"},
	}
	got := productservice.CategoriesInProject(flat, "p1")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("应只保留本工程分类 [a c]，实际 %+v", got)
	}
	if got := productservice.CategoriesInProject(flat, ""); len(got) != 0 {
		t.Fatalf("工程为空时不该给出任何候选，实际 %d 条", len(got))
	}
}

// TestCategoryParentOutOfScopeNoteRenders 跨工程父级的「不属于本工程」必须真的渲染出来 ——
// 名称是数据（ParentLabel），那句话是文案（ParentOutOfScope 为真时模板取词，见
// product_category_form.html 的父级缺失项）。原 categoryParentText 已随死代码删除，
// 这条断言因此从「Go 拼串」挪到「渲染结果」（判据同一件事，落点换到取词该在的那一层）。
func TestCategoryParentOutOfScopeNoteRenders(t *testing.T) {
	body := renderCategoryDrawer(t, map[string]productdto.CategoryParentInfoResp{
		parentLabelParentID: {Name: "男装", OutOfScope: true},
	})
	match := categoryParentOption(parentLabelParentID).FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("抽屉里找不到「父级缺失」的那一项（value=%s selected）", parentLabelParentID)
	}
	if got := strings.TrimSpace(match[1]); !strings.Contains(got, "男装") || !strings.Contains(got, "不属于本工程") {
		t.Fatalf("跨工程父级应渲染成「男装（不属于本工程）」，实际 %q", got)
	}
}

// TestCategoryRowStructRenders 行类型化（productdto.CategoryRowResp）之后模板仍然要渲染正确。
//
// 上面几条用例传的是 gin.H（map）行 —— 那是「模板单测直接造数据」的形态，不是生产形态；
// 生产走的是 struct。两条路径对 Jet 的差异就在 isset / 缺键语义上：map 缺键 → isset 假，
// struct 一定有字段 → 值恒为 true。这条用例用 struct 行渲染，钉住生产形态。
func TestCategoryRowStructRenders(t *testing.T) {
	html := renderAdminTemplate(t, "admin/product/product_categories.html", productPageLayoutData(gin.H{
		"title": "商品分类", "menu": "product-categories",
		"Projects": []gin.H{}, "SelectedProject": parentLabelProjectID, "Options": []gin.H{},
		"Categories": []productdto.CategoryRowResp{{
			ID: "c1", Name: "夹克", Slug: "jackets", Label: "夹克",
			ParentID: parentLabelParentID, Depth: 1, ParentOutOfScope: true,
			EditForm: gin.H{"Mode": "update", "ID": "c1", "Name": "夹克"},
		}},
		"Buttons": map[string]any{},
		"Err":     "",
	}))
	rowRE := regexp.MustCompile(`(?s)<tr data-category-row data-category-id="c1".*?</tr>`)
	row := rowRE.FindString(html)
	if row == "" {
		t.Fatal("struct 行没有渲出来：类型化之后模板读不到字段")
	}
	if !strings.Contains(row, `aria-level="2"`) {
		t.Errorf("Depth=1 应渲成 aria-level=2，实际那一行：%s", row)
	}
	if !strings.Contains(row, "badge badge-warning") {
		t.Errorf("ParentOutOfScope=true 应渲出徽章，实际那一行：%s", row)
	}
}
