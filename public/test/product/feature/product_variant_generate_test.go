// Package feature product 模块 feature 测试 —— 变体组合生成与默认值继承（issue #8）。
//
// 覆盖本票五条验收（真实 PostgreSQL + 生产 DDL + 真实 service）：
//  1. 勾选若干属性值后自动生成全部组合；重复勾选 / 重复提交不产生重复变体；
//  2. 超过维度或数量上限时拒绝并给出明确提示（且一条变体都不写）；
//  3. 新变体逐字段继承商品级默认值；空值以 NULL 判定，0 与 false 视为已填；
//  4. 编辑商品级默认值不影响已有变体（只影响之后新增的变体）；
//  5. 无表单路径（不传勾选 = 批量生成 / 导入 / 接口）同样走判空继承。
//
// 组合的载体语义也在本文件固定下来：商品创建时生成的首个「无规格变体」就地承接
// 第一个组合（保留其已填字段），其余组合新建 —— 商品不会多出一条悬挂变体。
package feature

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"

	"go_wp/public/migrations"
)

// mkVariationAttr 建一个「参与变体」的属性组（值与 key 一一对应，便于断言组合）。
func mkVariationAttr(t *testing.T, f *attrFixture, name, key string, valueKeys []string) *productdto.AttributeResp {
	t.Helper()
	values := make([]productdto.AttributeValueReq, 0, len(valueKeys))
	for _, k := range valueKeys {
		values = append(values, productdto.AttributeValueReq{Label: strings.ToUpper(k), Key: k})
	}
	attr, err := f.svc.CreateAttribute(context.Background(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: name, Key: key, Values: values,
	})
	if err != nil {
		t.Fatalf("创建属性组 %s 失败: %v", name, err)
	}
	return attr
}

// mkVariationProduct 建一个引用若干属性组的商品（同时生成它的首个无规格变体）。
func mkVariationProduct(t *testing.T, f *attrFixture, name, slug string, attributeIDs []string, defaultPrice *float64) *productdto.ProductResp {
	t.Helper()
	p, err := f.svc.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
		AttributeIDs: attributeIDs, DefaultPrice: defaultPrice,
	})
	if err != nil {
		t.Fatalf("创建商品 %s 失败: %v", name, err)
	}
	return p
}

// optionMap 读变体的规格组合。
func optionMap(t *testing.T, v *productdto.VariantResp) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := json.Unmarshal(v.OptionValues, &out); err != nil {
		t.Fatalf("规格组合解析失败: %v（%s）", err, string(v.OptionValues))
	}
	return out
}

// TestGenerateVariantsCartesianProduct 验收 1：
// 勾选（或全部）属性值后生成全部组合，顺序确定、组合互不相同、占位变体被承接。
func TestGenerateVariantsCartesianProduct(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	size := mkVariationAttr(t, f, "尺寸", "size", []string{"s", "m"})
	price := 99.0
	p := mkVariationProduct(t, f, "夏季衬衫", "shirt", []string{color.ID, size.ID}, &price)
	if p.VariantCount != 1 {
		t.Fatalf("前置条件：商品创建后应有 1 个占位变体，实际 %d", p.VariantCount)
	}

	// 无表单路径（不传勾选）= 全部参与变体的属性组 × 全部启用值。
	res, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("生成变体组合失败: %v", err)
	}
	if res.Total != 4 {
		t.Fatalf("2×2 应得到 4 个组合，实际 %d", res.Total)
	}
	if res.Adopted != 1 || res.Created != 3 || res.Skipped != 0 {
		t.Fatalf("占位变体承接 1 + 新建 3，实际 adopted=%d created=%d skipped=%d",
			res.Adopted, res.Created, res.Skipped)
	}
	if len(res.Variants) != 4 {
		t.Fatalf("生成后应有 4 个变体（不额外留悬挂变体），实际 %d", len(res.Variants))
	}
	// 组合顺序确定：按商品 attribute_ids 顺序（颜色 → 尺寸），最后一维变化最快。
	want := []map[string]string{
		{"color": "red", "size": "s"},
		{"color": "red", "size": "m"},
		{"color": "blue", "size": "s"},
		{"color": "blue", "size": "m"},
	}
	seen := map[string]bool{}
	for i, v := range res.Variants {
		got := optionMap(t, v)
		if len(got) != 2 {
			t.Fatalf("第 %d 个变体的组合应有两个维度，实际 %v", i, got)
		}
		if got["color"] != want[i]["color"] || got["size"] != want[i]["size"] {
			t.Fatalf("第 %d 个组合应为 %v，实际 %v", i, want[i], got)
		}
		if seen[v.SKUCode] {
			t.Fatalf("SKU 编码重复：%s", v.SKUCode)
		}
		seen[v.SKUCode] = true
	}

	// 商品详情回读：变体数与价格区间随之更新（价格区间是变体派生值）。
	got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if got.VariantCount != 4 {
		t.Fatalf("商品变体数应为 4，实际 %d", got.VariantCount)
	}
}

// TestGenerateVariantsIdempotentAndPartial 验收 1（幂等）：
// 重复提交、重复勾选同一值、只勾部分值，都不产生重复变体。
func TestGenerateVariantsIdempotentAndPartial(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	size := mkVariationAttr(t, f, "尺寸", "size", []string{"s", "m"})
	p := mkVariationProduct(t, f, "衬衫", "shirt-2", []string{color.ID, size.ID}, nil)

	if _, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID}); err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	// 同参数再生成一次：全部跳过，变体数不变。
	res, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("重复生成失败: %v", err)
	}
	if res.Created != 0 || res.Skipped != 4 || len(res.Variants) != 4 {
		t.Fatalf("重复提交应全部跳过：created=%d skipped=%d total=%d",
			res.Created, res.Skipped, len(res.Variants))
	}

	// 重复勾选同一个值（写两次）只算一个值：颜色限定为红，尺码取全部启用值，
	// 两个组合都已存在 → 全部跳过，不产生重复变体。
	red := color.Values[0].ID
	res, err = f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{
		ProductID: p.ID,
		Selections: []productdto.VariantSelectionReq{
			{AttributeID: color.ID, ValueIDs: []string{red, red}},
		},
	})
	if err != nil {
		t.Fatalf("重复勾选生成失败: %v", err)
	}
	if res.Total != 2 || res.Skipped != 2 || res.Created != 0 {
		t.Fatalf("重复勾选应只算一个值、且组合已存在：total=%d skipped=%d created=%d",
			res.Total, res.Skipped, res.Created)
	}
	if got, _ := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID}); got.VariantCount != 4 {
		t.Fatalf("变体数不应变化，实际 %d", got.VariantCount)
	}

	// 追加一个只有单个值的属性组：勾选覆盖三个组 → 只补新值带来的那一个组合。
	green := mkVariationAttr(t, f, "颜色2", "color2", []string{"green"})
	if _, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, AttributeIDs: []string{color.ID, size.ID, green.ID},
	}); err != nil {
		t.Fatalf("追加属性组失败: %v", err)
	}
	res, err = f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{
		ProductID: p.ID,
		Selections: []productdto.VariantSelectionReq{
			{AttributeID: color.ID, ValueIDs: []string{color.Values[0].ID}},
			{AttributeID: size.ID, ValueIDs: []string{size.Values[0].ID}},
			{AttributeID: green.ID, ValueIDs: []string{green.Values[0].ID}},
		},
	})
	if err != nil {
		t.Fatalf("追加维度后生成失败: %v", err)
	}
	if res.Total != 1 || res.Created != 1 || res.Skipped != 0 {
		t.Fatalf("新维度应只新增一个组合：total=%d created=%d skipped=%d",
			res.Total, res.Created, res.Skipped)
	}
}

// TestGenerateVariantsLimits 验收 2：超维度 / 超数量时整体拒绝，且不写任何变体。
func TestGenerateVariantsLimits(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	t.Run("超维度", func(t *testing.T) {
		ids := make([]string, 0, 5)
		for _, key := range []string{"d1", "d2", "d3", "d4", "d5"} {
			ids = append(ids, mkVariationAttr(t, f, "维度"+key, key, []string{"a", "b"}).ID)
		}
		p := mkVariationProduct(t, f, "维度商品", "dim-limit", ids, nil)
		_, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
		if err == nil {
			t.Fatal("5 个维度应被拒绝")
		}
		if !strings.Contains(err.Error(), productenums.ErrVariationDimensionLimit) {
			t.Fatalf("错误应指向维度上限: %v", err)
		}
		if !strings.Contains(err.Error(), "4") {
			t.Fatalf("错误应写明上限数值: %v", err)
		}
		got, _ := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
		if got.VariantCount != 1 {
			t.Fatalf("被拒绝时不应写入任何变体，实际 %d 个", got.VariantCount)
		}
	})

	t.Run("超数量", func(t *testing.T) {
		keys := []string{"v1", "v2", "v3", "v4", "v5", "v6", "v7"}
		mk7 := func(name, key string) *productdto.AttributeResp {
			return mkVariationAttr(t, f, name, key, keys)
		}
		ids := []string{mk7("量大1", "q1").ID, mk7("量大2", "q2").ID, mk7("量大3", "q3").ID}
		p := mkVariationProduct(t, f, "数量商品", "count-limit", ids, nil)
		_, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
		if err == nil {
			t.Fatal("343 个组合应被拒绝")
		}
		if !strings.Contains(err.Error(), productenums.ErrVariationCountLimit) {
			t.Fatalf("错误应指向数量上限: %v", err)
		}
		got, _ := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
		if got.VariantCount != 1 {
			t.Fatalf("被拒绝时不应写入任何变体，实际 %d 个", got.VariantCount)
		}
		// 占位变体仍是「无规格」状态（拒绝发生在写库之前）。
		if len(optionMap(t, got.Variants[0])) != 0 {
			t.Fatalf("占位变体不应被改写：%s", string(got.Variants[0].OptionValues))
		}
	})
}

// TestGenerateVariantsInheritsDefaults 验收 3 + 5：
// 新变体逐字段继承商品级默认值；已填字段（显式 0）不被覆盖。
func TestGenerateVariantsInheritsDefaults(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	price := 99.0
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "默认值商品", Slug: "defaults",
		AttributeIDs: []string{color.ID}, DefaultPrice: &price, DefaultImage: "/img/default.jpg",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	// 占位变体显式改成 0 价：0 是「已填」，生成时不得被商品默认值覆盖。
	zero := 0.0
	if _, err := f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: p.Variants[0].ID, Price: &zero,
	}); err != nil {
		t.Fatalf("修改占位变体失败: %v", err)
	}

	res, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	adopted, created := res.Variants[0], res.Variants[1]
	if adopted.Price != 0 {
		t.Fatalf("被承接的占位变体应保留已填的 0 价，实际 %v", adopted.Price)
	}
	if created.Price != 99 {
		t.Fatalf("新建变体应继承商品默认价 99，实际 %v", created.Price)
	}
	if created.Image != "/img/default.jpg" {
		t.Fatalf("新建变体应继承默认图，实际 %q", created.Image)
	}
}

// TestProductDefaultsEditDoesNotTouchVariants 验收 4：
// 改商品级默认值只影响之后新增的变体，已有变体一个字段都不动。
func TestProductDefaultsEditDoesNotTouchVariants(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	first := 50.0
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "改默认值商品", Slug: "edit-defaults",
		AttributeIDs: []string{color.ID}, DefaultPrice: &first,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if _, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID}); err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}

	// 商品级默认价改成 200。
	next := 200.0
	if _, err := f.svc.Update(ctx, &productdto.UpdateReq{ID: p.ID, DefaultPrice: &next}); err != nil {
		t.Fatalf("改商品默认值失败: %v", err)
	}
	got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	for _, v := range got.Variants {
		if v.Price != 50 {
			t.Fatalf("改商品默认值不应影响已有变体，实际 %v（%s）", v.Price, v.SKUCode)
		}
	}

	// 给属性组加一个新值 → 新组合继承新的默认值 200。
	if _, err := f.svc.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
		ID: color.ID,
		Values: []productdto.AttributeValueReq{
			{ID: color.Values[0].ID, Label: "红", Key: "red"},
			{ID: color.Values[1].ID, Label: "蓝", Key: "blue"},
			{Label: "绿", Key: "green"},
		},
	}); err != nil {
		t.Fatalf("追加属性值失败: %v", err)
	}
	res, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("追加值后生成失败: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("追加一个值应只新增一个变体，实际 %d", res.Created)
	}
	if res.Variants[len(res.Variants)-1].Price != 200 {
		t.Fatalf("新变体应继承更新后的默认价 200，实际 %v", res.Variants[len(res.Variants)-1].Price)
	}
}

// TestGenerateVariantsSelectionValidation 边界：非法勾选一律报错，不静默忽略。
func TestGenerateVariantsSelectionValidation(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	material := mkVariationAttr(t, f, "材质", "material", []string{"cotton"})
	if _, err := f.svc.UpdateAttribute(ctx, &productdto.UpdateAttributeReq{
		ID: material.ID, IsVariation: boolPtr(false),
	}); err != nil {
		t.Fatalf("改标记失败: %v", err)
	}
	// 停用一个值：勾选已停用的值必须报错（不允许悄悄跳过）。
	if _, err := f.svc.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
		ID: color.ID,
		Values: []productdto.AttributeValueReq{
			{ID: color.Values[0].ID, Label: "红", Key: "red", Enabled: boolPtr(false)},
			{ID: color.Values[1].ID, Label: "蓝", Key: "blue"},
		},
	}); err != nil {
		t.Fatalf("停用属性值失败: %v", err)
	}
	p := mkVariationProduct(t, f, "校验商品", "validate", []string{color.ID, material.ID}, nil)
	reloaded, err := f.svc.GetAttribute(ctx, &productdto.GetAttributeReq{ID: color.ID})
	if err != nil {
		t.Fatalf("读属性组失败: %v", err)
	}

	cases := map[string]struct {
		selections []productdto.VariantSelectionReq
		want       string
	}{
		"属性组未引用":   {[]productdto.VariantSelectionReq{{AttributeID: "00000000-0000-0000-0000-000000000000"}}, productenums.ErrVariationAttributeInvalid},
		"属性组不参与变体": {[]productdto.VariantSelectionReq{{AttributeID: material.ID}}, productenums.ErrVariationAttributeInvalid},
		"值不属于该组":   {[]productdto.VariantSelectionReq{{AttributeID: color.ID, ValueIDs: []string{"not-a-value"}}}, productenums.ErrVariationValueInvalid},
		"值已停用":     {[]productdto.VariantSelectionReq{{AttributeID: color.ID, ValueIDs: []string{reloaded.Values[0].ID}}}, productenums.ErrVariationValueInvalid},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{
				ProductID: p.ID, Selections: c.selections,
			}); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("应返回 %s，实际 %v", c.want, err)
			}
		})
	}

	t.Run("没有可参与变体的属性值", func(t *testing.T) {
		plain := mkVariationProduct(t, f, "无属性商品", "no-attr", nil, nil)
		if _, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: plain.ID}); err == nil ||
			!strings.Contains(err.Error(), productenums.ErrVariationNoDimension) {
			t.Fatalf("没有属性组时应返回 %s，实际 %v", productenums.ErrVariationNoDimension, err)
		}
	})
}

// TestGenerateVariantsDisabledValueExcluded 边界：停用的值不参与「全部生成」。
func TestGenerateVariantsDisabledValueExcluded(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	color := mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})
	if _, err := f.svc.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
		ID: color.ID,
		Values: []productdto.AttributeValueReq{
			{ID: color.Values[0].ID, Label: "红", Key: "red", Enabled: boolPtr(false)},
			{ID: color.Values[1].ID, Label: "蓝", Key: "blue"},
		},
	}); err != nil {
		t.Fatalf("停用属性值失败: %v", err)
	}
	p := mkVariationProduct(t, f, "停用值商品", "disabled-value", []string{color.ID}, nil)
	res, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if res.Total != 1 || res.Created != 0 || res.Adopted != 1 {
		t.Fatalf("停用值不应参与组合：total=%d created=%d adopted=%d", res.Total, res.Created, res.Adopted)
	}
	if got := optionMap(t, res.Variants[0]); got["color"] != "blue" {
		t.Fatalf("唯一组合应是启用中的 blue，实际 %v", got)
	}
}

// legacyProductTemplate 085 原始文档（issue #6，缺规格槽位）——
// 用来模拟「085 已执行过、087b 尚未补槽位」的存量库。
const legacyProductTemplate = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd-section","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"pd-body","type":"core.product","props":{"source":"product","mediaField":"product.defaultImage","galleryField":"product.images","titleField":"product.name","subtitleField":"product.subtitle","priceField":"product.priceRange","comparePriceField":"product.comparePrice","descriptionField":"product.description","currency":"¥","titleTag":"h1"}}]}]}`

// TestVariantOptionsTemplateBackfill 迁移 087b：存量库的默认商品详情模板补规格槽位。
//
// 三条边界（数据迁移最容易出错的地方）：
//
//	· 与 085 原样一致的模板：草稿与不可变版本都必须补上槽位，且 source_hash 重算；
//	· 作者改过的模板：一个字节都不动（缺槽位是作者的选择）；
//	· 幂等：补完之后种子条件不再成立，重复执行不再改写。
func TestVariantOptionsTemplateBackfill(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	// 先跑一次种子：085 建默认模板（新文档已含规格槽位）。
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("首次执行种子失败: %v", err)
	}
	// 回滚成 085 原始文档，模拟存量库。
	if err := f.db.Exec("UPDATE content_templates SET draft_document = ?::jsonb WHERE entity_type = 'product'", legacyProductTemplate).Error; err != nil {
		t.Fatalf("回滚草稿失败: %v", err)
	}
	if err := f.db.Exec("UPDATE content_template_versions v SET document = ?::jsonb, source_hash = 'legacy' FROM content_templates t WHERE v.id = t.current_version_id AND t.entity_type = 'product'", legacyProductTemplate).Error; err != nil {
		t.Fatalf("回滚版本失败: %v", err)
	}

	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("补槽位种子执行失败: %v", err)
	}
	var draft, version, hash string
	if err := f.db.Raw("SELECT t.draft_document::text, v.document::text, v.source_hash FROM content_templates t JOIN content_template_versions v ON v.id = t.current_version_id WHERE t.entity_type = 'product'").Row().Scan(&draft, &version, &hash); err != nil {
		t.Fatalf("读模板失败: %v", err)
	}
	for name, doc := range map[string]string{"草稿": draft, "当前版本": version} {
		if !strings.Contains(doc, "optionsField") || !strings.Contains(doc, "variantsField") {
			t.Fatalf("%s 应补上规格槽位: %s", name, doc)
		}
	}
	if hash == "legacy" || hash == "" {
		t.Fatalf("补槽位后应重算 source_hash，实际 %q", hash)
	}

	// 幂等：再跑一次种子，模板内容不变。
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("重复执行种子失败: %v", err)
	}
	var again string
	if err := f.db.Raw("SELECT draft_document::text FROM content_templates WHERE entity_type = 'product'").Scan(&again).Error; err != nil {
		t.Fatalf("再次读模板失败: %v", err)
	}
	if again != draft {
		t.Fatalf("重复执行不应改写模板：前=%s 后=%s", draft, again)
	}

	// 作者改过的模板（同样缺槽位）不被重写。
	custom := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"only","type":"core.heading","props":{"text":"我的版式"}}]}`
	if err := f.db.Exec("UPDATE content_templates SET draft_document = ?::jsonb WHERE entity_type = 'product'", custom).Error; err != nil {
		t.Fatalf("写入自定义模板失败: %v", err)
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("自定义模板下执行种子失败: %v", err)
	}
	var afterCustom string
	if err := f.db.Raw("SELECT draft_document::text FROM content_templates WHERE entity_type = 'product'").Scan(&afterCustom).Error; err != nil {
		t.Fatalf("读自定义模板失败: %v", err)
	}
	if !strings.Contains(afterCustom, "我的版式") || strings.Contains(afterCustom, "optionsField") {
		t.Fatalf("作者改过的模板不应被改写: %s", afterCustom)
	}
}

// TestVariantGeneratePermissionSeeded 迁移 087：变体组合生成的权限点已 seed
// （未 seed 时 Casbin Enforce 无策略匹配 → 含超管在内全员 403）。
func TestVariantGeneratePermissionSeeded(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	// 权限点由种子迁移写入：fixture 只跑结构迁移，这里显式跑一次种子。
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM sys_permission
		WHERE permission_code = 'product:variant_generate'
		  AND api_path = '/api/product/variant/generate' AND api_method = 'POST'`).Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("迁移 087 应 seed product:variant_generate（POST /api/product/variant/generate），实际 %d", n)
	}
}
