package feature

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	"go_wp/pkg/i18n"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

// loadFeatureWords 从本用例自己的迁移库里取词条灌进缓存：出口只认词库，
// 不灌就只有共享内部提示。不走 i18n.LoadCache，它读的是会漂移的全局库。
func loadFeatureWords(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		ItemKey string `gorm:"column:item_key"`
		Lang    string
		Value   string `gorm:"column:value"`
	}
	if err := db.Table("sys_i18n").
		Select("item_key", "lang", "item_value AS value").
		Where("status = ?", 1).
		Scan(&rows).Error; err != nil {
		t.Fatalf("读取词条失败：%v", err)
	}
	words := make(map[string]map[string]string, len(rows))
	for _, r := range rows {
		if words[r.ItemKey] == nil {
			words[r.ItemKey] = map[string]string{}
		}
		words[r.ItemKey][r.Lang] = r.Value
	}
	i18n.InjectForTest(words, nil)
}

type attrFixture struct {
	svc       *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	projectID string
}

// newAttrFixture 建真实迁移库、跑 seed 并加载词库：出口只认词库取词，
// 词条来自 seed，不跑不加载就会把可读文案退化成共享内部提示。
func newAttrFixture(t *testing.T) *attrFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	// 迁移只建表，seed 得单独跑：sys_i18n 的词条就是 seed 写进去的。
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败：%v", err)
	}
	loadFeatureWords(t, db)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "属性测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	return &attrFixture{
		svc:       productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects)),
		db:        db,
		projects:  projects,
		projectID: project.ID,
	}
}

// rewireProductChanges 用另一份审计端口重建商品服务：审计是构造依赖，故障注入只能换装配。
func (f *attrFixture) rewireProductChanges(t *testing.T, changes masterdatacontract.MasterDataService) {
	t.Helper()
	f.svc = productservice.NewService(productmodel.NewModel(f.db), f.projects, changes)
}

func boolPtr(b bool) *bool { return &b }

func TestAttributeGroupAndValuesStableIdentityAndSort(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	created, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID,
		Name:      "颜色",
		Key:       "color",
		Sort:      2,
		Values: []productdto.AttributeValueReq{
			{Label: "红色", Key: "red", Sort: 20},
			{Label: "蓝色", Key: "blue", Sort: 10},
		},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	if created.Key != "color" {
		t.Fatalf("属性组标识应为 color，实际 %q", created.Key)
	}
	if len(created.Values) != 2 {
		t.Fatalf("应有 2 个属性值，实际 %d", len(created.Values))
	}

	if created.Values[0].Key != "blue" || created.Values[1].Key != "red" {
		t.Fatalf("属性值应按 Sort 升序，实际 %v, %v", created.Values[0].Key, created.Values[1].Key)
	}
	for _, v := range created.Values {
		if v.ID == "" {
			t.Fatalf("属性值缺少稳定 id：%+v", v)
		}
		if v.Key == "" {
			t.Fatalf("属性值缺少稳定 key：%+v", v)
		}

		if strings.ToLower(v.Key) != v.Key {
			t.Fatalf("属性值 key 应为小写标识，实际 %q", v.Key)
		}
	}

	blueID := created.Values[0].ID
	updated, err := f.svc.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
		ID: created.ID,
		Values: []productdto.AttributeValueReq{
			{ID: blueID, Label: "深蓝", Key: "blue", Sort: 10},
			{Label: "绿色", Key: "green", Sort: 30},
			{Label: "红色", Key: "red", Sort: 20},
		},
	})
	if err != nil {
		t.Fatalf("保存属性值失败: %v", err)
	}
	if updated.Values[0].ID != blueID {
		t.Fatalf("改名不应改变属性值 id：期望 %s，实际 %s", blueID, updated.Values[0].ID)
	}
	if updated.Values[0].Label != "深蓝" {
		t.Fatalf("属性值改名未生效：%q", updated.Values[0].Label)
	}
	if updated.ValueCount != 3 {
		t.Fatalf("全量替换后应有 3 个值，实际 %d", updated.ValueCount)
	}
}

func TestAttributeSortNormalizationAndFallbackKey(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	created, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID,
		Name:      "口味",
		Values: []productdto.AttributeValueReq{
			{Label: "原味"},
			{Label: "香辣"},
		},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	if created.Key == "" {
		t.Fatalf("纯中文组名应兜底生成 key")
	}
	if len(created.Values) != 2 {
		t.Fatalf("应有 2 个值，实际 %d", len(created.Values))
	}
	for i, v := range created.Values {
		if v.Key == "" {
			t.Fatalf("第 %d 个值缺少 key（应兜底生成）", i)
		}
		if v.Sort == 0 && v.Label != "" {
			t.Fatalf("未给 Sort 的值应按下标补位，实际 0：%+v", v)
		}
	}
}

func TestAttributeVariationFlag(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	created, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "材质", IsVariation: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	if created.IsVariation {
		t.Fatalf("显式标记为不参与变体，实际 IsVariation=true")
	}

	row, gerr := productmodel.NewModel(f.db).GetAttribute(ctx, created.ID, f.projectID)
	if gerr != nil {
		t.Fatalf("读属性组失败: %v", gerr)
	}
	if row.IsVariation {
		t.Fatalf("数据库 is_variation 应为 false")
	}

	def, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "尺寸"})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	if !def.IsVariation {
		t.Fatalf("未显式指定时应默认参与变体")
	}

	updated, err := f.svc.UpdateAttribute(ctx, &productdto.UpdateAttributeReq{
		ID: created.ID, IsVariation: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("更新属性组失败: %v", err)
	}
	if !updated.IsVariation {
		t.Fatalf("标记改回参与变体未生效")
	}
	again, err := f.svc.GetAttribute(ctx, &productdto.GetAttributeReq{ID: created.ID})
	if err != nil {
		t.Fatalf("读属性组失败: %v", err)
	}
	if !again.IsVariation {
		t.Fatalf("重新读取应仍为参与变体")
	}
}

func TestAttributeGroupReusedByMultipleProducts(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	group, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色",
		Values: []productdto.AttributeValueReq{{Label: "红色", Key: "red"}},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	p1, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "衬衫", Slug: "shirt", AttributeIDs: []string{group.ID},
	})
	if err != nil {
		t.Fatalf("创建商品 1 失败: %v", err)
	}
	p2, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "外套", Slug: "coat", AttributeIDs: []string{group.ID},
	})
	if err != nil {
		t.Fatalf("创建商品 2 失败: %v", err)
	}
	for _, p := range []*productdto.ProductResp{p1, p2} {
		if len(p.AttributeIDs) != 1 || p.AttributeIDs[0] != group.ID {
			t.Fatalf("商品 %s 应引用同一属性组，实际 %v", p.Name, p.AttributeIDs)
		}
		if len(p.Attributes) != 1 || p.Attributes[0].ID != group.ID {
			t.Fatalf("商品 %s 应带出共享的属性组定义", p.Name)
		}
	}

	if _, err = f.svc.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
		ID: group.ID,
		Values: []productdto.AttributeValueReq{
			{Label: "红色", Key: "red"},
			{Label: "蓝色", Key: "blue"},
		},
	}); err != nil {
		t.Fatalf("更新属性值失败: %v", err)
	}
	for _, id := range []string{p1.ID, p2.ID} {
		got, gerr := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: id})
		if gerr != nil {
			t.Fatalf("读商品失败: %v", gerr)
		}
		if len(got.Attributes) != 1 || got.Attributes[0].ValueCount != 2 {
			t.Fatalf("商品 %s 应看到更新后的共享定义（2 个值），实际 %+v", got.Name, got.Attributes)
		}
	}
}

func TestAttributeDeleteGuards(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	group, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "颜色"})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "衬衫", Slug: "shirt-2", AttributeIDs: []string{group.ID},
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	delErr := f.svc.DeleteAttribute(ctx, &productdto.DeleteAttributeReq{ID: group.ID})
	assertRefGuardError(t, delErr, productenums.ErrAttrInUse, "products.attribute_ids")

	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ID: p.ID, AttributeIDs: []string{}}); err != nil {
		t.Fatalf("解绑属性组失败: %v", err)
	}
	if delErr := f.svc.DeleteAttribute(ctx, &productdto.DeleteAttributeReq{ID: group.ID}); delErr != nil {
		t.Fatalf("解绑后应可删除，实际 %v", delErr)
	}

	otherProjects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	other, err := otherProjects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	foreign, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: other.ID, Name: "别人家的属性"})
	if err != nil {
		t.Fatalf("创建第二工程属性组失败: %v", err)
	}
	if _, err = f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "外套", Slug: "coat-2", AttributeIDs: []string{foreign.ID},
	}); err == nil || err.Error() != productenums.ErrAttrProjectMismatch {
		t.Fatalf("跨工程引用应返回 ErrAttrProjectMismatch，实际 %v", err)
	}

	if _, err = f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "外套", Slug: "coat-3",
		AttributeIDs: []string{"00000000-0000-0000-0000-000000000000"},
	}); err == nil || err.Error() != productenums.ErrAttrNotFound {
		t.Fatalf("不存在的属性组应返回 ErrAttrNotFound，实际 %v", err)
	}
}

func TestAttributeKeyUniqueness(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if _, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "颜色", Key: "color"}); err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	if _, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "颜色 2", Key: "color"}); err == nil || err.Error() != productenums.ErrAttrKeyTaken {
		t.Fatalf("同工程重名标识应返回 ErrAttrKeyTaken，实际 %v", err)
	}

	otherProjects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	other, err := otherProjects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程 B"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	if _, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: other.ID, Name: "颜色", Key: "color"}); err != nil {
		t.Fatalf("不同工程可用同一标识，实际 %v", err)
	}
}

func TestAttributeMigrationIndexAndBackfill(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	var idxCount int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema()
		AND tablename = 'product_attributes'
		AND indexname = 'uq_product_attributes_project_key_nonempty'`).Scan(&idxCount).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	if idxCount != 1 {
		t.Fatalf("迁移 086 应建立 uq_product_attributes_project_key_nonempty 索引，实际 %d", idxCount)
	}
	var blank int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_attributes WHERE key IS NULL OR btrim(key) = ''").Scan(&blank).Error; err != nil {
		t.Fatalf("查询空 key 失败: %v", err)
	}
	if blank != 0 {
		t.Fatalf("历史空 key 应被回填，实际仍有 %d 行", blank)
	}
}
