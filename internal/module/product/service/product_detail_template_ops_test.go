package productservice_test

// product_detail_template_ops_test.go — 「详情页模板」写动作与预览编排的 service 层直接判据
//（dd.txt 第十一章合并门槛第 6 条「SEO 评分、模板发布及 URLPath 规则有独立测试」）。
//
// 此前这条门槛只由端点级 feature 测试（public/test/product/...）覆盖：那里的断言落在
// 「页面渲染出什么」上，而这一层真正的可判定面是**编排顺序**与**传到端口的参数** ——
//   · CreateDetailTemplate 先取文档、再建模板（反了会在取文档失败时留下一套没有内容的模板）；
//   · PublishDetailTemplate 只在 urlPath 为空时才查商品（非空时查商品是多一次无用取数）；
//   · 取商品失败必须可 errors.As 分流（它和「建实例失败」走两张不同的文案表）。
// 这三条全反了页面照样 200、feature 测试全绿 —— 所以这里要 service 层的直接判据。
//
// 四个端口全部 fake，不连数据库：Templates（contenttemplate 契约）、Instances（本文件被测的
// 窄端口 DetailTemplateInstancePort）、Products（ProductReadPort）、Projects（siteurl 的
// ProjectDetailReader）。断言只落在「端口收到了什么参数」与「谁被调用 / 谁没被调用」上，
// 不碰被测函数的私有实现。
//
// fake 的未设定端口一律**调用即失败**（t.Fatalf，或嵌入接口的 nil 调用 panic）：
// 这样注入缺陷时变红的是「不该发生的那次调用」本身，而不是某个字段的值 ——
// 例如把 if req.URLPath == "" 改成恒真，报的是「Products.Get 被调用」，一眼看出违反的是顺序契约。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/siteurl"
)

const (
	detailTemplateTestProjectID  = "proj-dt-1"
	detailTemplateTestProductID  = "prod-dt-1"
	detailTemplateTestTemplateID = "tpl-dt-1"
)

// 注入用的哨兵错误：每一个代表「某一步失败」的独立原因，用例靠 errors.Is 认出它确实冒了泡。
var (
	errDetailTemplateResolve     = errors.New("injected: 取默认模板失败")
	errDetailTemplateDocRead     = errors.New("injected: 读模板文档失败")
	errDetailTemplateCreateTpl   = errors.New("injected: 建模板失败")
	errDetailTemplateProductRead = errors.New("injected: 取商品失败")
	errDetailTemplateCreateInst  = errors.New("injected: 建发布实例失败")
	errDetailTemplateRebuild     = errors.New("injected: 重建实例失败")
	errDetailTemplateUpdateURL   = errors.New("injected: 改实例路径失败")
	errDetailTemplatePreview     = errors.New("injected: 预览渲染失败")
)

// detailTemplateJSON 构造用例用的 JSON。写成 helper 而不是原始字符串字面量：
// 本文件的断言是「文档原样透传」，需要的是一个能被 json.Marshal 稳定复现的值。
func detailTemplateJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("构造用例 JSON 失败：%v", err)
	}
	return b
}

// detailTemplateCopySourceDoc 复制来源模板的文档（形状与编辑器建的空文档一致）。
func detailTemplateCopySourceDoc(t *testing.T) json.RawMessage {
	t.Helper()
	return detailTemplateJSON(t, map[string]any{"settings": map[string]any{}, "root": []any{}})
}

// ---- 端口 fake ----

// fakeDetailTemplates 实现 contenttemplatecontract.ContentTemplateService。
//
// 只覆盖本文件用到的四个方法（ResolveTemplateScoped / ResolveTemplate / Get / Create）；
// ResolveTemplate 一并留着是**判据的一部分**：它是无工程的入口，多工程部署下解析不出工程，
// 被测编排一次都不该走它 —— 走了就是本批修掉的那个老缺陷，记录字段供断言钉住。
// 其余十四个方法靠**嵌入接口**兜底：
// 一旦被走到就是 nil 接口方法调用 panic —— 「编排落到别的模板方法上」是缺陷，用 panic 保证它不静默通过。
type fakeDetailTemplates struct {
	contenttemplatecontract.ContentTemplateService

	resolveEntityTypes []string

	// 带作用域入口收到的工程 id 与实体类型（顺序一一对应）。
	resolveScopedProjects    []string
	resolveScopedEntityTypes []string

	resolveErr        error
	resolveTemplateID string

	getIDs     []string
	getErr     error
	getDoc     json.RawMessage
	getNilResp bool // 模拟契约违约：Get 成功但返回 (nil, nil)

	createReqs []*contenttemplatecontract.CreateReq
	createErr  error
}

func (f *fakeDetailTemplates) ResolveTemplate(_ context.Context, entityType string) (*contenttemplatecontract.ResolvedTemplate, error) {
	f.resolveEntityTypes = append(f.resolveEntityTypes, entityType)
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &contenttemplatecontract.ResolvedTemplate{TemplateID: f.resolveTemplateID, EntityType: entityType}, nil
}

// ResolveTemplateScoped 带作用域的入口：记录收到的工程 id 与实体类型，返回与 ResolveTemplate 同形的结果。
func (f *fakeDetailTemplates) ResolveTemplateScoped(_ context.Context, projectID, entityType string) (*contenttemplatecontract.ResolvedTemplate, error) {
	f.resolveScopedProjects = append(f.resolveScopedProjects, projectID)
	f.resolveScopedEntityTypes = append(f.resolveScopedEntityTypes, entityType)
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &contenttemplatecontract.ResolvedTemplate{TemplateID: f.resolveTemplateID, EntityType: entityType}, nil
}

func (f *fakeDetailTemplates) Get(_ context.Context, req *contenttemplatecontract.GetReq) (*contenttemplatecontract.TemplateResp, error) {
	f.getIDs = append(f.getIDs, req.ID)
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getNilResp {
		return nil, nil
	}
	return &contenttemplatecontract.TemplateResp{ID: req.ID, DraftDocument: f.getDoc}, nil
}

func (f *fakeDetailTemplates) Create(_ context.Context, req *contenttemplatecontract.CreateReq) (*contenttemplatecontract.TemplateResp, error) {
	f.createReqs = append(f.createReqs, req)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &contenttemplatecontract.TemplateResp{ID: "tpl-created"}, nil
}

// fakeDetailInstances 实现 productservice.DetailTemplateInstancePort（四个方法各记一次入参）。
type fakeDetailInstances struct {
	previewReqs []*presentationcontract.PreviewInstanceReq
	previewResp *presentationcontract.PreviewInstanceResp
	previewErr  error

	createReqs []*presentationcontract.CreateInstanceReq
	createErr  error

	rebuildReqs []*presentationcontract.RebuildReq
	rebuildErr  error

	updateURLReqs []*presentationcontract.UpdateURLReq
	updateURLErr  error
}

func (f *fakeDetailInstances) PreviewInstance(_ context.Context, req *presentationcontract.PreviewInstanceReq) (*presentationcontract.PreviewInstanceResp, error) {
	f.previewReqs = append(f.previewReqs, req)
	return f.previewResp, f.previewErr
}

func (f *fakeDetailInstances) CreateInstance(_ context.Context, req *presentationcontract.CreateInstanceReq) (*presentationcontract.InstanceResp, error) {
	f.createReqs = append(f.createReqs, req)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &presentationcontract.InstanceResp{ID: "inst-created", URLPath: req.URLPath}, nil
}

func (f *fakeDetailInstances) Rebuild(_ context.Context, req *presentationcontract.RebuildReq) (*presentationcontract.InstanceResp, error) {
	f.rebuildReqs = append(f.rebuildReqs, req)
	if f.rebuildErr != nil {
		return nil, f.rebuildErr
	}
	return &presentationcontract.InstanceResp{ID: "inst-rebuilt"}, nil
}

func (f *fakeDetailInstances) UpdateURL(_ context.Context, req *presentationcontract.UpdateURLReq) (*presentationcontract.InstanceResp, error) {
	f.updateURLReqs = append(f.updateURLReqs, req)
	if f.updateURLErr != nil {
		return nil, f.updateURLErr
	}
	return &presentationcontract.InstanceResp{ID: "inst-updated"}, nil
}

// fakeDetailProducts 实现 productservice.ProductReadPort。
//
// Get 之外的五个读口本文件一次都不该走到，用 t.Fatalf 断言「不该发生」——
// 拿一个什么都返回的万能 stub，编排走错分支（比如从分类读口取商品）反而测不出来。
type fakeDetailProducts struct {
	t   *testing.T
	get func(*productdto.GetReq) (*productdto.ProductResp, error)

	getReqs []*productdto.GetReq
}

// Get 未设定 get 即调用失败：urlPath 非空的用例里，「查了商品」本身就是要检出的缺陷。
func (p *fakeDetailProducts) Get(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
	if p.get == nil {
		p.t.Fatalf("Products.Get 被调用（id=%s）：本用例的 urlPath 非空，编排不该查商品", req.ID)
	}
	p.getReqs = append(p.getReqs, req)
	return p.get(req)
}

func (p *fakeDetailProducts) unexpected(method string) {
	p.t.Fatalf("ProductReadPort.%s 被调用：本文件只经 Get 取数", method)
}

func (p *fakeDetailProducts) List(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) {
	p.unexpected("List")
	return nil, nil
}

func (p *fakeDetailProducts) GetCategory(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
	p.unexpected("GetCategory")
	return nil, nil
}

func (p *fakeDetailProducts) ListCategories(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	p.unexpected("ListCategories")
	return nil, nil
}

func (p *fakeDetailProducts) GetBrand(context.Context, *productdto.GetBrandReq) (*productdto.BrandResp, error) {
	p.unexpected("GetBrand")
	return nil, nil
}

func (p *fakeDetailProducts) ListBrands(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	p.unexpected("ListBrands")
	return nil, nil
}

// fakeDetailProjects 实现 siteurl.ProjectDetailReader —— 默认路径规则的唯一来源。
type fakeDetailProjects struct {
	settings json.RawMessage
	err      error

	detailIDs []string
}

func (p *fakeDetailProjects) Detail(_ context.Context, req *projectcontract.DetailReq) (*projectcontract.ProjectResp, error) {
	p.detailIDs = append(p.detailIDs, req.ID)
	if p.err != nil {
		return nil, p.err
	}
	return &projectcontract.ProjectResp{ID: req.ID, Settings: p.settings}, nil
}

// detailTemplateDeps 组装依赖（四项都可空，与被测函数的契约一致）。
func detailTemplateDeps(templates contenttemplatecontract.ContentTemplateService,
	instances productservice.DetailTemplateInstancePort,
	products productservice.ProductReadPort,
	projects siteurl.ProjectDetailReader) productservice.DetailTemplateDeps {
	return productservice.DetailTemplateDeps{
		Templates: templates,
		Instances: instances,
		Products:  products,
		Projects:  projects,
	}
}

// ---- 1. DetailTemplateDocument：默认模板回落与 Get ----

// 空 templateID（这里是纯空白）→ 先 ResolveTemplate 取 product 的默认模板，再用那个 id 去 Get。
func TestDetailTemplateDocumentBlankIDResolvesDefaultTemplate(t *testing.T) {
	ctx := context.Background()
	doc := detailTemplateCopySourceDoc(t)
	templates := &fakeDetailTemplates{resolveTemplateID: "tpl-default", getDoc: doc}

	got, err := productservice.DetailTemplateDocument(ctx, templates, detailTemplateTestProjectID, "   ")
	if err != nil {
		t.Fatalf("DetailTemplateDocument 报错：%v", err)
	}
	if string(got) != string(doc) {
		t.Fatalf("文档 = %s，want %s（DraftDocument 应原样返回）", got, doc)
	}
	// 判据 1：走的是带作用域的入口（无工程的 ResolveTemplate 一次都不该被调用）。
	if len(templates.resolveEntityTypes) != 0 {
		t.Fatalf("无工程的 ResolveTemplate 被调用 %v：多工程部署下它解析不出工程（ErrProjectRequired），"+
			"缺省来源必须走 ResolveTemplateScoped", templates.resolveEntityTypes)
	}
	// 判据 2：作用域原样传到 contenttemplate —— 传空串等价于没传，多工程下同样失败。
	if len(templates.resolveScopedProjects) != 1 || templates.resolveScopedProjects[0] != detailTemplateTestProjectID {
		t.Fatalf("ResolveTemplateScoped 收到的工程 id = %v，want [%s]", templates.resolveScopedProjects, detailTemplateTestProjectID)
	}
	if len(templates.resolveScopedEntityTypes) != 1 || templates.resolveScopedEntityTypes[0] != productcontract.EntityTypeProduct {
		t.Fatalf("ResolveTemplateScoped 收到的实体类型 = %v，want [%s]", templates.resolveScopedEntityTypes, productcontract.EntityTypeProduct)
	}
	if len(templates.getIDs) != 1 || templates.getIDs[0] != "tpl-default" {
		t.Fatalf("Get 收到的 id = %v，want [tpl-default]（应取 ResolveTemplate 解析出来的默认模板）", templates.getIDs)
	}
}

// 非空 templateID：TrimSpace 后直接 Get，不查默认模板（多查一次会在「类型无默认模板」时误报失败）。
func TestDetailTemplateDocumentExplicitIDTrimsAndSkipsResolve(t *testing.T) {
	ctx := context.Background()
	templates := &fakeDetailTemplates{getDoc: detailTemplateCopySourceDoc(t)}

	if _, err := productservice.DetailTemplateDocument(ctx, templates, detailTemplateTestProjectID, "  tpl-explicit  "); err != nil {
		t.Fatalf("DetailTemplateDocument 报错：%v", err)
	}
	if len(templates.resolveEntityTypes) != 0 {
		t.Fatalf("显式指定模板时 ResolveTemplate 被调用 %d 次（%v）：不该查默认模板", len(templates.resolveEntityTypes), templates.resolveEntityTypes)
	}
	if len(templates.resolveScopedProjects) != 0 {
		t.Fatalf("显式指定模板时 ResolveTemplateScoped 被调用 %d 次（%v）：不该查默认模板", len(templates.resolveScopedProjects), templates.resolveScopedProjects)
	}
	if len(templates.getIDs) != 1 || templates.getIDs[0] != "tpl-explicit" {
		t.Fatalf("Get 收到的 id = %v，want [tpl-explicit]（前后空白应被 TrimSpace 掉）", templates.getIDs)
	}
}

// 取默认模板失败：原样冒泡，且不继续 Get。
func TestDetailTemplateDocumentResolveFailureBubbles(t *testing.T) {
	templates := &fakeDetailTemplates{resolveErr: errDetailTemplateResolve}

	doc, err := productservice.DetailTemplateDocument(context.Background(), templates, detailTemplateTestProjectID, "")
	if !errors.Is(err, errDetailTemplateResolve) {
		t.Fatalf("err = %v，want %v（取默认模板失败应原样返回）", err, errDetailTemplateResolve)
	}
	if doc != nil {
		t.Fatalf("取默认模板失败时文档应为 nil，实际 %s", doc)
	}
	if len(templates.getIDs) != 0 {
		t.Fatalf("取默认模板失败后仍调用 Get %v：应当直接返回", templates.getIDs)
	}
}

// Get 失败：原样冒泡（不许吞成空文档 —— 那会变成「复制了一套空模板」）。
func TestDetailTemplateDocumentGetFailureBubbles(t *testing.T) {
	templates := &fakeDetailTemplates{getErr: errDetailTemplateDocRead}

	doc, err := productservice.DetailTemplateDocument(context.Background(), templates, detailTemplateTestProjectID, detailTemplateTestTemplateID)
	if !errors.Is(err, errDetailTemplateDocRead) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplateDocRead)
	}
	if doc != nil {
		t.Fatalf("Get 失败时文档应为 nil，实际 %s", doc)
	}
}

// 契约违约（Get 返回 nil, nil）：必须回一个明确错误，而不是 panic。
//
// 去掉护栏后这一条会 panic（nil 解引用），测试进程直接崩 —— 拦不住就必然变红。
func TestDetailTemplateDocumentNilGetResultFailsInsteadOfPanicking(t *testing.T) {
	templates := &fakeDetailTemplates{getNilResp: true}

	doc, err := productservice.DetailTemplateDocument(context.Background(), templates, detailTemplateTestProjectID, detailTemplateTestTemplateID)
	if err == nil {
		t.Fatalf("Get 返回 (nil, nil) 时应报错，实际 nil（文档 = %s）", doc)
	}
	if !strings.Contains(err.Error(), "空结果") {
		t.Fatalf("err = %v，want 明确说明「Get 返回空结果」的错误", err)
	}
	if doc != nil {
		t.Fatalf("出错时文档应为 nil，实际 %s", doc)
	}
}

// ---- 2. CreateDetailTemplate：顺序契约 ----

// **顺序契约**：取文档失败时 Templates.Create 一次都不许被调用。
//
// 反过来（先建空模板再补文档）的症状最难恢复：库里多出一套没有内容的模板，
// 而用户当时看到的是一个失败提示 —— 他会以为什么都没发生。
func TestCreateDetailTemplateReadsDocumentBeforeCreatingTemplate(t *testing.T) {
	ctx := context.Background()
	templates := &fakeDetailTemplates{getErr: errDetailTemplateDocRead}
	instances := &fakeDetailInstances{}

	err := productservice.CreateDetailTemplate(ctx,
		detailTemplateDeps(templates, instances, nil, nil),
		detailTemplateTestProjectID, "详情页模板 A", detailTemplateTestTemplateID)
	if !errors.Is(err, errDetailTemplateDocRead) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplateDocRead)
	}
	if len(templates.createReqs) != 0 {
		t.Fatalf("取文档失败却调用了 Templates.Create %d 次：顺序契约要求「先取文档，取不到就不建模板」", len(templates.createReqs))
	}
}

// 成功路径：Create 收到的四个字段逐字段透传（EntityType 必须是 product）。
func TestCreateDetailTemplatePassesDraftDocumentIntoCreate(t *testing.T) {
	ctx := context.Background()
	doc := detailTemplateCopySourceDoc(t)
	templates := &fakeDetailTemplates{getDoc: doc}
	const name = "详情页模板 B"

	if err := productservice.CreateDetailTemplate(ctx,
		detailTemplateDeps(templates, &fakeDetailInstances{}, nil, nil),
		detailTemplateTestProjectID, name, detailTemplateTestTemplateID); err != nil {
		t.Fatalf("CreateDetailTemplate 报错：%v", err)
	}
	if len(templates.createReqs) != 1 {
		t.Fatalf("Templates.Create 被调用 %d 次，want 1", len(templates.createReqs))
	}
	req := templates.createReqs[0]
	if req.EntityType != productcontract.EntityTypeProduct {
		t.Fatalf("CreateReq.EntityType = %q，want %q", req.EntityType, productcontract.EntityTypeProduct)
	}
	if req.Name != name {
		t.Fatalf("CreateReq.Name = %q，want %q", req.Name, name)
	}
	if req.ProjectID != detailTemplateTestProjectID {
		t.Fatalf("CreateReq.ProjectID = %q，want %q", req.ProjectID, detailTemplateTestProjectID)
	}
	if string(req.DraftDocument) != string(doc) {
		t.Fatalf("CreateReq.DraftDocument = %s，want %s（复制来源的文档应原样带过去）", req.DraftDocument, doc)
	}
}

// 建模板失败：原样冒泡。
func TestCreateDetailTemplatePropagatesCreateFailure(t *testing.T) {
	templates := &fakeDetailTemplates{getDoc: detailTemplateCopySourceDoc(t), createErr: errDetailTemplateCreateTpl}

	err := productservice.CreateDetailTemplate(context.Background(),
		detailTemplateDeps(templates, &fakeDetailInstances{}, nil, nil),
		detailTemplateTestProjectID, "详情页模板 C", detailTemplateTestTemplateID)
	if !errors.Is(err, errDetailTemplateCreateTpl) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplateCreateTpl)
	}
}

// 坏数据护栏：复制来源的文档为空（空串 / 纯空白 / JSON null）时必须报错，且**不建模板**。
//
// 去掉护栏后 Create 会被调用，库里多出一套没有内容的模板 —— 编辑器打开是一片空白，
// 而用户当时看到的是成功提示。四种「空」的写法都要覆盖：空串与 JSON null 是两种不同的
// 入库结果（前者落空 jsonb、后者落 JSON 字面量 null），只测其中一种会漏掉另一种。
func TestCreateDetailTemplateRejectsEmptyDocument(t *testing.T) {
	cases := []struct {
		name string
		doc  json.RawMessage
	}{
		{"nil", nil},
		{"空串", json.RawMessage("")},
		{"纯空白", json.RawMessage("   \n\t")},
		{"JSON null", json.RawMessage("null")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			templates := &fakeDetailTemplates{getDoc: tc.doc}

			err := productservice.CreateDetailTemplate(context.Background(),
				detailTemplateDeps(templates, &fakeDetailInstances{}, nil, nil),
				detailTemplateTestProjectID, "详情页模板 空文档", detailTemplateTestTemplateID)
			if err == nil {
				t.Fatalf("来源文档为空（%q）时应报错，实际 nil：空文档会被原样写进库", tc.doc)
			}
			if !strings.Contains(err.Error(), "文档为空") {
				t.Fatalf("err = %v，want 明确说明「文档为空」的错误", err)
			}
			if len(templates.createReqs) != 0 {
				t.Fatalf("空文档却调用了 Templates.Create %d 次：取不到内容就不该建模板", len(templates.createReqs))
			}
		})
	}
}

// ---- 3. PublishDetailTemplate：URLPath 规则与错误分流 ----

// urlPath 非空：直接用调用方给的路径建实例，**一次都不许查商品**（Products.Get 未设定即失败）。
func TestPublishDetailTemplateExplicitURLPathSkipsProductLookup(t *testing.T) {
	ctx := context.Background()
	instances := &fakeDetailInstances{}
	products := &fakeDetailProducts{t: t} // 未设定 get：被调用即红
	projects := &fakeDetailProjects{}
	const urlPath = "/custom/tee-path"

	if err := productservice.PublishDetailTemplate(ctx,
		detailTemplateDeps(nil, instances, products, projects),
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID, urlPath); err != nil {
		t.Fatalf("PublishDetailTemplate 报错：%v", err)
	}
	if len(projects.detailIDs) != 0 {
		t.Fatalf("urlPath 非空却查了工程 URL 规则 %d 次：派生只发生在 urlPath 为空时", len(projects.detailIDs))
	}
	if len(instances.createReqs) != 1 {
		t.Fatalf("CreateInstance 被调用 %d 次，want 1", len(instances.createReqs))
	}
	req := instances.createReqs[0]
	if req.URLPath != urlPath {
		t.Fatalf("CreateInstanceReq.URLPath = %q，want %q（非空时原样透传，不做规范化）", req.URLPath, urlPath)
	}
	if req.EntityType != productcontract.EntityTypeProduct {
		t.Fatalf("CreateInstanceReq.EntityType = %q，want %q", req.EntityType, productcontract.EntityTypeProduct)
	}
	if req.EntityID != detailTemplateTestProductID {
		t.Fatalf("CreateInstanceReq.EntityID = %q，want %q", req.EntityID, detailTemplateTestProductID)
	}
	if req.TemplateID != detailTemplateTestTemplateID {
		t.Fatalf("CreateInstanceReq.TemplateID = %q，want %q", req.TemplateID, detailTemplateTestTemplateID)
	}
	if req.ProjectID != detailTemplateTestProjectID {
		t.Fatalf("CreateInstanceReq.ProjectID = %q，want %q", req.ProjectID, detailTemplateTestProjectID)
	}
}

// urlPath 为空 + 取商品失败：错误必须能被 errors.As 分流成 *DetailTemplateProductLookupError，
// errors.Is 能到底层哨兵，且此时不建实例。
//
// 不分流的后果是文案错位：商品早已不在，用户却看到「这个商品还没有详情页实例」，于是去点一次发布。
func TestPublishDetailTemplateProductLookupFailureIsDistinguishable(t *testing.T) {
	ctx := context.Background()
	instances := &fakeDetailInstances{}
	products := &fakeDetailProducts{t: t, get: func(*productdto.GetReq) (*productdto.ProductResp, error) {
		return nil, errDetailTemplateProductRead
	}}

	err := productservice.PublishDetailTemplate(ctx,
		detailTemplateDeps(nil, instances, products, &fakeDetailProjects{}),
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID, "")
	if err == nil {
		t.Fatalf("取商品失败应报错，实际 nil")
	}
	var lookupErr *productservice.DetailTemplateProductLookupError
	if !errors.As(err, &lookupErr) {
		t.Fatalf("err = %T（%v），want *DetailTemplateProductLookupError：调用方靠它分流两张文案表", err, err)
	}
	if !errors.Is(err, errDetailTemplateProductRead) {
		t.Fatalf("errors.Is(err, 底层哨兵) = false（err=%v）：Unwrap 没把底层原因暴露出来", err)
	}
	if !errors.Is(lookupErr.Err, errDetailTemplateProductRead) {
		t.Fatalf("包装内的 Err = %v，want %v", lookupErr.Err, errDetailTemplateProductRead)
	}
	if len(instances.createReqs) != 0 {
		t.Fatalf("取商品失败却建了 %d 个实例：应当直接返回，不继续建实例", len(instances.createReqs))
	}
	if len(products.getReqs) != 1 || products.getReqs[0].ID != detailTemplateTestProductID {
		t.Fatalf("Products.Get 收到的 id = %v，want [%s]", products.getReqs, detailTemplateTestProductID)
	}
}

// urlPath 为空 + 取商品成功：CreateInstance 收到的 URLPath 必须等于
// siteurl.DetailPathForProject 同参数的结果（不是写死的默认模式，也不是拼出来的近似值）。
func TestPublishDetailTemplateDerivesURLPathFromProjectRules(t *testing.T) {
	ctx := context.Background()
	const slug = "  tee-shirt  "
	projects := &fakeDetailProjects{settings: detailTemplateJSON(t, map[string]any{
		"urlPatterns": map[string]string{siteurl.KindProduct: "/shop/{slug}-{id}"},
	})}
	products := &fakeDetailProducts{t: t, get: func(*productdto.GetReq) (*productdto.ProductResp, error) {
		return &productdto.ProductResp{ID: detailTemplateTestProductID, Slug: slug}, nil
	}}

	// 期望值用同样的入参自己算一遍（不硬编码字符串）。
	want := siteurl.DetailPathForProject(ctx, projects, detailTemplateTestProjectID,
		siteurl.KindProduct, slug, detailTemplateTestProductID)
	// 判据有效性自证：工程规则与默认模式必须**不同形**，否则「读了工程规则」与「写死默认模式」
	// 在断言上无法区分（settings 写错 / 未被解析时会静默退化成假的绿）。
	defaultPath := siteurl.DetailPath(siteurl.KindProduct, slug, detailTemplateTestProductID, nil)
	if want == defaultPath {
		t.Fatalf("判据无效：工程规则（/shop/{slug}-{id}）派生出的路径与默认模式同形（%q）", want)
	}
	projectReadsBefore := len(projects.detailIDs)

	instances := &fakeDetailInstances{}
	if err := productservice.PublishDetailTemplate(ctx,
		detailTemplateDeps(nil, instances, products, projects),
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID, ""); err != nil {
		t.Fatalf("PublishDetailTemplate 报错：%v", err)
	}
	if len(instances.createReqs) != 1 {
		t.Fatalf("CreateInstance 被调用 %d 次，want 1", len(instances.createReqs))
	}
	if got := instances.createReqs[0].URLPath; got != want {
		t.Fatalf("CreateInstanceReq.URLPath = %q，want %q（DetailPathForProject 同参数的结果；硬编码默认模式会得到 %q）",
			got, want, defaultPath)
	}
	// 取商品这一跳同样必须在工程作用域内：product 侧 Get 经 resolveProjectID 解析工程，
	// 不带作用域时多工程部署直接回 ErrInvalidParam —— 单工程夹具里它恰好解析出唯一的那个工程，
	// 于是「带了」与「没带」在既有端点上行为等价、差异不可观测（本文件这条断言是它的直接判据）。
	if len(products.getReqs) != 1 {
		t.Fatalf("Products.Get 被调用 %d 次，want 1（派生路径必须先取商品拿 slug）", len(products.getReqs))
	}
	if got := products.getReqs[0].ProjectID; got != detailTemplateTestProjectID {
		t.Fatalf("Products.Get 收到的 GetReq.ProjectID = %q，want %q（漏传时多工程下 product 侧 resolveProjectID 回 ErrInvalidParam）",
			got, detailTemplateTestProjectID)
	}
	if reads := len(projects.detailIDs) - projectReadsBefore; reads != 1 {
		t.Fatalf("派生默认路径时查工程 URL 规则 %d 次，want 1", reads)
	}
}

// 建实例失败：原样冒泡，且**不能**被包装成取商品失败（两张文案表不能串）。
func TestPublishDetailTemplateCreateInstanceFailureIsNotProductLookup(t *testing.T) {
	ctx := context.Background()
	instances := &fakeDetailInstances{createErr: errDetailTemplateCreateInst}

	err := productservice.PublishDetailTemplate(ctx,
		detailTemplateDeps(nil, instances, &fakeDetailProducts{t: t}, &fakeDetailProjects{}),
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID, "/explicit/path")
	if !errors.Is(err, errDetailTemplateCreateInst) {
		t.Fatalf("err = %v，want %v（建实例失败应原样冒泡）", err, errDetailTemplateCreateInst)
	}
	var lookupErr *productservice.DetailTemplateProductLookupError
	if errors.As(err, &lookupErr) {
		t.Fatalf("建实例失败被包装成 *DetailTemplateProductLookupError：调用方会按「商品取数失败」翻文案，而商品其实读到了")
	}
	if len(instances.createReqs) != 1 {
		t.Fatalf("CreateInstance 被调用 %d 次，want 1", len(instances.createReqs))
	}
}

// ---- 4. UpdateDetailTemplateURL：参数透传 ----

// 五个字段逐字段透传。WithRedirect 写错（true → false）不会报错，只会让旧链接被直接取消激活
// 而不是登记 301 —— 用户看不见区别，直到外链全断。
//
// ProjectID 漏传（原先的缺陷）同样不报错：presentation 会按「工程唯一时取该工程」兜底，
// 单工程站点上一切正常，多工程站点改的是**另一个工程**的线上路径。
func TestUpdateDetailTemplateURLPassesFieldsThrough(t *testing.T) {
	instances := &fakeDetailInstances{}
	const newPath = "/shop/tee-shirt"

	if err := productservice.UpdateDetailTemplateURL(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, newPath, true); err != nil {
		t.Fatalf("UpdateDetailTemplateURL 报错：%v", err)
	}
	if len(instances.updateURLReqs) != 1 {
		t.Fatalf("UpdateURL 被调用 %d 次，want 1", len(instances.updateURLReqs))
	}
	req := instances.updateURLReqs[0]
	if req.EntityType != productcontract.EntityTypeProduct {
		t.Fatalf("UpdateURLReq.EntityType = %q，want %q", req.EntityType, productcontract.EntityTypeProduct)
	}
	if req.EntityID != detailTemplateTestProductID {
		t.Fatalf("UpdateURLReq.EntityID = %q，want %q", req.EntityID, detailTemplateTestProductID)
	}
	if req.ProjectID != detailTemplateTestProjectID {
		t.Fatalf("UpdateURLReq.ProjectID = %q，want %q（工程作用域靠调用方传入，漏传会落到别的工程）",
			req.ProjectID, detailTemplateTestProjectID)
	}
	if req.NewPath != newPath {
		t.Fatalf("UpdateURLReq.NewPath = %q，want %q", req.NewPath, newPath)
	}
	if !req.WithRedirect {
		t.Fatalf("UpdateURLReq.WithRedirect = false，want true（旧路径应登记 301 而不是被直接取消激活）")
	}
}

// 改 URL 失败：原样冒泡。
func TestUpdateDetailTemplateURLPropagatesFailure(t *testing.T) {
	instances := &fakeDetailInstances{updateURLErr: errDetailTemplateUpdateURL}

	err := productservice.UpdateDetailTemplateURL(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, "/shop/tee-shirt", false)
	if !errors.Is(err, errDetailTemplateUpdateURL) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplateUpdateURL)
	}
	if len(instances.updateURLReqs) != 1 || instances.updateURLReqs[0].WithRedirect {
		t.Fatalf("WithRedirect = %v，want false（这一档是「直接取消旧路径激活」）", instances.updateURLReqs)
	}
}

// 空串也原样透传：service 不替调用方挑工程。
//
// 判据能变红：若在这里加一层「空则取唯一工程 / 默认工程」的兜底，收到的就不是空串；
// 而那一兜底正是多工程站点改错站点的根因，必须留在 presentation 的显式解析里。
func TestUpdateDetailTemplateURLPassesEmptyProjectIDThrough(t *testing.T) {
	instances := &fakeDetailInstances{}

	if err := productservice.UpdateDetailTemplateURL(context.Background(), instances,
		"", detailTemplateTestProductID, "/shop/tee-shirt", false); err != nil {
		t.Fatalf("UpdateDetailTemplateURL 报错：%v", err)
	}
	if len(instances.updateURLReqs) != 1 {
		t.Fatalf("UpdateURL 被调用 %d 次，want 1", len(instances.updateURLReqs))
	}
	if got := instances.updateURLReqs[0].ProjectID; got != "" {
		t.Fatalf("UpdateURLReq.ProjectID = %q，want \"\"：service 只透传，不替调用方解析工程", got)
	}
}

// ---- 5. ApplyDetailTemplate：参数透传 ----

// ProjectID / EntityID / TemplateID 逐字段透传（TemplateID 漏传 → 沿用实例当前绑定，切换模板
// 看起来「没生效」；ProjectID 漏传 → 多工程站点上重建的是另一个工程的实例，同样是「看起来没生效」）。
func TestApplyDetailTemplatePassesEntityAndTemplate(t *testing.T) {
	instances := &fakeDetailInstances{}

	if err := productservice.ApplyDetailTemplate(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID); err != nil {
		t.Fatalf("ApplyDetailTemplate 报错：%v", err)
	}
	if len(instances.rebuildReqs) != 1 {
		t.Fatalf("Rebuild 被调用 %d 次，want 1", len(instances.rebuildReqs))
	}
	req := instances.rebuildReqs[0]
	if req.ProjectID != detailTemplateTestProjectID {
		t.Fatalf("RebuildReq.ProjectID = %q，want %q（工程作用域靠调用方传入，漏传会落到别的工程）",
			req.ProjectID, detailTemplateTestProjectID)
	}
	if req.EntityID != detailTemplateTestProductID {
		t.Fatalf("RebuildReq.EntityID = %q，want %q", req.EntityID, detailTemplateTestProductID)
	}
	if req.TemplateID != detailTemplateTestTemplateID {
		t.Fatalf("RebuildReq.TemplateID = %q，want %q", req.TemplateID, detailTemplateTestTemplateID)
	}
}

// 重建失败：原样冒泡。
func TestApplyDetailTemplatePropagatesFailure(t *testing.T) {
	instances := &fakeDetailInstances{rebuildErr: errDetailTemplateRebuild}

	err := productservice.ApplyDetailTemplate(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID)
	if !errors.Is(err, errDetailTemplateRebuild) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplateRebuild)
	}
}

// 空串同样原样透传（与 UpdateDetailTemplateURL 同一判据）。
func TestApplyDetailTemplatePassesEmptyProjectIDThrough(t *testing.T) {
	instances := &fakeDetailInstances{}

	if err := productservice.ApplyDetailTemplate(context.Background(), instances,
		"", detailTemplateTestProductID, detailTemplateTestTemplateID); err != nil {
		t.Fatalf("ApplyDetailTemplate 报错：%v", err)
	}
	if len(instances.rebuildReqs) != 1 {
		t.Fatalf("Rebuild 被调用 %d 次，want 1", len(instances.rebuildReqs))
	}
	if got := instances.rebuildReqs[0].ProjectID; got != "" {
		t.Fatalf("RebuildReq.ProjectID = %q，want \"\"：service 只透传，不替调用方解析工程", got)
	}
}

// ---- 6. PreviewDetailTemplate：参数透传 / 返回原指针 ----

// ProjectID / EntityType / EntityID / TemplateID 逐字段透传。
// ProjectID 漏传会让集合源落到「唯一工程」兜底上 —— 多工程部署时预览取错站点的数据。
func TestPreviewDetailTemplatePassesFieldsThrough(t *testing.T) {
	instances := &fakeDetailInstances{previewResp: &presentationcontract.PreviewInstanceResp{HTML: "<p>preview</p>"}}

	if _, err := productservice.PreviewDetailTemplate(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID); err != nil {
		t.Fatalf("PreviewDetailTemplate 报错：%v", err)
	}
	if len(instances.previewReqs) != 1 {
		t.Fatalf("PreviewInstance 被调用 %d 次，want 1", len(instances.previewReqs))
	}
	req := instances.previewReqs[0]
	if req.ProjectID != detailTemplateTestProjectID {
		t.Fatalf("PreviewInstanceReq.ProjectID = %q，want %q", req.ProjectID, detailTemplateTestProjectID)
	}
	if req.EntityType != productcontract.EntityTypeProduct {
		t.Fatalf("PreviewInstanceReq.EntityType = %q，want %q", req.EntityType, productcontract.EntityTypeProduct)
	}
	if req.EntityID != detailTemplateTestProductID {
		t.Fatalf("PreviewInstanceReq.EntityID = %q，want %q", req.EntityID, detailTemplateTestProductID)
	}
	if req.TemplateID != detailTemplateTestTemplateID {
		t.Fatalf("PreviewInstanceReq.TemplateID = %q，want %q", req.TemplateID, detailTemplateTestTemplateID)
	}
}

// 预览失败：原样冒泡。
func TestPreviewDetailTemplatePropagatesFailure(t *testing.T) {
	instances := &fakeDetailInstances{previewErr: errDetailTemplatePreview}

	resp, err := productservice.PreviewDetailTemplate(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID)
	if !errors.Is(err, errDetailTemplatePreview) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplatePreview)
	}
	if resp != nil {
		t.Fatalf("预览失败时 resp 应为 nil，实际 %+v", resp)
	}
}

// 返回值必须是 fake 给的那个指针本身（不是复制）—— 调用方据此在响应头上标注「预览的是哪一套」，
// 复制一份会让「模板名 / 版本」与真正渲染用的那套脱钩。
func TestPreviewDetailTemplateReturnsFakeRespPointer(t *testing.T) {
	want := &presentationcontract.PreviewInstanceResp{
		HTML:              "<p>preview</p>",
		EntityType:        productcontract.EntityTypeProduct,
		EntityID:          detailTemplateTestProductID,
		TemplateID:        detailTemplateTestTemplateID,
		TemplateName:      "详情页模板 A",
		TemplateVersionID: "ver-1",
		TemplateVersion:   3,
	}
	instances := &fakeDetailInstances{previewResp: want}

	got, err := productservice.PreviewDetailTemplate(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID)
	if err != nil {
		t.Fatalf("PreviewDetailTemplate 报错：%v", err)
	}
	if got != want {
		t.Fatalf("返回的是 %p，want %p（同一指针）：复制一份会让调用方标注的模板版本与真正渲染的那套脱钩", got, want)
	}
}
