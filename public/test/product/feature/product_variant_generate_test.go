package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"

	"go_wp/public/schema"
)

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

func optionMap(t *testing.T, v *productdto.VariantResp) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := json.Unmarshal(v.OptionValues, &out); err != nil {
		t.Fatalf("规格组合解析失败: %v（%s）", err, string(v.OptionValues))
	}
	return out
}

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

	got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if got.VariantCount != 4 {
		t.Fatalf("商品变体数应为 4，实际 %d", got.VariantCount)
	}
}

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

	res, err := f.svc.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("重复生成失败: %v", err)
	}
	if res.Created != 0 || res.Skipped != 4 || len(res.Variants) != 4 {
		t.Fatalf("重复提交应全部跳过：created=%d skipped=%d total=%d",
			res.Created, res.Skipped, len(res.Variants))
	}

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

		if len(optionMap(t, got.Variants[0])) != 0 {
			t.Fatalf("占位变体不应被改写：%s", string(got.Variants[0].OptionValues))
		}
	})
}

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
		AttributeIDs: []string{color.ID}, DefaultPrice: &price, DefaultImage: "/storage/img/default.jpg",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

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
	if created.Image != testMediaBaseURL+"/img/default.jpg" {
		t.Fatalf("新建变体应继承默认图，实际 %q", created.Image)
	}
}

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

func TestVariantGeneratePermissionSeeded(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}

	if err := schema.Apply(f.db); err != nil {
		t.Fatalf("载入 schema 失败：%v", err)
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
