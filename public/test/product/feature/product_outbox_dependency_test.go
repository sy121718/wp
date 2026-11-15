package feature

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/pkg/builder/core"

	"go_wp/pkg/i18n"
	"go_wp/public/schema"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

const productCollectionKey = "collection:content:product"

const productListDoc = "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}}," +
	"\"root\":[{\"id\":\"pl1\",\"type\":\"core.productList\",\"props\":{" +
	"\"collectionSource\":\"content:product\",\"collectionLimit\":12,\"layout\":\"grid\",\"columns\":\"auto\"," +
	"\"filterStatus\":\"published\",\"imageField\":\"item.images\",\"titleField\":\"item.name\"," +
	"\"priceField\":\"item.priceRange\",\"comparePriceField\":\"item.comparePrice\",\"tagsField\":\"item.tags\"," +
	"\"linkField\":\"item.url\",\"currency\":\"¥\",\"titleTag\":\"h3\",\"emptyText\":\"暂无商品\"}}]}"

type outboxFixture struct {
	db          *gorm.DB
	invalidator productcontract.DependencyInvalidator
	products    *productservice.Service
	templates   contenttemplatecontract.ContentTemplateService
	pres        *presentationservice.Service
	pages       *pageservice.Service
	projects    *projectservice.Service
	projectID   string
}

func newOutboxFixture(t *testing.T) *outboxFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "商品失效闭环测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败：%v", err)
	}

	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) "+
			"VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		project.ID, "zh-CN", project.ID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}

	registry := core.NewEntitySourceRegistry()
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetContentStore(i18n.NewDBContentStore(db))
	if err := products.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品实体类型失败: %v", err)
	}

	collections := core.NewCollectionRegistry()
	if err := collections.Register(contentservice.NewService(contentmodel.NewModel(db))); err != nil {
		t.Fatalf("注册内容集合源失败: %v", err)
	}
	if err := collections.Register(products); err != nil {
		t.Fatalf("注册商品集合源失败: %v", err)
	}

	templates := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, registry)
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pub := pubservice.NewService(pubmodel.NewPublicationModel(db))
	pres := presentationservice.NewService(presentationmodel.NewModel(db), templates, registry, projects, blocks, pub)
	pres.SetCollectionResolver(collections)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	pages := pageservice.NewService(pagemodel.NewPageModel(db), artifacts, pub, projects, blocks, nil, collections, nil, nil)

	fanout := publish.NewFanout()
	fanout.SetSyncRebuild(true)
	fanout.Register(publish.SourceTypePage, pages)
	fanout.Register(publish.SourceTypePresentation, pres)
	fanout.SetRebuilder(publish.SourceTypePage, pages)
	fanout.SetRebuilder(publish.SourceTypePresentation, pres)
	products.SetDependencyInvalidator(fanout)

	return &outboxFixture{
		db: db, products: products, templates: templates, pres: pres, pages: pages,
		projects: projects, projectID: project.ID, invalidator: fanout,
	}
}

// rewireProductChanges 用另一份审计端口重建商品服务并重新接回失效端口：
// 审计是构造依赖，故障注入只能换装配。
func (f *outboxFixture) rewireProductChanges(t *testing.T, changes masterdatacontract.MasterDataService) {
	t.Helper()
	svc := productservice.NewService(productmodel.NewModel(f.db), f.projects, changes)
	svc.SetContentStore(i18n.NewDBContentStore(f.db))
	svc.SetDependencyInvalidator(f.invalidator)
	f.products = svc
}

func (f *outboxFixture) createProduct(t *testing.T, name, slug string) string {
	t.Helper()
	ctx := context.Background()
	price := 88.0
	res, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug, DefaultPrice: &price,
		DefaultImage: "/storage/" + slug + ".jpg",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	status := "published"
	if _, err = f.products.Update(ctx, &productdto.UpdateReq{ID: res.ID, ProjectID: f.projectID, Status: &status}); err != nil {
		t.Fatalf("上架商品失败: %v", err)
	}
	return res.ID
}

func (f *outboxFixture) createCategory(t *testing.T, name, slug string) string {
	t.Helper()
	res, err := f.products.CreateCategory(context.Background(), &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("创建分类失败: %v", err)
	}
	return res.ID
}

func (f *outboxFixture) publishListPage(t *testing.T, path string) string {
	t.Helper()
	return f.publishListPageWithDoc(t, path, productListDoc)
}

func (f *outboxFixture) publishListPageWithDoc(t *testing.T, path, doc string) string {
	t.Helper()
	ctx := context.Background()
	page, err := f.pages.Create(ctx, &pagedto.CreateReq{
		ProjectID: f.projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: path, DraftDocument: []byte(doc),
	})
	if err != nil {
		t.Fatalf("创建列表页失败: %v", err)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		if _, err = f.pages.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: lang}); err != nil {
			t.Fatalf("列表页 %s 构建失败: %v", lang, err)
		}
		if _, err = f.pages.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: lang}); err != nil {
			t.Fatalf("列表页 %s 发布失败: %v", lang, err)
		}
	}
	return page.ID
}

func (f *outboxFixture) publishCollectionInstance(t *testing.T, productID, urlPath string) *presentationdto.InstanceResp {
	t.Helper()
	ctx := context.Background()
	tpl, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: productcontract.EntityTypeProduct, Name: "商品集合页", ProjectID: f.projectID,
		DraftDocument: json.RawMessage(productListDoc),
	})
	if err != nil {
		t.Fatalf("创建商品集合模板失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: productcontract.EntityTypeProduct, EntityID: productID,
		URLPath: urlPath, TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatalf("创建商品集合实例失败: %v", err)
	}
	return inst
}

func (f *outboxFixture) publishDetailInstance(t *testing.T, productID, urlPath string) *presentationdto.InstanceResp {
	t.Helper()
	inst, err := f.pres.CreateInstance(context.Background(), &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: productcontract.EntityTypeProduct, EntityID: productID, URLPath: urlPath,
	})
	if err != nil {
		t.Fatalf("创建商品详情实例失败: %v", err)
	}
	return inst
}

func (f *outboxFixture) dispatch(t *testing.T) int {
	t.Helper()
	n, err := f.products.DispatchOutbox(context.Background(), 0)
	if err != nil {
		t.Fatalf("消费 outbox 失败: %v", err)
	}
	return n
}

func pagePublicationArtifact(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var id *string
	if err := db.Raw("SELECT artifact_id FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&id).Error; err != nil {
		t.Fatalf("读取 page_publications 失败: %v", err)
	}
	if id == nil {
		return ""
	}
	return *id
}

func instancePublication(t *testing.T, db *gorm.DB, instanceID, lang string) (artifactID, activePath string) {
	t.Helper()
	var row struct {
		ArtifactID *string
		ActivePath string
	}
	if err := db.Raw("SELECT artifact_id, active_path FROM presentation_publications WHERE presentation_id = ? AND lang = ?",
		instanceID, lang).Scan(&row).Error; err != nil {
		t.Fatalf("读取 presentation_publications 失败: %v", err)
	}
	if row.ArtifactID != nil {
		artifactID = *row.ArtifactID
	}
	return artifactID, row.ActivePath
}

func activeHTMLAt(activePath string) string {
	if strings.TrimSpace(activePath) == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(publish.ActiveRoot(), strings.TrimPrefix(activePath, "/"), "index.html"))
	if err != nil {
		return ""
	}
	return string(b)
}

func pageDependencyKeys(t *testing.T, db *gorm.DB, pageID string) map[string]bool {
	t.Helper()
	var keys []string
	sql := "SELECT DISTINCT d.dependency_kind || '|' || d.dependency_key " +
		"FROM page_dependencies d JOIN pages p ON p.id = d.page_id " +
		"WHERE d.page_id = ? AND (d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) " +
		"OR d.artifact_id IN (SELECT artifact_id FROM page_publications WHERE page_id = p.id))"
	if err := db.Raw(sql, pageID).Scan(&keys).Error; err != nil {
		t.Fatalf("读取 page_dependencies 失败: %v", err)
	}
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func presentationDependencyKeys(t *testing.T, db *gorm.DB, instanceID string) map[string]bool {
	t.Helper()
	var keys []string
	if err := db.Raw("SELECT DISTINCT dependency_kind || '|' || dependency_key FROM presentation_dependencies WHERE presentation_id = ?",
		instanceID).Scan(&keys).Error; err != nil {
		t.Fatalf("读取 presentation_dependencies 失败: %v", err)
	}
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func mustArtifact(t *testing.T, db *gorm.DB, instanceID, lang string) string {
	t.Helper()
	id, _ := instancePublication(t, db, instanceID, lang)
	if id == "" {
		t.Fatalf("实例 %s 的语言 %s 没有激活产物", instanceID, lang)
	}
	return id
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func TestProductListPageRegistersCollectionDependency(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	f.createProduct(t, "列表页商品", "list-item")
	pageID := f.publishListPage(t, "/shop")

	keys := pageDependencyKeys(t, f.db, pageID)
	want := publish.DepKindContentCollection + "|" + productCollectionKey
	if !keys[want] {
		t.Fatalf("列表页产物未登记商品集合依赖 %q（改了商品不会让列表页失效）——实际依赖：%v", want, keys)
	}
}

func TestPresentationCollectionInstanceRegistersCollectionDependency(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "集合页商品", "collection-item")
	inst := f.publishCollectionInstance(t, productID, "/collections/collection-item")

	keys := presentationDependencyKeys(t, f.db, inst.ID)
	want := publish.DepKindContentCollection + "|" + productCollectionKey
	if !keys[want] {
		t.Fatalf("presentation 列表产物未登记商品集合依赖 %q（别的商品变更不会让它更新）——实际依赖：%v", want, keys)
	}
}

func TestProductChangeRebuildsDetailListAndCollectionPages(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	aID := f.createProduct(t, "商品甲原名", "item-a")
	bID := f.createProduct(t, "商品乙原名", "item-b")

	cID := f.createProduct(t, "商品丙原名", "item-c")

	detailA := f.publishDetailInstance(t, aID, "/products/item-a")
	pageID := f.publishListPage(t, "/shop")
	collectionPage := f.publishCollectionInstance(t, cID, "/collections/item-c")

	f.dispatch(t)

	nameA, nameB := "商品甲新名", "商品乙新名"
	langs := []string{"zh-CN", "en-US"}

	_, detailZH := instancePublication(t, f.db, detailA.ID, "zh-CN")
	if html := activeHTMLAt(detailZH); !strings.Contains(html, "商品甲原名") {
		t.Fatalf("改动前详情页应含旧名，实际：%s", firstLine(html))
	}
	if html := pageActiveHTML(t, f.db, pageID, "zh-CN"); !strings.Contains(html, "商品乙原名") {
		t.Fatalf("改动前列表页应含商品乙旧名，实际：%s", firstLine(html))
	}
	detailBefore := map[string]string{}
	for _, lang := range langs {
		detailBefore[lang] = mustArtifact(t, f.db, detailA.ID, lang)
	}
	collBefore := map[string]string{}
	for _, lang := range langs {
		collBefore[lang] = mustArtifact(t, f.db, collectionPage.ID, lang)
	}

	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: bID, ProjectID: f.projectID, Name: &nameB}); err != nil {
		t.Fatalf("改商品乙失败: %v", err)
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("改商品后 outbox 应有待消费事件，实际 0 条（静态产物不会更新）")
	}
	for _, lang := range langs {

		if html := pageActiveHTML(t, f.db, pageID, lang); !strings.Contains(html, nameB) {
			t.Fatalf("阶段一：列表页 %s 的线上产物仍是旧字节（不含 %q）：%s", lang, nameB, firstLine(html))
		}

		if got := mustArtifact(t, f.db, collectionPage.ID, lang); got == collBefore[lang] {
			t.Fatalf("阶段一：商品乙变更后商品丙的集合页 %s 未换代（集合键没命中）", lang)
		}
		if html := instanceActiveHTML(t, f.db, collectionPage.ID, lang); !strings.Contains(html, nameB) {
			t.Fatalf("阶段一：集合页 %s 的线上产物不含商品乙新名：%s", lang, firstLine(html))
		}

		if got := mustArtifact(t, f.db, detailA.ID, lang); got != detailBefore[lang] {
			t.Fatalf("阶段一：商品乙变更**不该**让商品甲的详情页 %s 换代（误标）", lang)
		}
	}

	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: aID, ProjectID: f.projectID, Name: &nameA}); err != nil {
		t.Fatalf("改商品甲失败: %v", err)
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("第二阶段应有待消费事件")
	}
	for _, lang := range langs {
		if got := mustArtifact(t, f.db, detailA.ID, lang); got == detailBefore[lang] {
			t.Fatalf("阶段二：商品甲变更后详情页 %s 未换代（direct_content 没命中）", lang)
		}
		if html := instanceActiveHTML(t, f.db, detailA.ID, lang); !strings.Contains(html, nameA) {
			t.Fatalf("详情页 %s 的线上产物仍是旧字节（不含 %q）：%s", lang, nameA, firstLine(html))
		}
		if html := pageActiveHTML(t, f.db, pageID, lang); !strings.Contains(html, nameA) {
			t.Fatalf("列表页 %s 的线上产物仍是旧字节（不含 %q）：%s", lang, nameA, firstLine(html))
		}
	}

	var stalePages, staleInstances int64
	if err := f.db.Raw("SELECT COUNT(*) FROM pages WHERE stale = true").Scan(&stalePages).Error; err != nil {
		t.Fatalf("统计待重建页面失败: %v", err)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM presentation_instances WHERE stale = true").Scan(&staleInstances).Error; err != nil {
		t.Fatalf("统计待重建实例失败: %v", err)
	}
	if stalePages != 0 || staleInstances != 0 {
		t.Fatalf("重建完成后 stale 应全部收敛：pages=%d instances=%d", stalePages, staleInstances)
	}
}

func pageActiveHTML(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var path string
	if err := db.Raw("SELECT active_path FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&path).Error; err != nil {
		t.Fatalf("读取列表页路径失败: %v", err)
	}
	return activeHTMLAt(path)
}

func instanceActiveHTML(t *testing.T, db *gorm.DB, instanceID, lang string) string {
	t.Helper()
	_, path := instancePublication(t, db, instanceID, lang)
	return activeHTMLAt(path)
}

const categoryArchiveDoc = `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px"}},"root":[` +
	`{"id":"pl1","type":"core.productList","props":{"collectionSource":"content:product",` +
	`"filterFromArchive":"on","collectionLimit":12,"layout":"grid","columns":"auto",` +
	`"filterStatus":"published","imageField":"item.images","titleField":"item.name",` +
	`"priceField":"item.priceRange","comparePriceField":"item.comparePrice","tagsField":"item.tags",` +
	`"linkField":"item.url","currency":"¥","titleTag":"h3","emptyText":"暂无商品"}}]}`

const categoryArchiveCardDoc = `{"settings":{"layout":{"mode":"full"}},"root":[` +
	`{"id":"pc1","type":"core.productCard","props":{"titleField":"product.images"}}]}`

func (f *outboxFixture) archiveInstanceOfCategory(t *testing.T) *presentationdto.InstanceResp {
	t.Helper()
	list, err := f.pres.List(context.Background(), &presentationdto.ListReq{
		ProjectID: f.projectID, EntityType: productcontract.EntityTypeCategory,
	})
	if err != nil {
		t.Fatalf("列出分类归档实例失败: %v", err)
	}
	for _, it := range list {
		if it.InstanceRole == presentationmodel.InstanceRoleArchive {
			return it
		}
	}
	t.Fatalf("新建分类后归档实例没建起来（归档同步失败只记日志，这里把它暴露成硬失败）")
	return nil
}

func TestCategoryArchivePageListsCategoryProducts(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	f.products.SetArchiveInstanceEnsurer(f.pres)

	if _, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		ProjectID: f.projectID, EntityType: productcontract.EntityTypeCategory,
		Name: "分类归档页", TemplateRole: "archive", DraftDocument: json.RawMessage(categoryArchiveDoc),
	}); err != nil {
		t.Fatalf("分类归档模板（core.productList 绑 product.*）应能保存: %v", err)
	}

	catA := f.createCategory(t, "电子设备", "electronics")
	inst := f.archiveInstanceOfCategory(t)
	if inst.URLPath != "/product_category/electronics" {
		t.Fatalf("归档路径应按实体类型派生（注册表口径），实际 %q", inst.URLPath)
	}

	keys := presentationDependencyKeys(t, f.db, inst.ID)
	wantKey := publish.DepKindContentCollection + "|" + productCollectionKey
	if !keys[wantKey] {
		t.Fatalf("分类归档产物未登记商品集合依赖 %q（商品增删改不会让它更新）——实际依赖：%v", wantKey, keys)
	}

	itemA := f.createProduct(t, "归档商品甲", "archive-item-a")
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: itemA, ProjectID: f.projectID, CategoryIDs: []string{catA},
	}); err != nil {
		t.Fatalf("把商品挂到分类失败: %v", err)
	}
	catB := f.createCategory(t, "服饰", "apparel")
	itemB := f.createProduct(t, "归档商品乙", "archive-item-b")
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: itemB, ProjectID: f.projectID, CategoryIDs: []string{catB},
	}); err != nil {
		t.Fatalf("把商品乙挂到分类失败: %v", err)
	}

	if n := f.dispatch(t); n == 0 {
		t.Fatalf("商品变更后 outbox 应有待消费事件，实际 0 条（归档页不会更新）")
	}

	for _, lang := range []string{"zh-CN", "en-US"} {
		html := instanceActiveHTML(t, f.db, inst.ID, lang)
		if !strings.Contains(html, "归档商品甲") {
			t.Fatalf("归档页 %s 不含该分类下的商品：%s", lang, firstLine(html))
		}
		if strings.Contains(html, "归档商品乙") {
			t.Fatalf("归档页 %s 含**别的分类**的商品（filterFromArchive 没生效，列的是全站商品）：%s",
				lang, firstLine(html))
		}
	}

	var routes int64
	if err := f.db.Raw("SELECT COUNT(*) FROM page_routes WHERE presentation_id = ? AND route_kind = 'active'",
		inst.ID).Scan(&routes).Error; err != nil {
		t.Fatalf("统计归档页路由失败: %v", err)
	}
	if routes == 0 {
		t.Fatalf("归档页没有登记访问面路由（URL 打不开）")
	}

	renamed := "归档商品甲改名"
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: itemA, ProjectID: f.projectID, Name: &renamed,
	}); err != nil {
		t.Fatalf("改商品名失败: %v", err)
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("改商品名后 outbox 应有待消费事件，实际 0 条（归档页不会更新）")
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		html := instanceActiveHTML(t, f.db, inst.ID, lang)
		if !strings.Contains(html, renamed) {
			t.Fatalf("归档页 %s 未跟上商品改名（集合键没命中）：%s", lang, firstLine(html))
		}
	}
}

func TestCategoryArchiveTemplateRejectsNonCollectionCrossSourceBinding(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	_, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		ProjectID: f.projectID, EntityType: productcontract.EntityTypeCategory,
		Name: "分类归档页（非集合组件）", TemplateRole: "archive",
		DraftDocument: json.RawMessage(categoryArchiveCardDoc),
	})
	if err == nil {
		t.Fatalf("非集合组件绑跨源字段必须被拒绝，实际保存成功")
	}
	if !strings.Contains(err.Error(), "product_category") {
		t.Fatalf("拒绝原因应说明不属于 product_category 数据源，实际：%v", err)
	}
}

type failingMasterData struct {
	masterdatacontract.MasterDataService
}

func (failingMasterData) RecordChanges(context.Context, []*masterdatacontract.ChangeInput) error {
	return errors.New("留痕端口故障（用例注入）")
}

func (failingMasterData) RecordChangesTx(context.Context, *gorm.DB, []*masterdatacontract.ChangeInput) error {
	return errors.New("留痕端口故障（用例注入）")
}

func TestProductWriteRollbackLeavesNoEvent(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	f.rewireProductChanges(t, failingMasterData{})
	price := 10.0
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "回滚商品", Slug: "rollback-item", DefaultPrice: &price,
	})
	if err == nil {
		t.Fatalf("留痕失败时商品创建应整体失败（事务回滚）")
	}
	var products, events int64
	if err := f.db.Raw("SELECT COUNT(*) FROM products WHERE slug = ?", "rollback-item").Scan(&products).Error; err != nil {
		t.Fatalf("统计商品行失败: %v", err)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM product_outbox_events").Scan(&events).Error; err != nil {
		t.Fatalf("统计 outbox 失败: %v", err)
	}
	if products != 0 {
		t.Fatalf("事务回滚后不应留下商品行，实际 %d", products)
	}
	if events != 0 {
		t.Fatalf("事务回滚后不应产生失效事件，实际 %d 条", events)
	}
}

func countArtifacts(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_artifacts WHERE page_id = ?", pageID).Scan(&n).Error; err != nil {
		t.Fatalf("统计产物行失败: %v", err)
	}
	return n
}

func TestProductOutboxDispatchIsIdempotent(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	f.createProduct(t, "幂等商品", "idem-item")
	pageID := f.publishListPage(t, "/idem-shop")

	var rows int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_outbox_events").Scan(&rows).Error; err != nil {
		t.Fatalf("统计 outbox 失败: %v", err)
	}
	if rows == 0 {
		t.Fatalf("商品创建应写入 outbox 行")
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("首次消费应处理到事件")
	}
	var pending int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_outbox_events WHERE processed_time IS NULL").Scan(&pending).Error; err != nil {
		t.Fatalf("统计未处理事件失败: %v", err)
	}
	if pending != 0 {
		t.Fatalf("消费完成后不应有未处理事件，实际 %d", pending)
	}
	if n := f.dispatch(t); n != 0 {
		t.Fatalf("已消费的事件不应被再次领取，实际处理 %d 条", n)
	}

	beforeArtifacts := countArtifacts(t, f.db, pageID)
	var lastID int64
	if err := f.db.Raw("SELECT MAX(id) FROM product_outbox_events").Scan(&lastID).Error; err != nil {
		t.Fatalf("取事件 id 失败: %v", err)
	}
	if err := f.db.Exec("UPDATE product_outbox_events SET processed_time = NULL, claimed_time = NULL WHERE id = ?", lastID).Error; err != nil {
		t.Fatalf("重置事件状态失败: %v", err)
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("重置后的事件应被重新领取（可重放）")
	}
	if after := countArtifacts(t, f.db, pageID); after != beforeArtifacts {
		t.Fatalf("重放不应产生新产物行（%d → %d）", beforeArtifacts, after)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM product_outbox_events WHERE processed_time IS NULL").Scan(&pending).Error; err != nil {
		t.Fatalf("统计未处理事件失败: %v", err)
	}
	if pending != 0 {
		t.Fatalf("重放后不应残留未处理事件，实际 %d", pending)
	}
}

const attributeValuesDoc = "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}}," +
	"\"root\":[{\"id\":\"h1\",\"type\":\"core.heading\",\"props\":{" +
	"\"binding\":{\"field\":\"product_attribute.values\"},\"tag\":\"h2\"}}]}"

func TestAttributeChangeRebuildsBoundAttributePage(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	attr, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
		Values: []productdto.AttributeValueReq{{Key: "red", Label: "旧红"}},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	tpl, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: productcontract.EntityTypeAttribute, Name: "属性展示页", ProjectID: f.projectID,
		DraftDocument: json.RawMessage(attributeValuesDoc),
	})
	if err != nil {
		t.Fatalf("创建属性模板失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: productcontract.EntityTypeAttribute, EntityID: attr.ID,
		URLPath: "/attributes/color", TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatalf("创建属性实例失败: %v", err)
	}
	f.dispatch(t)

	if html := instanceActiveHTML(t, f.db, inst.ID, "zh-CN"); !strings.Contains(html, "旧红") {
		t.Fatalf("基线属性页应含属性值旧标签「旧红」：%s", firstLine(html))
	}

	if _, err = f.products.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
		ID: attr.ID, ProjectID: f.projectID,
		Values: []productdto.AttributeValueReq{{Key: "red", Label: "新红"}},
	}); err != nil {
		t.Fatalf("保存属性值失败: %v", err)
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("属性变更后 outbox 应有待消费事件（否则属性页停在旧值）")
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		html := instanceActiveHTML(t, f.db, inst.ID, lang)
		if strings.Contains(html, "旧红") || !strings.Contains(html, "新红") {
			t.Fatalf("属性页 %s 的线上产物仍是旧值（不含「新红」）：%s", lang, firstLine(html))
		}
	}
}

const productTagsDoc = "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}}," +
	"\"root\":[{\"id\":\"p1\",\"type\":\"core.product\",\"props\":{" +
	"\"source\":\"product\",\"titleField\":\"product.name\",\"subtitleField\":\"product.tags\",\"titleTag\":\"h2\"}}]}"

func TestTagRecalcRebuildsBoundProductDetailPage(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	xID := f.createProduct(t, "标签商品", "tag-item")
	tpl, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: productcontract.EntityTypeProduct, Name: "标签详情页", ProjectID: f.projectID,
		DraftDocument: json.RawMessage(productTagsDoc),
	})
	if err != nil {
		t.Fatalf("创建标签详情模板失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: productcontract.EntityTypeProduct, EntityID: xID,
		URLPath: "/products/tag-item", TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatalf("创建详情实例失败: %v", err)
	}

	tagName := "高价精选"
	tag, err := f.products.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: tagName, Slug: "premium",
		Kind: productenums.TagKindRule, RuleType: productenums.TagRulePriceRange,
		RuleParams: json.RawMessage(`{"minPrice":1000,"maxPrice":2000}`),
	})
	if err != nil {
		t.Fatalf("创建规则标签失败: %v", err)
	}

	f.dispatch(t)
	if html := instanceActiveHTML(t, f.db, inst.ID, "zh-CN"); strings.Contains(html, tagName) {
		t.Fatalf("基线详情页不该含未命中的标签名：%s", firstLine(html))
	}

	ruleType := productenums.TagRulePriceRange
	if _, err = f.products.UpdateTag(ctx, &productdto.UpdateTagReq{
		ID: tag.ID, ProjectID: f.projectID, RuleType: &ruleType,
		RuleParams: json.RawMessage(`{"minPrice":50,"maxPrice":150}`),
	}); err != nil {
		t.Fatalf("修改规则失败: %v", err)
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("规则变更后 outbox 应有待消费事件")
	}

	rows, err := productmodel.NewModel(f.db).ListOutboxEvents(ctx, productcontract.EntityTypeProduct, xID)
	if err != nil {
		t.Fatalf("读取 outbox 失败: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.DependencyKind == publish.DepKindDirectContent && row.DependencyKey == "product:"+xID {
			found = true
		}
	}
	if !found {
		t.Fatalf("归属重算应为命中变化过的商品写 direct_content:product:%s（否则详情页不更新）", xID)
	}

	for _, lang := range []string{"zh-CN", "en-US"} {
		html := instanceActiveHTML(t, f.db, inst.ID, lang)
		if !strings.Contains(html, tagName) {
			t.Fatalf("详情页 %s 未随归属重算更新（不含 %q）：%s", lang, tagName, firstLine(html))
		}
	}
}

func renameFieldDoc(field string) string {
	return "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}},\"root\":" +
		"[{\"id\":\"p1\",\"type\":\"core.product\",\"props\":{\"source\":\"product\"," +
		"\"titleField\":\"product.name\",\"subtitleField\":\"" + field + "\",\"titleTag\":\"h2\"}}]}"
}

func TestEntityRenameRebuildsReferencingProductPages(t *testing.T) {
	type renameCase struct {
		name       string
		entityType string

		field string

		oldText string
		newText string

		create func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq))

		change func(t *testing.T, f *outboxFixture, entityID, newText string)
	}
	cases := []renameCase{
		{
			name: "分类", entityType: productcontract.EntityTypeCategory, field: "product.related",
			oldText: "旧分类名", newText: "新分类名",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				cat, err := f.products.CreateCategory(context.Background(), &productdto.CreateCategoryReq{
					ProjectID: f.projectID, Name: "旧分类名", Slug: slug,
				})
				if err != nil {
					t.Fatalf("创建分类失败: %v", err)
				}
				return cat.ID, func(req *productdto.CreateReq) { req.CategoryIDs = []string{cat.ID} }
			},
			change: func(t *testing.T, f *outboxFixture, entityID, newText string) {
				if _, err := f.products.UpdateCategory(context.Background(), &productdto.UpdateCategoryReq{
					ID: entityID, ProjectID: f.projectID, Name: &newText,
				}); err != nil {
					t.Fatalf("改分类名失败: %v", err)
				}
			},
		},
		{
			name: "品牌", entityType: productcontract.EntityTypeBrand, field: "product.related",
			oldText: "旧品牌名", newText: "新品牌名",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				brand, err := f.products.CreateBrand(context.Background(), &productdto.CreateBrandReq{
					ProjectID: f.projectID, Name: "旧品牌名", Slug: slug,
				})
				if err != nil {
					t.Fatalf("创建品牌失败: %v", err)
				}
				return brand.ID, func(req *productdto.CreateReq) { req.BrandID = brand.ID }
			},
			change: func(t *testing.T, f *outboxFixture, entityID, newText string) {
				if _, err := f.products.UpdateBrand(context.Background(), &productdto.UpdateBrandReq{
					ID: entityID, ProjectID: f.projectID, Name: &newText,
				}); err != nil {
					t.Fatalf("改品牌名失败: %v", err)
				}
			},
		},
		{
			name: "标签", entityType: productcontract.EntityTypeTag, field: "product.tags",
			oldText: "旧标签名", newText: "新标签名",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				tag, err := f.products.CreateTag(context.Background(), &productdto.CreateTagReq{
					ProjectID: f.projectID, Name: "旧标签名", Slug: slug, Kind: productenums.TagKindManual,
				})
				if err != nil {
					t.Fatalf("创建标签失败: %v", err)
				}
				return tag.ID, func(req *productdto.CreateReq) { req.TagIDs = []string{tag.ID} }
			},
			change: func(t *testing.T, f *outboxFixture, entityID, newText string) {
				if _, err := f.products.UpdateTag(context.Background(), &productdto.UpdateTagReq{
					ID: entityID, ProjectID: f.projectID, Name: &newText,
				}); err != nil {
					t.Fatalf("改标签名失败: %v", err)
				}
			},
		},
		{
			name: "属性组", entityType: productcontract.EntityTypeAttribute, field: "product.options",
			oldText: "旧属性组名", newText: "新属性组名",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				attr, err := f.products.CreateAttribute(context.Background(), &productdto.CreateAttributeReq{
					ProjectID: f.projectID, Name: "旧属性组名", Key: slug,
					Values: []productdto.AttributeValueReq{{Key: "v1", Label: "规格一"}},
				})
				if err != nil {
					t.Fatalf("创建属性组失败: %v", err)
				}
				return attr.ID, func(req *productdto.CreateReq) { req.AttributeIDs = []string{attr.ID} }
			},
			change: func(t *testing.T, f *outboxFixture, entityID, newText string) {
				if _, err := f.products.UpdateAttribute(context.Background(), &productdto.UpdateAttributeReq{
					ID: entityID, ProjectID: f.projectID, Name: &newText,
				}); err != nil {
					t.Fatalf("改属性组名失败: %v", err)
				}
			},
		},
		{

			name: "属性值", entityType: productcontract.EntityTypeAttribute, field: "product.options",
			oldText: "规格一", newText: "规格二",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				attr, err := f.products.CreateAttribute(context.Background(), &productdto.CreateAttributeReq{
					ProjectID: f.projectID, Name: "规格组", Key: slug,
					Values: []productdto.AttributeValueReq{{Key: "v1", Label: "规格一"}},
				})
				if err != nil {
					t.Fatalf("创建属性组失败: %v", err)
				}
				return attr.ID, func(req *productdto.CreateReq) { req.AttributeIDs = []string{attr.ID} }
			},
			change: func(t *testing.T, f *outboxFixture, entityID, newText string) {
				if _, err := f.products.SetAttributeValues(context.Background(), &productdto.SetAttributeValuesReq{
					ID: entityID, ProjectID: f.projectID,
					Values: []productdto.AttributeValueReq{{Key: "v1", Label: newText}},
				}); err != nil {
					t.Fatalf("保存属性值失败: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOutboxFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			entityID, attach := tc.create(t, f, "rename-"+tc.name)
			price := 42.0
			req := &productdto.CreateReq{
				ProjectID: f.projectID, Name: "改名目标商品", Slug: "rename-item",
				DefaultPrice: &price,
			}
			attach(req)
			product, err := f.products.Create(ctx, req)
			if err != nil {
				t.Fatalf("创建商品失败: %v", err)
			}
			if tc.entityType == productcontract.EntityTypeAttribute {

				if len(product.Variants) != 1 || len(optionMap(t, product.Variants[0])) != 0 {
					t.Fatalf("属性测试前置：首变体应为空规格占位，实际 %+v", product.Variants)
				}
				generated, gerr := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: product.ID})
				if gerr != nil {
					t.Fatalf("生成属性组合失败: %v", gerr)
				}
				if len(generated.Variants) != 1 || len(optionMap(t, generated.Variants[0])) != 1 {
					t.Fatalf("属性测试前置：变体应持有该组的真实选项值，实际 %+v", generated.Variants)
				}
			}
			tpl, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
				EntityType: productcontract.EntityTypeProduct, Name: "改名详情页", ProjectID: f.projectID,
				DraftDocument: json.RawMessage(renameFieldDoc(tc.field)),
			})
			if err != nil {
				t.Fatalf("创建详情模板失败: %v", err)
			}
			inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
				ProjectID: f.projectID, EntityType: productcontract.EntityTypeProduct, EntityID: product.ID,
				URLPath: "/products/rename-item", TemplateID: tpl.ID,
			})
			if err != nil {
				t.Fatalf("创建详情实例失败: %v", err)
			}
			f.dispatch(t)

			if html := instanceActiveHTML(t, f.db, inst.ID, "zh-CN"); !strings.Contains(html, tc.oldText) {
				t.Fatalf("%s：基线详情页应含「%s」：%s", tc.name, tc.oldText, firstLine(html))
			}

			key := "product:" + product.ID
			rowsBefore, lerr := productmodel.NewModel(f.db).ListOutboxEvents(ctx, productcontract.EntityTypeProduct, product.ID)
			if lerr != nil {
				t.Fatalf("读取 outbox 失败: %v", lerr)
			}
			beforeHits := 0
			for _, row := range rowsBefore {
				if row.DependencyKey == key {
					beforeHits++
				}
			}
			tc.change(t, f, entityID, tc.newText)
			if n := f.dispatch(t); n == 0 {
				t.Fatalf("%s 变更后 outbox 应有待消费事件", tc.name)
			}

			rows, lerr := productmodel.NewModel(f.db).ListOutboxEvents(ctx, productcontract.EntityTypeProduct, product.ID)
			if lerr != nil {
				t.Fatalf("读取 outbox 失败: %v", lerr)
			}
			afterHits := 0
			for _, row := range rows {
				if row.DependencyKey == key {
					afterHits++
				}
			}
			if afterHits <= beforeHits {
				t.Fatalf("%s 变更应为引用它的商品新增 direct_content:%s（变更前 %d 条，变更后 %d 条）",
					tc.name, key, beforeHits, afterHits)
			}

			for _, lang := range []string{"zh-CN", "en-US"} {
				html := instanceActiveHTML(t, f.db, inst.ID, lang)
				if !strings.Contains(html, tc.newText) {
					t.Fatalf("%s 变更后详情页 %s 未更新（不含 %q）：%s", tc.name, lang, tc.newText, firstLine(html))
				}
			}
		})
	}
}
