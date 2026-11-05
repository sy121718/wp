package feature

import (
	"context"
	"strings"
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/seo"
)

// publishForCatalog 走真实发布链路（实例 + 产物 + 语言激活行），返回实例 id。
func publishForCatalog(t *testing.T, f *detailFixture, productID, urlPath, role string) string {
	t.Helper()
	inst, err := f.pres.CreateInstance(context.Background(), &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID, URLPath: urlPath, InstanceRole: role,
	})
	if err != nil {
		t.Fatalf("创建发布实例失败：%v", err)
	}
	return inst.ID
}

func setPublished(t *testing.T, f *detailFixture, ids ...string) {
	t.Helper()
	status := productenums.StatusPublished
	for _, id := range ids {
		if _, err := f.products.Update(context.Background(), &productdto.UpdateReq{ID: id, Status: &status}); err != nil {
			t.Fatal(err)
		}
	}
}

func catalogOf(t *testing.T, f *detailFixture, projectID, lang string) *productdto.PublicCatalogResp {
	t.Helper()
	out, err := f.products.PublicCatalog(context.Background(), &productdto.PublicCatalogReq{
		ProjectID: projectID, Lang: lang, Page: 1, PageSize: 60,
	})
	if err != nil {
		t.Fatalf("目录查询失败：%v", err)
	}
	return out
}

// TestPublicCatalogPublicationCandidates 用真实发布夹具验证公开目录的候选资格：
// 有产物且路径非空白才进目录，同一实体多个发布实例不放大 total，工程与语言互不串味。
func TestPublicCatalogPublicationCandidates(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	defaultLang := f.projectsDefaultLang(t)

	visible := f.createProduct(t, "可见商品", "catalog-visible", "", 100, 120)
	hidden := f.createProduct(t, "草稿商品", "catalog-hidden", "", 100, 120)
	noArtifact := f.createProduct(t, "无产物商品", "catalog-noartifact", "", 100, 120)
	blankPath := f.createProduct(t, "空白路径商品", "catalog-blank", "", 100, 120)
	setPublished(t, f, visible, noArtifact, blankPath, hidden)
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: hidden, Status: ptrStatus(productenums.StatusDraft)}); err != nil {
		t.Fatal(err)
	}

	publishForCatalog(t, f, visible, "/catalog/visible", "detail")
	publishForCatalog(t, f, noArtifact, "/catalog/noartifact", "detail")
	publishForCatalog(t, f, blankPath, "/catalog/blank", "detail")
	publishForCatalog(t, f, hidden, "/catalog/hidden", "detail")
	// 去掉产物 = 「有实例没产物」；全空白路径含制表符与全角空格，与 TrimSpace 等价判定。
	if err := f.db.Exec("UPDATE presentation_publications SET artifact_id = NULL WHERE active_path = ?", "/catalog/noartifact").Error; err != nil {
		t.Fatalf("清理产物失败：%v", err)
	}
	if err := f.db.Exec("UPDATE presentation_publications SET active_path = ? WHERE active_path = ?", " \t\u3000\u00a0\n ", "/catalog/blank").Error; err != nil {
		t.Fatalf("改写空白路径失败：%v", err)
	}

	out := catalogOf(t, f, f.projectID, defaultLang)
	if out.Total != 1 || len(out.Items) != 1 || out.Items[0].ID != visible {
		t.Fatalf("只有可见商品应入选，实际 total=%d items=%+v", out.Total, out.Items)
	}
	if want := seo.AbsoluteSiteURL("/catalog/visible"); out.Items[0].URL != want {
		t.Fatalf("目录项应带该语言的发布路径 %s，实际 %q", want, out.Items[0].URL)
	}

	// 同一实体再发布一个 archive 实例：两条独立实例行，去重后仍只算一个商品。
	publishForCatalog(t, f, visible, "/archive/visible", "archive")
	var instanceCount int64
	if err := f.db.Raw("SELECT COUNT(*) FROM presentation_instances WHERE entity_type = ? AND entity_id = ?", "product", visible).Scan(&instanceCount).Error; err != nil {
		t.Fatalf("统计实例失败：%v", err)
	}
	if instanceCount != 2 {
		t.Fatalf("夹具应有 2 条实例（detail + archive），实际 %d", instanceCount)
	}
	again := catalogOf(t, f, f.projectID, defaultLang)
	if again.Total != 1 || len(again.Items) != 1 {
		t.Fatalf("同一实体两个发布实例时 total 不能翻倍，实际 total=%d items=%d", again.Total, len(again.Items))
	}
	// 两条路径都在候选内时取确定的最小值，与旧全集 map 的无序取值对齐。
	if want := seo.AbsoluteSiteURL("/archive/visible"); again.Items[0].URL != want {
		t.Fatalf("多路径时应取 MIN(active_path)=%s，实际 %q", want, again.Items[0].URL)
	}

	// 语言隔离：只发布了默认语言，用任何别的语言查都为空。
	otherLang := "en-US"
	if otherLang == defaultLang {
		otherLang = "zh-CN"
	}
	if other := catalogOf(t, f, f.projectID, otherLang); other.Total != 0 || len(other.Items) != 0 {
		t.Fatalf("语言 %s 不该命中，实际 total=%d items=%d", otherLang, other.Total, len(other.Items))
	}

	// 工程隔离：另一个工程的目录看不到本工程商品。
	otherProject, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个目录工程"})
	if err != nil {
		t.Fatal(err)
	}
	if foreign := catalogOf(t, f, otherProject.ID, defaultLang); foreign.Total != 0 || len(foreign.Items) != 0 {
		t.Fatalf("其他工程不该命中，实际 total=%d items=%d", foreign.Total, len(foreign.Items))
	}

	// 实体类型隔离：把该商品的全部发布实例都改成 article，它就不再是商品目录候选。
	if err := f.db.Exec(
		"UPDATE presentation_instances SET entity_type = ? WHERE entity_id = ?",
		"article", visible).Error; err != nil {
		t.Fatalf("改写实体类型失败：%v", err)
	}
	dropped := catalogOf(t, f, f.projectID, defaultLang)
	if dropped.Total != 0 || len(dropped.Items) != 0 {
		t.Fatalf("实体类型不是 product 的发布不该进商品目录，实际 total=%d items=%d", dropped.Total, len(dropped.Items))
	}
}

func ptrStatus(v string) *string { return &v }

func (f *detailFixture) projectsDefaultLang(t *testing.T) string {
	t.Helper()
	lang, err := f.projects.DefaultLocale(context.Background(), f.projectID)
	if err != nil || strings.TrimSpace(lang) == "" {
		t.Fatalf("取工程默认语言失败：%v %q", err, lang)
	}
	return lang
}
