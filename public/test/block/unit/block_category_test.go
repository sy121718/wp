package unit

import (
	"context"
	"fmt"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
)

// TestBlockCategoryCreate 覆盖 Create 路径的分类归一化：空值默认 general、
// 合法白名单值原样入库、非法值（大写/中文/空格/点/斜杠/超长）拒绝。
func TestBlockCategoryCreate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("EmptyDefaultsGeneral", func(t *testing.T) {
		res := e.createBlock(t, "默认分类", "")
		if res.Category != blockmodel.DefaultCategory {
			t.Fatalf("空分类应默认 general，实际: %q", res.Category)
		}
	})

	t.Run("WhitespaceDefaultsGeneral", func(t *testing.T) {
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "空白分类", Category: "   "})
		if err != nil {
			t.Fatalf("空白分类应默认 general: %v", err)
		}
		if res.Category != blockmodel.DefaultCategory {
			t.Fatalf("空白分类应默认 general，实际: %q", res.Category)
		}
	})

	t.Run("ValidCategories", func(t *testing.T) {
		valid := []string{"product", "product-list", "product_list", "marketing-2024", "a", strings.Repeat("x", 50)}
		for _, c := range valid {
			res, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "cat-" + c, Category: c})
			if err != nil {
				t.Fatalf("合法分类 %q 应通过: %v", c, err)
			}
			if res.Category != c {
				t.Fatalf("分类未按输入保存: got %q want %q", res.Category, c)
			}
		}
	})

	t.Run("InvalidCategoriesRejected", func(t *testing.T) {
		invalid := []string{"Hero", "商品区块", "a b", "a.b", "a@b", "a/b", strings.Repeat("x", 51)}
		for i, c := range invalid {
			_, err := e.svc.Create(ctx, &blockdto.CreateReq{
				ProjectID: e.projectID, Name: fmt.Sprintf("非法分类%d", i), Category: c,
			})
			errContains(t, err, blockenums.ErrBlockInvalidCategory)
		}
	})
}

// TestBlockCategoryListFilter 覆盖 List 按分类筛选：命中/无命中/非法筛选拒绝。
func TestBlockCategoryListFilter(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, _ = e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "商品A", Category: "product"})
	_, _ = e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "商品B", Category: "product"})
	_, _ = e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "文章A", Category: "article"})
	_, _ = e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "默认块", Category: ""})

	t.Run("FilterByCategory", func(t *testing.T) {
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Category: "product"})
		if err != nil {
			t.Fatalf("分类筛选应无错误: %v", err)
		}
		if len(res) != 2 {
			t.Fatalf("product 分类应 2 个块: %#v", res)
		}
		for _, b := range res {
			if b.Category != "product" {
				t.Fatalf("筛选结果含非 product 分类: %#v", b)
			}
		}
	})

	t.Run("FilterGeneral", func(t *testing.T) {
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Category: blockmodel.DefaultCategory})
		if err != nil {
			t.Fatalf("general 筛选应无错误: %v", err)
		}
		if len(res) != 1 || res[0].Name != "默认块" {
			t.Fatalf("general 分类应 1 个块: %#v", res)
		}
	})

	t.Run("FilterNoMatch", func(t *testing.T) {
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Category: "nonexistent"})
		if err != nil {
			t.Fatalf("无匹配分类应无错误: %v", err)
		}
		if len(res) != 0 {
			t.Fatalf("无匹配分类应空列表: %#v", res)
		}
	})

	t.Run("InvalidCategoryRejected", func(t *testing.T) {
		_, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Category: "Hero"})
		errContains(t, err, blockenums.ErrBlockInvalidCategory)
	})
}

// TestBlockCategoryUpdate 覆盖 Update 路径：改分类持久化、空值保留原值、非法值拒绝且数据不变。
func TestBlockCategoryUpdate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	created := e.createBlock(t, "分类块", "")

	t.Run("ChangeCategory", func(t *testing.T) {
		res, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: created.ID, Category: "product"})
		if err != nil {
			t.Fatalf("改分类应成功: %v", err)
		}
		if res.Category != "product" {
			t.Fatalf("分类未更新: %q", res.Category)
		}
		// Detail 的 ProjectID 是必填（跨工程越权防护），漏传会得到「参数缺失」而不是「块不存在」。
		got, err := e.svc.Detail(ctx, &blockdto.DetailReq{ProjectID: e.projectID, ID: created.ID})
		if err != nil {
			t.Fatalf("读块详情失败: %v", err)
		}
		if got.Category != "product" {
			t.Fatalf("分类更新未持久化: %#v", got)
		}
	})

	t.Run("EmptyCategoryKeepsExisting", func(t *testing.T) {
		res, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: created.ID, Category: "  "})
		if err != nil {
			t.Fatalf("空分类应保留原值: %v", err)
		}
		if res.Category != "product" {
			t.Fatalf("空分类不应覆盖，实际: %q", res.Category)
		}
	})

	t.Run("InvalidCategoryRejected", func(t *testing.T) {
		_, err := e.svc.Update(ctx, &blockdto.UpdateReq{ID: created.ID, Category: "商品区块"})
		errContains(t, err, blockenums.ErrBlockInvalidCategory)
		got, derr := e.svc.Detail(ctx, &blockdto.DetailReq{ProjectID: e.projectID, ID: created.ID})
		if derr != nil {
			t.Fatalf("读块详情失败: %v", derr)
		}
		if got.Category != "product" {
			t.Fatalf("非法分类更新失败后分类不应变化: %q", got.Category)
		}
	})
}
