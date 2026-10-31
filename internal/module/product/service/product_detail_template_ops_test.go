package productservice_test

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
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

func detailTemplateJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("构造用例 JSON 失败：%v", err)
	}
	return b
}

func detailTemplateCopySourceDoc(t *testing.T) json.RawMessage {
	t.Helper()
	return detailTemplateJSON(t, map[string]any{"settings": map[string]any{}, "root": []any{}})
}

type fakeDetailTemplates struct {
	contenttemplatecontract.ContentTemplateService

	resolveEntityTypes []string

	resolveScopedProjects    []string
	resolveScopedEntityTypes []string

	resolveErr        error
	resolveTemplateID string

	getProjects []string
	getIDs      []string
	getErr      error
	getDoc      json.RawMessage
	getNilResp  bool

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

func (f *fakeDetailTemplates) ResolveTemplateScoped(_ context.Context, projectID, entityType string) (*contenttemplatecontract.ResolvedTemplate, error) {
	f.resolveScopedProjects = append(f.resolveScopedProjects, projectID)
	f.resolveScopedEntityTypes = append(f.resolveScopedEntityTypes, entityType)
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &contenttemplatecontract.ResolvedTemplate{TemplateID: f.resolveTemplateID, EntityType: entityType}, nil
}

func (f *fakeDetailTemplates) GetScoped(_ context.Context, projectID, id string) (*contenttemplatecontract.TemplateResp, error) {
	f.getProjects = append(f.getProjects, projectID)
	f.getIDs = append(f.getIDs, id)
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getNilResp {
		return nil, nil
	}
	return &contenttemplatecontract.TemplateResp{ID: id, DraftDocument: f.getDoc}, nil
}

func (f *fakeDetailTemplates) Create(_ context.Context, req *contenttemplatecontract.CreateReq) (*contenttemplatecontract.TemplateResp, error) {
	f.createReqs = append(f.createReqs, req)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &contenttemplatecontract.TemplateResp{ID: "tpl-created"}, nil
}

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

type fakeDetailProducts struct {
	t   *testing.T
	get func(*productdto.GetReq) (*productdto.ProductResp, error)

	getReqs []*productdto.GetReq
}

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

	if len(templates.resolveEntityTypes) != 0 {
		t.Fatalf("无工程的 ResolveTemplate 被调用 %v：多工程部署下它解析不出工程（ErrProjectRequired），"+
			"缺省来源必须走 ResolveTemplateScoped", templates.resolveEntityTypes)
	}

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
	if len(templates.getProjects) != 1 || templates.getProjects[0] != detailTemplateTestProjectID {
		t.Fatalf("指定模板读取必须保持工程作用域：%v", templates.getProjects)
	}
	if len(templates.getIDs) != 1 || templates.getIDs[0] != "tpl-explicit" {
		t.Fatalf("Get 收到的 id = %v，want [tpl-explicit]（前后空白应被 TrimSpace 掉）", templates.getIDs)
	}
}

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

func TestCreateDetailTemplatePropagatesCreateFailure(t *testing.T) {
	templates := &fakeDetailTemplates{getDoc: detailTemplateCopySourceDoc(t), createErr: errDetailTemplateCreateTpl}

	err := productservice.CreateDetailTemplate(context.Background(),
		detailTemplateDeps(templates, &fakeDetailInstances{}, nil, nil),
		detailTemplateTestProjectID, "详情页模板 C", detailTemplateTestTemplateID)
	if !errors.Is(err, errDetailTemplateCreateTpl) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplateCreateTpl)
	}
}

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

func TestPublishDetailTemplateExplicitURLPathSkipsProductLookup(t *testing.T) {
	ctx := context.Background()
	instances := &fakeDetailInstances{}
	products := &fakeDetailProducts{t: t}
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
	if !errors.Is(err, errDetailTemplateProductRead) {
		t.Fatalf("未保留商品读取错误：%v", err)
	}
	if len(instances.createReqs) != 0 {
		t.Fatalf("取商品失败却建了 %d 个实例：应当直接返回，不继续建实例", len(instances.createReqs))
	}
	if len(products.getReqs) != 1 || products.getReqs[0].ID != detailTemplateTestProductID {
		t.Fatalf("Products.Get 收到的 id = %v，want [%s]", products.getReqs, detailTemplateTestProductID)
	}
}

func TestPublishDetailTemplateDerivesURLPathFromProjectRules(t *testing.T) {
	ctx := context.Background()
	const slug = "  tee-shirt  "
	projects := &fakeDetailProjects{settings: detailTemplateJSON(t, map[string]any{
		"urlPatterns": map[string]string{siteurl.KindProduct: "/shop/{slug}-{id}"},
	})}
	products := &fakeDetailProducts{t: t, get: func(*productdto.GetReq) (*productdto.ProductResp, error) {
		return &productdto.ProductResp{ID: detailTemplateTestProductID, Slug: slug}, nil
	}}

	want := siteurl.DetailPathForProject(ctx, projects, detailTemplateTestProjectID,
		siteurl.KindProduct, slug, detailTemplateTestProductID)

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

func TestPublishDetailTemplateCreateInstanceFailureIsNotProductLookup(t *testing.T) {
	ctx := context.Background()
	instances := &fakeDetailInstances{createErr: errDetailTemplateCreateInst}

	err := productservice.PublishDetailTemplate(ctx,
		detailTemplateDeps(nil, instances, &fakeDetailProducts{t: t}, &fakeDetailProjects{}),
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID, "/explicit/path")
	if !errors.Is(err, errDetailTemplateCreateInst) {
		t.Fatalf("err = %v，want %v（建实例失败应原样冒泡）", err, errDetailTemplateCreateInst)
	}
	if len(instances.createReqs) != 1 {
		t.Fatalf("CreateInstance 被调用 %d 次，want 1", len(instances.createReqs))
	}
}

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

func TestApplyDetailTemplatePropagatesFailure(t *testing.T) {
	instances := &fakeDetailInstances{rebuildErr: errDetailTemplateRebuild}

	err := productservice.ApplyDetailTemplate(context.Background(), instances,
		detailTemplateTestProjectID, detailTemplateTestProductID, detailTemplateTestTemplateID)
	if !errors.Is(err, errDetailTemplateRebuild) {
		t.Fatalf("err = %v，want %v", err, errDetailTemplateRebuild)
	}
}

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
