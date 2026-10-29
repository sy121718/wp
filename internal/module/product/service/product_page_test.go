package productservice

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
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
