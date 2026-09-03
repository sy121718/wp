package feature

// media_category_test.go — 媒体库分类链路：无限级分类 CRUD、防环、
// 删除级联（附件移入未分类）、按分类/类型/搜索筛选。

import (
	"context"
	"testing"
	"time"

	mediadto "go_wp/internal/module/media/dto"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func newMediaService(t *testing.T) (*gorm.DB, *mediaservice.Service) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, nil
	}
	for _, stmt := range []string{
		`CREATE TABLE sys_file_category (id BIGSERIAL PRIMARY KEY, category_name TEXT NOT NULL, category_code TEXT NOT NULL, parent_id INTEGER DEFAULT 0, sort_order INTEGER DEFAULT 0, icon TEXT, status INTEGER DEFAULT 1, create_by INTEGER, update_by INTEGER, create_time TIMESTAMPTZ, update_time TIMESTAMPTZ)`,
		`CREATE TABLE sys_attachment (id BIGSERIAL PRIMARY KEY, category_id INTEGER, file_name TEXT NOT NULL, file_path TEXT NOT NULL, file_size INTEGER, file_type TEXT, mime_type TEXT, storage_type TEXT DEFAULT 'local', storage_path TEXT, url TEXT, md5 TEXT, extra_info TEXT, status INTEGER DEFAULT 1, create_by INTEGER, update_by INTEGER, create_time TIMESTAMPTZ, update_time TIMESTAMPTZ)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	return db, mediaservice.NewService(mediamodel.NewAttachmentModel(db), mediamodel.NewFileCategoryModel(db))
}

func seedAttachment(t *testing.T, db *gorm.DB, categoryID *uint64, name, fileType, mime string) uint64 {
	t.Helper()
	now := time.Now()
	url := "/storage/test-" + name
	e := &mediamodel.AttachmentEntity{
		CategoryID: categoryID,
		FileName:   name,
		FilePath:   "test/" + name,
		FileSize:   1024,
		FileType:   fileType,
		MimeType:   &mime,
		StorageType: "local",
		URL:        &url,
		Status:     1,
		CreateTime: now,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("插入附件失败: %v", err)
	}
	return e.ID
}

func TestMediaCategoryTreeCRUD(t *testing.T) {
	db, svc := newMediaService(t)
	ctx := context.Background()

	root, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: "品牌素材"})
	if err != nil {
		t.Fatalf("创建顶级分类失败: %v", err)
	}
	child, err := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: root.ID, CategoryName: "Logo"})
	if err != nil {
		t.Fatalf("创建子分类失败: %v", err)
	}
	// 同父级重名拒绝。
	if _, err = svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{ParentID: root.ID, CategoryName: "  Logo "}); err == nil {
		t.Fatalf("同级重名应被拒绝")
	}
	// 防环：把 root 移到 child 下应拒绝。
	parent := child.ID
	if err = svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: root.ID, ParentID: &parent}); err == nil {
		t.Fatalf("移动到自己的子分类下应被拒绝")
	}
	// 改名。
	name := "品牌 Logo"
	if err = svc.UpdateCategory(ctx, &mediadto.CategoryUpdateReq{ID: child.ID, CategoryName: &name}); err != nil {
		t.Fatalf("改名称失败: %v", err)
	}
	// 有子级时删除 root 应拒绝。
	if err = svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: root.ID}); err == nil {
		t.Fatalf("有子级的分类删除应被拒绝")
	}
	// 删除子分类成功。
	if err = svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: child.ID}); err != nil {
		t.Fatalf("删除叶子分类失败: %v", err)
	}
	tree, err := svc.CategoryTree(ctx)
	if err != nil || len(tree) != 1 || len(tree[0].Children) != 0 {
		t.Fatalf("分类树应只剩根节点: %+v err=%v", tree, err)
	}
	_ = db
}

func TestMediaCategoryDeleteDetachesAttachments(t *testing.T) {
	db, svc := newMediaService(t)
	ctx := context.Background()
	root, _ := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: "素材"})

	// 分类下挂两个附件。
	cid := root.ID
	seedAttachment(t, db, &cid, "a.png", "image", "image/png")
	seedAttachment(t, db, &cid, "b.png", "image", "image/png")

	// 删除分类 → 附件应移入未分类（category_id=NULL）。
	if err := svc.DeleteCategory(ctx, &mediadto.CategoryDeleteReq{ID: root.ID}); err != nil {
		t.Fatalf("删除分类失败: %v", err)
	}
	resp, err := svc.List(ctx, &mediadto.ListReq{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("附件应保留且移入未分类: total=%d", resp.Total)
	}
	for _, item := range resp.List {
		if item.CategoryID != nil && *item.CategoryID != 0 {
			t.Fatalf("附件分类应被清空: %+v", item)
		}
	}
}

func TestMediaListFilters(t *testing.T) {
	db, svc := newMediaService(t)
	ctx := context.Background()
	root, _ := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: "产品图"})
	cid := root.ID
	seedAttachment(t, db, &cid, "product.png", "image", "image/png")
	seedAttachment(t, db, &cid, "video.mp4", "video", "video/mp4")
	seedAttachment(t, db, nil, "doc.pdf", "document", "application/pdf")

	// 按分类筛选：应命中 2 项。
	byCat, err := svc.List(ctx, &mediadto.ListReq{Page: 1, Limit: 20, CategoryID: &cid})
	if err != nil || byCat.Total != 2 {
		t.Fatalf("按分类筛选应命中 2 项: total=%d err=%v", byCat.Total, err)
	}
	// 按类型筛选。
	byType, err := svc.List(ctx, &mediadto.ListReq{Page: 1, Limit: 20, FileType: "video"})
	if err != nil || byType.Total != 1 {
		t.Fatalf("按类型筛选应命中 1 项: total=%d err=%v", byType.Total, err)
	}
	// 按名称搜索。
	bySearch, err := svc.List(ctx, &mediadto.ListReq{Page: 1, Limit: 20, Search: "product"})
	if err != nil || bySearch.Total != 1 {
		t.Fatalf("按名称搜索应命中 1 项: total=%d err=%v", bySearch.Total, err)
	}
	// 组合筛选：分类 + 类型。
	both, err := svc.List(ctx, &mediadto.ListReq{Page: 1, Limit: 20, CategoryID: &cid, FileType: "image"})
	if err != nil || both.Total != 1 {
		t.Fatalf("组合筛选应命中 1 项: total=%d err=%v", both.Total, err)
	}
}

func TestMediaUpdateAttachmentMeta(t *testing.T) {
	db, svc := newMediaService(t)
	ctx := context.Background()
	root, _ := svc.CreateCategory(ctx, &mediadto.CategoryCreateReq{CategoryName: "素材"})
	id := seedAttachment(t, db, nil, "x.png", "image", "image/png")

	// 更新：分类 + alt/title/description（ExtraInfo JSON 合并）。
	cid := root.ID
	alt, title, desc := "替代文本", "标题", "说明"
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{
		ID: id, CategoryID: &cid, Alt: &alt, Title: &title, Description: &desc,
	}); err != nil {
		t.Fatalf("更新附件失败: %v", err)
	}
	resp, err := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	if resp.CategoryID == nil || *resp.CategoryID != cid {
		t.Fatalf("分类未更新: %+v", resp.CategoryID)
	}
	if resp.ExtraInfo == "" {
		t.Fatalf("ExtraInfo 应写入 alt/title/description")
	}
	// 移入未分类。
	zero := uint64(0)
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: id, CategoryID: &zero}); err != nil {
		t.Fatalf("移入未分类失败: %v", err)
	}
	resp2, _ := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if resp2.CategoryID != nil {
		t.Fatalf("移入未分类后 CategoryID 应为空: %+v", resp2.CategoryID)
	}
}
