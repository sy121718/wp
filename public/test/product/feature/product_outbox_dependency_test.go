// product_outbox_dependency_test.go — 商品写路径的静态产物失效闭环（审计 ARCH-01）。
//
// 链路（改动后的形状）：
//
//	商品写事务内写 outbox 行（product_outbox_events：project / entity / revision / 依赖键）
//	  → 消费者（product.Service.DispatchOutbox）领取
//	  → 窄端口 productcontract.DependencyInvalidator（装配层注入 pipeline.Fanout）
//	  → page / presentation 的 MarkStaleByDependency（按 page_dependencies /
//	    presentation_dependencies 反查）
//	  → RebuildStale（本用例内联执行）→ 全部语言的产物更新
//
// 三段验收对应：
//  1. TestProductListPageRegistersCollectionDependency —— 列表页（内置 core.productList）
//     的依赖表里必须**真有**集合键（collection:content:product）。改动前这里是空的
//     （page 的 collectionSourcesOf 只认插件 manifest），集合扇出因此永远空转 ——
//     这是本审计实测出来的第一条洞，也是「依赖键根本表达不了集合成员变化」的准确落点；
//  2. TestArchiveInstanceRegistersCollectionDependency —— presentation 侧（归档页）同理；
//  3. TestProductChangeRebuildsDetailListAndArchive —— 端到端：改商品名后详情页 /
//     列表页 / 归档页在 zh-CN 与 en-US 两种语言上都重新发布；
//  4. TestProductWriteRollbackLeavesNoEvent —— 事务回滚 → 不产生事件；
//  5. TestProductOutboxDispatchIsIdempotent —— 消费者可重放且幂等（重放不产生新产物）。
package feature

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
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

	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// productCollectionKey 商品集合依赖键（与 content_service.go 的发射端逐字一致）。
const productCollectionKey = "collection:content:product"

// productListDoc 含内置 core.productList 的文档（列表页与归档模板共用）。
const productListDoc = "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}}," +
	"\"root\":[{\"id\":\"pl1\",\"type\":\"core.productList\",\"props\":{" +
	"\"collectionSource\":\"content:product\",\"collectionLimit\":12,\"layout\":\"grid\",\"columns\":\"auto\"," +
	"\"filterStatus\":\"published\",\"imageField\":\"item.images\",\"titleField\":\"item.name\"," +
	"\"priceField\":\"item.priceRange\",\"comparePriceField\":\"item.comparePrice\",\"tagsField\":\"item.tags\"," +
	"\"linkField\":\"item.url\",\"currency\":\"¥\",\"titleTag\":\"h3\",\"emptyText\":\"暂无商品\"}}]}"

// outboxFixture 一套真实装配：商品 + 内容模板 + 自动发布 + 手工 Page + 依赖扇出。
type outboxFixture struct {
	db        *gorm.DB
	products  *productservice.Service
	templates contenttemplatecontract.ContentTemplateService
	pres      *presentationservice.Service
	pages     *pageservice.Service
	projects  *projectservice.Service
	projectID string
}

// newOutboxFixture 装配 fixture；PG 不可用时 t.Skip（返回 nil）。
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
	// 默认商品详情模板来自种子（迁移 085），必须在工程创建之后跑。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	// 两种语言：验收要求「全部语言最终更新」。
	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) "+
			"VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		project.ID, "zh-CN", project.ID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}

	registry := core.NewEntitySourceRegistry()
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetContentStore(i18n.NewDBContentStore(db))
	if err := products.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品实体类型失败: %v", err)
	}
	// 集合源注册表（与线上装配同形）。
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

	// 依赖扇出：与线上同一份内核 + 内联重建（同步，便于断言）。
	fanout := pipeline.NewFanout()
	fanout.SetSyncRebuild(true)
	fanout.Register(pipeline.SourceTypePage, pages)
	fanout.Register(pipeline.SourceTypePresentation, pres)
	fanout.SetRebuilder(pipeline.SourceTypePage, pages)
	fanout.SetRebuilder(pipeline.SourceTypePresentation, pres)
	products.SetDependencyInvalidator(fanout)

	return &outboxFixture{
		db: db, products: products, templates: templates, pres: pres, pages: pages,
		projects: projects, projectID: project.ID,
	}
}

// createProduct 建商品并上架（列表页默认只出已上架商品）。
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

// createCategory 建一个分类（归档页实例的实体）。
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

// publishListPage 建一个用内置 core.productList 的列表页（默认文档）并逐语言构建 + 发布。
func (f *outboxFixture) publishListPage(t *testing.T, path string) string {
	t.Helper()
	return f.publishListPageWithDoc(t, path, productListDoc)
}

// publishListPageWithDoc 同上，但用调用方给的页面文档。
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

// publishCollectionInstance 用一套「商品详情 + 商品集合」模板建实例（presentation 侧的列表产物）。
//
// 为什么不用**分类归档页**来验 presentation 侧的集合键：product_category 类型的模板
// 不允许出现商品集合组件 —— contenttemplate 的 Create 走 builder.ValidateFieldRefs，
// 其中「ref.EntityType != entityType 即拒绝」（internal/builder/field_binding.go:90），
// 而 core.productList / core.cardstack 的字段绑定实体类型恒为 product。
// 实测报错：ErrFieldBindingInvalid: 字段绑定 product.images 不属于 product_category 数据源
// （跨数据源绑定被拒绝）。分类归档页因此**今天在架构上列不出商品**，这是本审计的独立发现，
// 立案后需在 builder.ValidateFieldRefs / 组件的 FieldRefs 上决定集合组件如何跨源。
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

// publishDetailInstance 建商品详情实例（每种语言一个产物）。
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

// dispatch 消费一次 outbox（返回处理条数）。
func (f *outboxFixture) dispatch(t *testing.T) int {
	t.Helper()
	n, err := f.products.DispatchOutbox(context.Background(), 0)
	if err != nil {
		t.Fatalf("消费 outbox 失败: %v", err)
	}
	return n
}

// pagePublicationArtifact 读取页面某语言的激活产物 id。
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

// instancePublication 读取实例某语言的激活产物 id 与线上路径。
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

// activeHTMLAt 读访问面上某路径的激活 index.html（访客面等价读法）。
func activeHTMLAt(activePath string) string {
	if strings.TrimSpace(activePath) == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(pipeline.ActiveRoot(), strings.TrimPrefix(activePath, "/"), "index.html"))
	if err != nil {
		return ""
	}
	return string(b)
}

// pageDependencyKeys 读取列表页当前产物的依赖键集合（kind|key）。
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

// presentationDependencyKeys 读取实例产物的依赖键集合（kind|key）。
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

// mustArtifact 读取 presentation 侧某语言的激活产物 id（缺行即失败）。
func mustArtifact(t *testing.T, db *gorm.DB, instanceID, lang string) string {
	t.Helper()
	id, _ := instancePublication(t, db, instanceID, lang)
	if id == "" {
		t.Fatalf("实例 %s 的语言 %s 没有激活产物", instanceID, lang)
	}
	return id
}

// firstLine 截取首部片段（失败信息用，避免把整页 HTML 铺进日志）。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// TestProductListPageRegistersCollectionDependency 列表页（内置 core.productList）的
// 产物必须登记 content_collection:collection:content:product —— 缺它时集合键扇出永远空转。
func TestProductListPageRegistersCollectionDependency(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	f.createProduct(t, "列表页商品", "list-item")
	pageID := f.publishListPage(t, "/shop")

	keys := pageDependencyKeys(t, f.db, pageID)
	want := pipeline.DepKindContentCollection + "|" + productCollectionKey
	if !keys[want] {
		t.Fatalf("列表页产物未登记商品集合依赖 %q（改了商品不会让列表页失效）——实际依赖：%v", want, keys)
	}
}

// TestPresentationCollectionInstanceRegistersCollectionDependency presentation 侧的列表产物
// 同样要登记集合键：它的 direct_content 只认自己那个实体，集合里**别的**商品变更全都不命中。
func TestPresentationCollectionInstanceRegistersCollectionDependency(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "集合页商品", "collection-item")
	inst := f.publishCollectionInstance(t, productID, "/collections/collection-item")

	keys := presentationDependencyKeys(t, f.db, inst.ID)
	want := pipeline.DepKindContentCollection + "|" + productCollectionKey
	if !keys[want] {
		t.Fatalf("presentation 列表产物未登记商品集合依赖 %q（别的商品变更不会让它更新）——实际依赖：%v", want, keys)
	}
}

// TestProductChangeRebuildsDetailListAndCollectionPages 端到端。两阶段：
//
//	阶段一：改**另一个**商品 B 的名字 —— 商品 A 的详情页不该动（direct_content 不命中 A），
//	        但列表页与 A 的集合页必须更新（集合键命中：列表渲染的是整个集合）。
//	        这一段是「集合成员 / 成员可见字段变化」这条失效路径的**独立证据**。
//	阶段二：改商品 A 自己 —— 详情页也跟上。
//
// 判据一律取**访客可见的字节**（访问面上各语言的激活 HTML）与 stale 收敛，
// 不依赖产物行 id 的换代语义：列表页的重建路径会复用同一产物行（实测），
// 用 id 当判据会把「字节已经更新」误判成「没重建」。
func TestProductChangeRebuildsDetailListAndCollectionPages(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	aID := f.createProduct(t, "商品甲原名", "item-a")
	bID := f.createProduct(t, "商品乙原名", "item-b")
	// 集合页实例绑定的实体必须与详情页实例不同：CreateInstance 对同一实体是**幂等**的
	//（同一 (entity_type, entity_id) 只会有一个实例），拿同一个商品建两次实例会拿到同一个实例。
	cID := f.createProduct(t, "商品丙原名", "item-c")

	detailA := f.publishDetailInstance(t, aID, "/products/item-a")
	pageID := f.publishListPage(t, "/shop")
	collectionPage := f.publishCollectionInstance(t, cID, "/collections/item-c")

	// 先把 fixture 装配过程产生的所有事件消费干净（建商品本身也会发集合键）。
	f.dispatch(t)

	nameA, nameB := "商品甲新名", "商品乙新名"
	langs := []string{"zh-CN", "en-US"}

	// 改动前：详情页与列表页的线上字节都是旧名字。
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

	// —— 阶段一：改 B ——
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: bID, ProjectID: f.projectID, Name: &nameB}); err != nil {
		t.Fatalf("改商品乙失败: %v", err)
	}
	if n := f.dispatch(t); n == 0 {
		t.Fatalf("改商品后 outbox 应有待消费事件，实际 0 条（静态产物不会更新）")
	}
	for _, lang := range langs {
		// 列表页（手工 Page）：两条语言都必须换成新字节。
		if html := pageActiveHTML(t, f.db, pageID, lang); !strings.Contains(html, nameB) {
			t.Fatalf("阶段一：列表页 %s 的线上产物仍是旧字节（不含 %q）：%s", lang, nameB, firstLine(html))
		}
		// presentation 侧的集合页：产物换代（集合键命中，与它自己的 direct_content 无关）。
		if got := mustArtifact(t, f.db, collectionPage.ID, lang); got == collBefore[lang] {
			t.Fatalf("阶段一：商品乙变更后商品丙的集合页 %s 未换代（集合键没命中）", lang)
		}
		if html := instanceActiveHTML(t, f.db, collectionPage.ID, lang); !strings.Contains(html, nameB) {
			t.Fatalf("阶段一：集合页 %s 的线上产物不含商品乙新名：%s", lang, firstLine(html))
		}
		// 商品甲的详情页不该被这次变更动到（direct_content 精确性）。
		if got := mustArtifact(t, f.db, detailA.ID, lang); got != detailBefore[lang] {
			t.Fatalf("阶段一：商品乙变更**不该**让商品甲的详情页 %s 换代（误标）", lang)
		}
	}

	// —— 阶段二：改 A 自己 ——
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
	// 收尾：stale 已收敛（重建成功把标记清掉）。
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

// pageActiveHTML 读页面某语言的线上字节。
func pageActiveHTML(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var path string
	if err := db.Raw("SELECT active_path FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&path).Error; err != nil {
		t.Fatalf("读取列表页路径失败: %v", err)
	}
	return activeHTMLAt(path)
}

// instanceActiveHTML 读实例某语言的线上字节。
func instanceActiveHTML(t *testing.T, db *gorm.DB, instanceID, lang string) string {
	t.Helper()
	_, path := instancePublication(t, db, instanceID, lang)
	return activeHTMLAt(path)
}

// TestCategoryArchiveTemplateCannotBindProductFields 记录一条**今天在架构上做不到**的验收：
// 验收要求「presentation 侧的分类归档页随商品集合变化更新」，但分类归档页的模板
// （entity_type = product_category）不允许出现商品集合组件 —— contenttemplate 的 Create
// 走 builder.ValidateFieldRefs，跨数据源绑定被硬拒（internal/builder/field_binding.go:90），
// 而 core.productList / core.cardstack 的字段绑定实体类型恒为 product。
//
// 本用例把这条限制**钉住**（而不是假装它不存在）：它红了说明有人放开了跨源绑定 ——
// 那时应当把 TestPresentationCollectionInstanceRegistersCollectionDependency 扩到
// 分类归档页，并删掉本用例。
func TestCategoryArchiveTemplateCannotBindProductFields(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	_, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: productcontract.EntityTypeCategory, Name: "分类归档页", ProjectID: f.projectID,
		TemplateRole: "archive", DraftDocument: json.RawMessage(productListDoc),
	})
	if err == nil {
		t.Fatalf("分类归档模板今天不应能绑定商品集合组件的字段；若这里通过，说明跨源绑定已放开，请把归档页纳入集合失效用例")
	}
	if !strings.Contains(err.Error(), "product_category") {
		t.Fatalf("拒绝原因应是跨数据源绑定（涉及 product_category），实际：%v", err)
	}
}

// failingMasterData 留痕端口故障：用来在商品写事务的**后续步骤**制造失败，
// 以证明「事务回滚 → 不产生事件」。
type failingMasterData struct {
	masterdatacontract.MasterDataService
}

func (failingMasterData) RecordChanges(context.Context, []*masterdatacontract.ChangeInput) error {
	return errors.New("留痕端口故障（用例注入）")
}

func (failingMasterData) RecordChangesTx(context.Context, *gorm.DB, []*masterdatacontract.ChangeInput) error {
	return errors.New("留痕端口故障（用例注入）")
}

// TestProductWriteRollbackLeavesNoEvent 事务回滚 → 不产生失效事件，也不留半截商品行。
func TestProductWriteRollbackLeavesNoEvent(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	f.products.SetMasterDataChanges(failingMasterData{})
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

// countArtifacts 统计页面产物行数（重放幂等的判据）。
func countArtifacts(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_artifacts WHERE page_id = ?", pageID).Scan(&n).Error; err != nil {
		t.Fatalf("统计产物行失败: %v", err)
	}
	return n
}

// TestProductOutboxDispatchIsIdempotent 消费者崩溃可重放且幂等：
// 已消费的事件不重复消费；重放（重置 processed/claimed）不产生新产物。
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

	// 崩溃重放：把最新一行恢复成待领取（模拟消费者在标记完成前崩溃）。
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

// —— 审计 ARCH-01 尾巴：后补的两个写入口 ——
//
// 1) 商品属性（product_attributes）的增改删；
// 2) 自动标签归属重算里的**逐商品** direct_content。
// 两条都用「摘掉该处入队 → 目标产物不更新（红）」的方式验过（见报告）。

// attributeValuesDoc 只渲染属性组值文本的模板：core.heading 的字段绑定**不限定前缀**
// （heading.go 的 ct:"bindingfield"），因此可以绑定 product_attribute.values。
// 该实例只登记 direct_content:product_attribute:{id}（没有集合组件），
// 于是「属性实体键是否发出」不会被商品集合键掩盖。
const attributeValuesDoc = "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}}," +
	"\"root\":[{\"id\":\"h1\",\"type\":\"core.heading\",\"props\":{" +
	"\"binding\":{\"field\":\"product_attribute.values\"},\"tag\":\"h2\"}}]}"

// TestAttributeChangeRebuildsBoundAttributePage 属性组变更 → 绑定该属性组的产物必须重建。
// 失败能力的落点：SetAttributeValues 里的 enqueueAttributeInvalidation 摘掉即红。
//
// 已知边界（不在本票扩，见报告）：**引用该属性组的商品详情页**不在本用例覆盖内 ——
// 商品详情页登记的是 direct_content:product:{id}，只发属性实体键命中不到它；
// 要覆盖得按 products.attribute_ids 反查逐商品发键（与标签重算同一手法）。
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

	// 基线：产物里是旧属性值标签（且装配期事件已消费干净）。
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

// productTagsDoc 只渲染「商品名 + 商品标签」的详情模板：**不含集合组件**，
// 因此该实例只登记 direct_content:product:{id}，不登记商品集合键 ——
// 这样「逐商品键是否发出」与「集合键是否发出」不会互相掩盖。
const productTagsDoc = "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}}," +
	"\"root\":[{\"id\":\"p1\",\"type\":\"core.product\",\"props\":{" +
	"\"source\":\"product\",\"titleField\":\"product.name\",\"subtitleField\":\"product.tags\",\"titleTag\":\"h2\"}}]}"

// TestTagRecalcRebuildsBoundProductDetailPage 自动标签归属重算 → 归属**变化过的商品**的
// 详情页必须重建（详情页登记的是 direct_content:product:{id}，只有逐商品键能命中它）。
// 失败能力的落点：recalcTagTx 里 membershipDiff 那段逐商品入队摘掉即红。
func TestTagRecalcRebuildsBoundProductDetailPage(t *testing.T) {
	f := newOutboxFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	xID := f.createProduct(t, "标签商品", "tag-item") // 售价 88
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
	// 规则标签：先设成「1000~2000」——售价 88 的商品不命中。
	tagName := "高价精选"
	tag, err := f.products.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: tagName, Slug: "premium",
		Kind: productenums.TagKindRule, RuleType: productenums.TagRulePriceRange,
		RuleParams: json.RawMessage(`{"minPrice":1000,"maxPrice":2000}`),
	})
	if err != nil {
		t.Fatalf("创建规则标签失败: %v", err)
	}
	// 消费掉装配期事件，再取基线。
	f.dispatch(t)
	if html := instanceActiveHTML(t, f.db, inst.ID, "zh-CN"); strings.Contains(html, tagName) {
		t.Fatalf("基线详情页不该含未命中的标签名：%s", firstLine(html))
	}

	// 改规则让它命中该商品（88 落在 50~150）→ 归属变化 → 详情页必须重建。
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
	// 机制证据：事件表里必须有一条指向**该商品**的 direct_content 键。
	rows, err := productmodel.NewModel(f.db).ListOutboxEvents(ctx, productcontract.EntityTypeProduct, xID)
	if err != nil {
		t.Fatalf("读取 outbox 失败: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.DependencyKind == pipeline.DepKindDirectContent && row.DependencyKey == "product:"+xID {
			found = true
		}
	}
	if !found {
		t.Fatalf("归属重算应为命中变化过的商品写 direct_content:product:%s（否则详情页不更新）", xID)
	}
	// 字节证据：详情页的线上产物出现标签名。
	for _, lang := range []string{"zh-CN", "en-US"} {
		html := instanceActiveHTML(t, f.db, inst.ID, lang)
		if !strings.Contains(html, tagName) {
			t.Fatalf("详情页 %s 未随归属重算更新（不含 %q）：%s", lang, tagName, firstLine(html))
		}
	}
}

// —— 审计 ARCH-01 收口票：实体改名 → 引用它的商品详情页 ——
//
// 四个改名入口（分类 / 品牌 / 标签 / 属性组的 Update）在提交事务内，除实体键外
// **逐引用商品**发 direct_content:product:{id}。没有这一步时：商品详情页登记的是
// product:{id}，改名只发实体键命中不到它 —— 站点上表现为「改了分类名，商品页还是旧名」，
// 且日志里什么都没有。
//
// 用例的目标产物只登记 direct_content:product:{id}（模板里没有集合组件），
// 因此这件事不会被商品集合键掩盖。

// renameFieldDoc 只渲染「商品名 + 指定商品字段」的模板（无集合组件）。
func renameFieldDoc(field string) string {
	return "{\"settings\":{\"layout\":{\"mode\":\"boxed\",\"maxWidth\":\"1200px\"}},\"root\":" +
		"[{\"id\":\"p1\",\"type\":\"core.product\",\"props\":{\"source\":\"product\"," +
		"\"titleField\":\"product.name\",\"subtitleField\":\"" + field + "\",\"titleTag\":\"h2\"}}]}"
}

// TestEntityRenameRebuildsReferencingProductPages 四个实体的改名入口各一条：
// 改名后引用它的商品详情页在 zh-CN / en-US 两种语言上都必须更新。
func TestEntityRenameRebuildsReferencingProductPages(t *testing.T) {
	type renameCase struct {
		name       string
		entityType string
		// field 模板里绑定的商品字段：分类 / 品牌走 related（含名称），
		// 标签走 tags（展示名数组），属性组走 options（规格维度含组名）。
		field string
		// create 建实体并返回 id + 「把该实体挂到商品上」的装配函数。
		create func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq))
		// rename 改名（只改名字，不动引用关系）。
		rename func(t *testing.T, f *outboxFixture, entityID, newName string)
	}
	cases := []renameCase{
		{
			name: "分类", entityType: productcontract.EntityTypeCategory, field: "product.related",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				cat, err := f.products.CreateCategory(context.Background(), &productdto.CreateCategoryReq{
					ProjectID: f.projectID, Name: "旧分类名", Slug: slug,
				})
				if err != nil {
					t.Fatalf("创建分类失败: %v", err)
				}
				return cat.ID, func(req *productdto.CreateReq) { req.CategoryIDs = []string{cat.ID} }
			},
			rename: func(t *testing.T, f *outboxFixture, entityID, newName string) {
				if _, err := f.products.UpdateCategory(context.Background(), &productdto.UpdateCategoryReq{
					ID: entityID, ProjectID: f.projectID, Name: &newName,
				}); err != nil {
					t.Fatalf("改分类名失败: %v", err)
				}
			},
		},
		{
			name: "品牌", entityType: productcontract.EntityTypeBrand, field: "product.related",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				brand, err := f.products.CreateBrand(context.Background(), &productdto.CreateBrandReq{
					ProjectID: f.projectID, Name: "旧品牌名", Slug: slug,
				})
				if err != nil {
					t.Fatalf("创建品牌失败: %v", err)
				}
				return brand.ID, func(req *productdto.CreateReq) { req.BrandID = brand.ID }
			},
			rename: func(t *testing.T, f *outboxFixture, entityID, newName string) {
				if _, err := f.products.UpdateBrand(context.Background(), &productdto.UpdateBrandReq{
					ID: entityID, ProjectID: f.projectID, Name: &newName,
				}); err != nil {
					t.Fatalf("改品牌名失败: %v", err)
				}
			},
		},
		{
			name: "标签", entityType: productcontract.EntityTypeTag, field: "product.tags",
			create: func(t *testing.T, f *outboxFixture, slug string) (string, func(*productdto.CreateReq)) {
				tag, err := f.products.CreateTag(context.Background(), &productdto.CreateTagReq{
					ProjectID: f.projectID, Name: "旧标签名", Slug: slug, Kind: productenums.TagKindManual,
				})
				if err != nil {
					t.Fatalf("创建标签失败: %v", err)
				}
				return tag.ID, func(req *productdto.CreateReq) { req.TagIDs = []string{tag.ID} }
			},
			rename: func(t *testing.T, f *outboxFixture, entityID, newName string) {
				if _, err := f.products.UpdateTag(context.Background(), &productdto.UpdateTagReq{
					ID: entityID, ProjectID: f.projectID, Name: &newName,
				}); err != nil {
					t.Fatalf("改标签名失败: %v", err)
				}
			},
		},
		{
			name: "属性组", entityType: productcontract.EntityTypeAttribute, field: "product.options",
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
			rename: func(t *testing.T, f *outboxFixture, entityID, newName string) {
				if _, err := f.products.UpdateAttribute(context.Background(), &productdto.UpdateAttributeReq{
					ID: entityID, ProjectID: f.projectID, Name: &newName,
				}); err != nil {
					t.Fatalf("改属性组名失败: %v", err)
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

			// 基线：产物里是旧名字。
			if html := instanceActiveHTML(t, f.db, inst.ID, "zh-CN"); !strings.Contains(html, "旧"+tc.name+"名") {
				t.Fatalf("%s：基线详情页应含旧名「旧%s名」：%s", tc.name, tc.name, firstLine(html))
			}
			// 机制证据的基线：建商品本身也会写一条 product:{id}，这里数的是**改名新增**的那条。
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
			newName := "新" + tc.name + "名"
			tc.rename(t, f, entityID, newName)
			if n := f.dispatch(t); n == 0 {
				t.Fatalf("%s 改名后 outbox 应有待消费事件", tc.name)
			}
			// 机制证据：改名必须为引用它的商品**新增**一条 direct_content。
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
				t.Fatalf("%s 改名应为引用它的商品新增 direct_content:%s（改名前 %d 条，改名后 %d 条）",
					tc.name, key, beforeHits, afterHits)
			}
			// 字节证据：详情页两种语言都换成新名字。
			for _, lang := range []string{"zh-CN", "en-US"} {
				html := instanceActiveHTML(t, f.db, inst.ID, lang)
				if !strings.Contains(html, newName) {
					t.Fatalf("%s 改名后详情页 %s 未更新（不含 %q）：%s", tc.name, lang, newName, firstLine(html))
				}
			}
		})
	}
}
