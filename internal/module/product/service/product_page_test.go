package productservice

import (
	"context"
	"errors"
	"net/http"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
)

// 商品新建页读模型（整页 GET 取数下沉）的失败短路与口径：
//
// 这一屏的**档位与「失败之后还要不要继续查」**是业务结论，下沉后只在这个方法里，
// 所以这两条必须在这里钉住 —— 控制器层已经看不到取数编排了（它只会把视图渲染出去）。
//
// 「合法工程必须真的取到属性组」那一半要靠真实库（ListAttributes 走 model），
// 由 public/test/product/feature/product_new_page_parity_test.go 覆盖：它断言整页里
// 出现 `name="attributeIds"` 与属性组的标签，属型断言在那边才有意义。

// productPageProjectStub 工程契约桩（只有读清单一条会被本用例走到）。
type productPageProjectStub struct {
	projectcontract.ProjectService
	rows []projectdto.ProjectResp
	err  error
}

func (s productPageProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.rows, s.err
}

// productPageWarehousePortStub 仓库选项端口桩：记录调用次数。
//
// 记「有没有被调用」而不是「返回了什么」：本组用例要断的正是**失败时不派发**
// （在已被判非法的工程上继续查，会把「工程不存在」渲染成「这个工程下没有可选项」）。
type productPageWarehousePortStub struct {
	calls int
	// err 非 nil 时仓库清单读失败：用来钉「失败之后一条派生取数都不发」。
	err error
}

func (s *productPageWarehousePortStub) WarehouseOptions(context.Context, string) ([]productdto.WarehouseOptionResp, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return []productdto.WarehouseOptionResp{}, nil
}

func (s *productPageWarehousePortStub) WarehouseSKUOptions(context.Context, string, []productdto.WarehouseOptionResp) ([]productdto.WarehouseSKUGroupResp, error) {
	s.calls++
	return []productdto.WarehouseSKUGroupResp{}, nil
}

// productPageTemplatePortStub 模板端口桩：同上。
type productPageTemplatePortStub struct {
	calls int
}

func (s *productPageTemplatePortStub) EntityTemplates(context.Context, string, string) ([]productdto.EntityTemplateOptionResp, error) {
	s.calls++
	return []productdto.EntityTemplateOptionResp{}, nil
}

func (s *productPageTemplatePortStub) DefaultEntityTemplate(context.Context, string, string) (string, error) {
	s.calls++
	return "", nil
}

// newProductPageReadModel 装一个只有工程契约与两条端口桩的读模型。
//
// model 传 nil：本组用例全部落在「主数据或参数已经判定失败」的短路分支上，
// 属性组取数（需要 model）一条都不会被走到 —— 走到就说明短路失效了，
// 那时这里会以 nil 解引用当场崩掉，比一个静默通过的断言更早暴露。
func newProductPageReadModel(projects productPageProjectStub) (*Service, *productPageWarehousePortStub, *productPageTemplatePortStub) {
	wh := &productPageWarehousePortStub{}
	tpl := &productPageTemplatePortStub{}
	s := NewService(nil, projects)
	s.SetWarehouseOptionsPort(wh)
	s.SetEntityTemplatePort(tpl)
	return s, wh, tpl
}

// TestProductNewPageInvalidProjectStopsBeforeFanout 非法工程 = 400 + 一条派生取数都不发。
func TestProductNewPageInvalidProjectStopsBeforeFanout(t *testing.T) {
	s, wh, tpl := newProductPageReadModel(productPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
	})
	view, err := s.ProductNewPage(context.Background(), &productdto.ProductNewPageReq{ProjectID: "does-not-exist"})
	if err == nil || err.Error() != productenums.ErrInvalidParam {
		t.Fatalf("非法工程应返回 %s，实际 err=%v", productenums.ErrInvalidParam, err)
	}
	wantPageFailure(t, "非法工程", view, err, http.StatusBadRequest)
	if wh.calls != 0 || tpl.calls != 0 {
		t.Fatalf("非法工程不该发任何派生取数，实际仓库端口 %d 次、模板端口 %d 次", wh.calls, tpl.calls)
	}
}

// TestProductNewPageProjectReadFailureIsFiveHundred 工程清单读不出来 = 500（不是空态、也不是 400）。
func TestProductNewPageProjectReadFailureIsFiveHundred(t *testing.T) {
	s, wh, tpl := newProductPageReadModel(productPageProjectStub{
		err: errors.New(`pq: relation "projects" does not exist`),
	})
	view, err := s.ProductNewPage(context.Background(), &productdto.ProductNewPageReq{ProjectID: "p1"})
	if err == nil {
		t.Fatal("工程清单读不出来必须返回错误")
	}
	wantPageFailure(t, "工程清单读不到", view, err, http.StatusInternalServerError)
	if wh.calls != 0 || tpl.calls != 0 {
		t.Fatalf("主数据失败后不该继续查，实际仓库端口 %d 次、模板端口 %d 次", wh.calls, tpl.calls)
	}
}

// TestProjectExistsMatchesListedProjects 工程存在性判据（控制器与读模型共用的那一份）。
func TestProjectExistsMatchesListedProjects(t *testing.T) {
	rows := []projectdto.ProjectResp{{ID: "p1"}, {ID: "p2"}}
	if !ProjectExists(rows, "p2") {
		t.Fatal("清单里的工程应判为存在")
	}
	if ProjectExists(rows, "p3") || ProjectExists(rows, "") || ProjectExists(nil, "p1") {
		t.Fatal("不在清单里 / 空 id / 空清单都应判为不存在")
	}
}

// TestAttributeOptionViewsKeepsKeyInLabel 属性组标签口径：名字 + 标识 key。
//
// 只给名字会在同名属性组之间选错（商品详情页与属性页都按 key 指代属性组），
// 而这条规则改一处就漏一处 —— 所以它必须有一条会变红的判据。
func TestAttributeOptionViewsKeepsKeyInLabel(t *testing.T) {
	rows := []*productdto.AttributeResp{
		{ID: "a1", Name: "颜色", Key: "color", IsVariation: true},
		{ID: "a2", Name: "无 key"},
		nil,
	}
	got := AttributeOptionViews(rows)
	if len(got) != 2 {
		t.Fatalf("nil 行应跳过，期望 2 项，实际 %d", len(got))
	}
	if got[0].Label != "颜色（color）" {
		t.Fatalf("标签应是「名字（key）」，实际 %q", got[0].Label)
	}
	if !got[0].IsVariation {
		t.Fatal("IsVariation 应透传（模板据此显示「参与变体」徽标）")
	}
	if got[1].Label != "无 key" {
		t.Fatalf("没有 key 时不该拼空的括号，实际 %q", got[1].Label)
	}
}

var _ productcontract.WarehouseOptionsPort = (*productPageWarehousePortStub)(nil)
var _ productcontract.EntityTemplatePort = (*productPageTemplatePortStub)(nil)
