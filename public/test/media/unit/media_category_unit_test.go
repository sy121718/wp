package unit

// media_category_unit_test.go — media 模块分类 service 层单元测试。
// 覆盖：创建（重名/层级/父级校验/空名/超长）、树构建、更新（改名/移动防环/排序）、
// 删除（有子级拒绝/附件级联/幂等）。
// 标注 [BUG-xx] 的用例按「正确语义」断言（期望行为），当前生产代码未满足时会 FAIL，
// 作为生产代码 bug 的复现证据；bug 明细见测试报告。

import (
	"context"
	"strings"
	"testing"

	mediadto "go_wp/internal/module/media/dto"
	mediamodel "go_wp/internal/module/media/model"
)

// TestMediaCategoryCreateSuccess 创建顶级与子级分类成功：去空格、code 生成、Children 空切片。
func TestMediaCategoryCreateSuccess(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	root, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: "  品牌素材  "})
	if err != nil {
		t.Fatalf("创建顶级分类失败: %v", err)
	}
	if root.CategoryName != "品牌素材" {
		t.Fatalf("分类名应去除首尾空格: got=%q", root.CategoryName)
	}
	if root.ID == 0 || !strings.HasPrefix(root.CategoryCode, "cat_") {
		t.Fatalf("分类 ID/Code 生成异常: %+v", root)
	}
	if root.ParentID != 0 {
		t.Fatalf("顶级分类 ParentID 应为 0: %+v", root)
	}
	if root.Children == nil || len(root.Children) != 0 {
		t.Fatalf("新建分类 Children 应为空切片: %+v", root.Children)
	}

	child, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: root.ID, CategoryName: "Logo"})
	if err != nil {
		t.Fatalf("创建子级分类失败: %v", err)
	}
	if child.ParentID != root.ID {
		t.Fatalf("子级分类 ParentID 不正确: got=%d want=%d", child.ParentID, root.ID)
	}
	_ = db
}

// TestMediaCategoryCreateEmptyNameRejected 空名/纯空白名拒绝。
func TestMediaCategoryCreateEmptyNameRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	for name, req := range map[string]*mediadto.CategoryCreateReq{
		"空字符串": {CategoryName: ""},
		"纯空白":  {CategoryName: "   \t "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.CreateCategory(ctx, req); err == nil {
				t.Fatalf("空分类名应被拒绝")
			}
		})
	}
}

// TestMediaCategoryCreateDuplicateSiblingRejected 同父级重名拒绝（含大小写与空白变体）；不同父级同名允许。
func TestMediaCategoryCreateDuplicateSiblingRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	root, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: "品牌素材"})
	if err != nil {
		t.Fatalf("创建顶级分类失败: %v", err)
	}
	if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: root.ID, CategoryName: "Logo"}); err != nil {
		t.Fatalf("创建子分类失败: %v", err)
	}

	t.Run("同名拒绝", func(t *testing.T) {
		if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: root.ID, CategoryName: "Logo"}); err == nil {
			t.Fatalf("同级重名应被拒绝")
		}
	})
	t.Run("大小写变体拒绝", func(t *testing.T) {
		if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: root.ID, CategoryName: "LOGO"}); err == nil {
			t.Fatalf("同级重名（大小写不敏感）应被拒绝")
		}
	})
	t.Run("空格变体拒绝", func(t *testing.T) {
		if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: root.ID, CategoryName: "  Logo "}); err == nil {
			t.Fatalf("同级重名（带空格）应被拒绝")
		}
	})
	t.Run("不同父级同名允许", func(t *testing.T) {
		if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: "Logo"}); err != nil {
			t.Fatalf("不同父级下同名应被允许: %v", err)
		}
	})
}

// TestMediaCategoryCreateParentNotExistRejected 父级不存在拒绝。
func TestMediaCategoryCreateParentNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: 99999, CategoryName: "孤儿"}); err == nil {
		t.Fatalf("父级不存在的创建应被拒绝")
	}
}

// TestMediaCategoryCreateParentDeletedAccepted [BUG-01] 父分类已软删除（status=0）时仍允许创建子分类。
// 期望：拒绝（父级已删除，不应再作为父级）。实际：GetCategory 不过滤 status，创建成功 → FAIL。
func TestMediaCategoryCreateParentDeletedAccepted(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	deleted := seedCategory(t, db, "已删除分类", 0, 0) // status=0 模拟软删除
	if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: deleted.ID, CategoryName: "新子分类"}); err == nil {
		t.Errorf("在已删除分类下创建子分类应被拒绝，实际成功（孤儿数据）")
	}
}

// TestMediaCategoryCreateOversizeNameError 超长分类名（> varchar(100)）返回错误且不 panic。
func TestMediaCategoryCreateOversizeNameError(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	longName := strings.Repeat("长", 200)
	if _, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: longName}); err == nil {
		t.Fatalf("超长分类名应返回错误")
	}
}

// TestMediaCategoryTreeBuild 多级树构建：层级正确、按 sort_order 排序、空库返回空列表。
func TestMediaCategoryTreeBuild(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	t.Run("空库返回空列表", func(t *testing.T) {
		tree, err := svc.CategoryTree(ctx)
		if err != nil {
			t.Fatalf("空库分类树应无错误: %v", err)
		}
		if len(tree) != 0 {
			t.Fatalf("空库应返回空树: %+v", tree)
		}
	})

	t.Run("三级树结构", func(t *testing.T) {
		r1 := seedCategory(t, db, "根1", 0, 1)
		seedCategory(t, db, "根2", 0, 1)
		c1 := seedCategory(t, db, "子1", r1.ID, 1)
		seedCategory(t, db, "孙1", c1.ID, 1)

		tree, err := svc.CategoryTree(ctx)
		if err != nil {
			t.Fatalf("构建分类树失败: %v", err)
		}
		if len(tree) != 2 {
			t.Fatalf("顶级节点数应为 2: %d", len(tree))
		}
		var root *mediadto.CategoryTreeNode
		for i := range tree {
			if tree[i].ID == r1.ID {
				root = &tree[i]
			}
		}
		if root == nil {
			t.Fatalf("未找到根1 节点: %+v", tree)
		}
		if len(root.Children) != 1 || len(root.Children[0].Children) != 1 {
			t.Fatalf("树层级不正确: %+v", root)
		}
		if root.Children[0].Children[0].CategoryName != "孙1" {
			t.Fatalf("孙级节点名不正确: %+v", root.Children[0].Children[0])
		}
	})
}

// TestMediaCategoryUpdateRenameSuccess 改名成功且去除首尾空格。
func TestMediaCategoryUpdateRenameSuccess(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	e := seedCategory(t, db, "旧名", 0, 1)
	newName := "  新名  "
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: e.ID, CategoryName: &newName}); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	got, err := svc.CategoryTree(ctx)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 1 || got[0].CategoryName != "新名" {
		t.Fatalf("改名后名称不正确: %+v", got)
	}
}

// TestMediaCategoryUpdateRenameDuplicateAccepted [BUG-03] 更新改名不检查同级重名，绕过创建时约束。
// 期望：同级重名拒绝。实际：UpdateCategory 直接写 name，重名成功 → FAIL。
func TestMediaCategoryUpdateRenameDuplicateAccepted(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	a := seedCategory(t, db, "分类A", 0, 1)
	seedCategory(t, db, "分类B", 0, 1)

	dup := "分类B"
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: a.ID, CategoryName: &dup}); err == nil {
		t.Errorf("改名为同级已存在名称应被拒绝，实际成功（重名约束被绕过）")
	}
}

// TestMediaCategoryUpdateMoveSelfRejected 父级设为自己拒绝。
func TestMediaCategoryUpdateMoveSelfRejected(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	e := seedCategory(t, db, "A", 0, 1)
	self := e.ID
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: e.ID, ParentID: &self}); err == nil {
		t.Fatalf("父级不能是自己")
	}
}

// TestMediaCategoryUpdateMoveDescendantRejected 移动到自己的子/孙分类下拒绝（防环）。
func TestMediaCategoryUpdateMoveDescendantRejected(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	root := seedCategory(t, db, "根", 0, 1)
	child := seedCategory(t, db, "子", root.ID, 1)
	grand := seedCategory(t, db, "孙", child.ID, 1)

	t.Run("移到直接子级", func(t *testing.T) {
		parent := child.ID
		if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: root.ID, ParentID: &parent}); err == nil {
			t.Fatalf("移动到自己的子分类下应被拒绝")
		}
	})
	t.Run("移到孙级", func(t *testing.T) {
		parent := grand.ID
		if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: root.ID, ParentID: &parent}); err == nil {
			t.Fatalf("移动到自己的孙分类下应被拒绝")
		}
	})
	t.Run("叶子移动到兄弟下允许", func(t *testing.T) {
		parent := child.ID
		if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: grand.ID, ParentID: &parent}); err != nil {
			t.Fatalf("叶子分类移动到父级下应允许: %v", err)
		}
	})
}

// TestMediaCategoryUpdateMoveParentNotExistRejected 目标父级不存在拒绝。
func TestMediaCategoryUpdateMoveParentNotExistRejected(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	e := seedCategory(t, db, "A", 0, 1)
	missing := uint64(99999)
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: e.ID, ParentID: &missing}); err == nil {
		t.Fatalf("目标父级不存在应被拒绝")
	}
}

// TestMediaCategoryUpdateMoveToDeletedParentAccepted [BUG-02] 可把分类移动到已软删除的分类下。
// 期望：拒绝（目标父级已删除）。实际：GetCategory 不过滤 status，移动成功 → FAIL。
func TestMediaCategoryUpdateMoveToDeletedParentAccepted(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	a := seedCategory(t, db, "A", 0, 1)
	deleted := seedCategory(t, db, "已删除父级", 0, 0) // status=0 模拟软删除
	parent := deleted.ID

	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: a.ID, ParentID: &parent}); err == nil {
		t.Errorf("移动到已删除分类下应被拒绝，实际成功（产生不可见孤儿节点）")
	}
}

// TestMediaCategoryUpdateSortOrder 排序更新生效。
func TestMediaCategoryUpdateSortOrder(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	a := seedCategory(t, db, "A", 0, 1)
	b := seedCategory(t, db, "B", 0, 1)

	sort := 100
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: a.ID, SortOrder: &sort}); err != nil {
		t.Fatalf("更新排序失败: %v", err)
	}
	tree, err := svc.CategoryTree(ctx)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(tree) != 2 || tree[0].ID != b.ID || tree[1].ID != a.ID {
		t.Fatalf("排序未按 sort_order 生效: %+v", tree)
	}
	_ = db
}

// TestMediaCategoryUpdateNotExistRejected 更新不存在的分类拒绝。
func TestMediaCategoryUpdateNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	name := "任意"
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: 99999, CategoryName: &name}); err == nil {
		t.Fatalf("更新不存在的分类应被拒绝")
	}
}

// TestMediaCategoryUpdateBlankRenameKeepsName 改名传纯空白时保持原名（不报错不覆盖）。
func TestMediaCategoryUpdateBlankRenameKeepsName(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	e := seedCategory(t, db, "原名", 0, 1)
	blank := "   "
	if err := svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: e.ID, CategoryName: &blank}); err != nil {
		t.Fatalf("空白改名不应报错: %v", err)
	}
	tree, err := svc.CategoryTree(ctx)
	if err != nil || len(tree) != 1 || tree[0].CategoryName != "原名" {
		t.Fatalf("空白改名应保持原名: %+v err=%v", tree, err)
	}
}

// TestMediaCategoryDeleteWithChildrenRejected 有启用子级时拒绝删除。
func TestMediaCategoryDeleteWithChildrenRejected(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	root := seedCategory(t, db, "根", 0, 1)
	seedCategory(t, db, "子", root.ID, 1)

	if err := svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: root.ID}); err == nil {
		t.Fatalf("有子级的分类删除应被拒绝")
	}
}

// TestMediaCategoryDeleteWithAttachmentsDetach 有附件时删除成功，附件移入未分类（category_id=NULL）。
func TestMediaCategoryDeleteWithAttachmentsDetach(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	cat := seedCategory(t, db, "素材", 0, 1)
	cid := cat.ID
	id1 := seedAttachment(t, db, &cid, "a.png", "image", "")
	id2 := seedAttachment(t, db, &cid, "b.png", "image", "")

	if err := svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: cat.ID}); err != nil {
		t.Fatalf("删除分类失败: %v", err)
	}
	for _, id := range []uint64{id1, id2} {
		var got mediamodel.AttachmentEntity
		if err := db.First(&got, id).Error; err != nil {
			t.Fatalf("附件应保留: %v", err)
		}
		if got.CategoryID != nil {
			t.Fatalf("附件分类应移入未分类: %+v", got)
		}
	}
}

// TestMediaCategoryDeleteNotExistRejected 删除不存在的分类拒绝。
func TestMediaCategoryDeleteNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	if err := svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: 99999}); err == nil {
		t.Fatalf("删除不存在的分类应被拒绝")
	}
}

// TestMediaCategoryDeleteTwiceAccepted [BUG-07] 二次删除已删除分类仍返回成功。
// 期望：返回"分类不存在"。实际：GetCategory 不过滤 status，二次删除成功 → FAIL。
func TestMediaCategoryDeleteTwiceAccepted(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	e := seedCategory(t, db, "将被删除", 0, 1)
	if err := svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: e.ID}); err != nil {
		t.Fatalf("首次删除失败: %v", err)
	}
	if err := svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: e.ID}); err == nil {
		t.Errorf("二次删除已删除分类应报分类不存在，实际返回成功")
	}
}
