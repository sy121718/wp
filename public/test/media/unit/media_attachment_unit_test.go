package unit

// media_attachment_unit_test.go — media 模块附件 service 层单元测试。
// 覆盖：上传记录创建（类型分类/大小校验/物理文件清理）、查询、软删除、分类归属更新、
// ExtraInfo JSON 合并。
// 标注 [BUG-xx] 的用例按「正确语义」断言（期望行为），当前生产代码未满足时会 FAIL，
// 作为生产代码 bug 的复现证据；bug 明细见测试报告。

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mediadto "go_wp/internal/module/media/dto"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/upload"

	"github.com/spf13/viper"
)

// initUploadForTest 初始化上传组件（local provider，默认 10MB 上限）。
func initUploadForTest(t *testing.T) {
	t.Helper()
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() { _ = upload.Close() })
}

// newMultipartFileHeader 构造带指定 Content-Type 的 multipart 文件头；
// declaredSize>0 时覆盖声明大小（用于超限快速拒绝路径）。
func newMultipartFileHeader(t *testing.T, filename, contentType string, declaredSize int64) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fake-content-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	fhs := req.MultipartForm.File["file"]
	if len(fhs) == 0 {
		t.Fatal("multipart 解析未得到文件")
	}
	fh := fhs[0]
	if declaredSize > 0 {
		fh.Size = declaredSize
	}
	return fh
}

// cleanupUploadedFile 注册清理：删除 local provider 写入的物理文件（相对测试包 cwd 的 public/storage）。
func cleanupUploadedFile(t *testing.T, resp *mediadto.AttachmentResp) {
	t.Helper()
	if resp == nil || resp.URL == "" {
		return
	}
	key := strings.TrimPrefix(resp.URL, "/storage/")
	t.Cleanup(func() {
		path := filepath.Join("public", "storage", filepath.FromSlash(key))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Errorf("清理上传物理文件失败: %v", err)
		}
	})
}

// TestMediaUploadSuccess 上传成功：类型分类、元数据落库、可查询。
func TestMediaUploadSuccess(t *testing.T) {
	db, svc := newMediaUnitService(t)
	initUploadForTest(t)
	ctx := context.Background()

	fh := newMultipartFileHeader(t, "photo.png", "image/png", 0)
	resp, err := svc.Upload(ctx, fh, nil)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	cleanupUploadedFile(t, resp)

	if resp.FileType != "image" {
		t.Fatalf("png 应归类为 image: got=%q", resp.FileType)
	}
	if resp.StorageType != "local" {
		t.Fatalf("存储类型应为 local: got=%q", resp.StorageType)
	}
	if resp.MimeType != "image/png" {
		t.Fatalf("MIME 类型不正确: got=%q", resp.MimeType)
	}
	if resp.URL == "" || !strings.HasPrefix(resp.URL, "/storage/") {
		t.Fatalf("URL 应指向存储: %q", resp.URL)
	}
	if resp.FileSize != int64(len("fake-content-bytes")) {
		t.Fatalf("文件大小不正确: got=%d", resp.FileSize)
	}
	if resp.CreateTime == "" {
		t.Fatalf("创建时间不应为空")
	}

	detail, err := svc.Detail(ctx, &mediadto.DetailReq{ID: resp.ID})
	if err != nil {
		t.Fatalf("上传后详情查询失败: %v", err)
	}
	if detail.ID != resp.ID || detail.FileName != "photo.png" {
		t.Fatalf("落库记录不一致: %+v", detail)
	}
	_ = db
}

// TestMediaUploadNilFileRejected nil 文件拒绝。
func TestMediaUploadNilFileRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	_, err := svc.Upload(ctx, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "上传文件不能为空") {
		t.Fatalf("nil 文件应被拒绝: %v", err)
	}
}

// TestMediaUploadCategoryNotExistAccepted [BUG-05] 上传指定不存在的分类仍成功。
// 期望：拒绝（目标分类不存在，与 UpdateAttachment 的校验保持一致）。
// 实际：Upload 直接写 category_id，不校验分类存在 → FAIL。
func TestMediaUploadCategoryNotExistAccepted(t *testing.T) {
	_, svc := newMediaUnitService(t)
	initUploadForTest(t)
	ctx := context.Background()

	fh := newMultipartFileHeader(t, "x.png", "image/png", 0)
	missing := uint64(99999)
	resp, err := svc.Upload(ctx, fh, &missing)
	if resp != nil {
		cleanupUploadedFile(t, resp)
	}
	if err == nil {
		t.Errorf("上传到不存在的分类应被拒绝，实际成功（脏数据：附件指向不存在分类）")
	}
}

// TestMediaUploadOversizeRejected 声明大小超过上限（默认 10MB）拒绝。
func TestMediaUploadOversizeRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	initUploadForTest(t)
	ctx := context.Background()

	fh := newMultipartFileHeader(t, "big.png", "image/png", 11*1024*1024)
	resp, err := svc.Upload(ctx, fh, nil)
	if resp != nil {
		cleanupUploadedFile(t, resp)
	}
	if err == nil || !strings.Contains(err.Error(), "大小超限") {
		t.Fatalf("超限文件应被拒绝: %v", err)
	}
}

// TestMediaUploadMimeFallbackClassify 未知扩展名按 MIME 兜底归类。
func TestMediaUploadMimeFallbackClassify(t *testing.T) {
	_, svc := newMediaUnitService(t)
	initUploadForTest(t)
	ctx := context.Background()

	fh := newMultipartFileHeader(t, "photo.xyz", "image/png", 0)
	resp, err := svc.Upload(ctx, fh, nil)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	cleanupUploadedFile(t, resp)
	if resp.FileType != "image" {
		t.Fatalf("未知扩展名 + image/* MIME 应兜底为 image: got=%q", resp.FileType)
	}
}

// TestMediaListDefaultsAndPaging 分页默认值：Page<=0→1、Limit<=0 或 >100→20；显式分页透传。
func TestMediaListDefaultsAndPaging(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	t.Run("空库默认分页", func(t *testing.T) {
		resp, err := svc.List(ctx, &mediadto.ListReq{})
		if err != nil {
			t.Fatalf("列表查询失败: %v", err)
		}
		if resp.Page != 1 || resp.Limit != 20 {
			t.Fatalf("默认分页不正确: page=%d limit=%d", resp.Page, resp.Limit)
		}
		if resp.Total != 0 || len(resp.List) != 0 {
			t.Fatalf("空库应返回空列表: %+v", resp)
		}
	})
	t.Run("Limit 超上限收敛为 20", func(t *testing.T) {
		resp, err := svc.List(ctx, &mediadto.ListReq{Limit: 101})
		if err != nil {
			t.Fatalf("列表查询失败: %v", err)
		}
		if resp.Limit != 20 {
			t.Fatalf("Limit>100 应收敛为 20: got=%d", resp.Limit)
		}
	})
	t.Run("显式分页透传", func(t *testing.T) {
		resp, err := svc.List(ctx, &mediadto.ListReq{Page: 3, Limit: 10})
		if err != nil {
			t.Fatalf("列表查询失败: %v", err)
		}
		if resp.Page != 3 || resp.Limit != 10 {
			t.Fatalf("显式分页未透传: page=%d limit=%d", resp.Page, resp.Limit)
		}
	})
}

// TestMediaListFilters 按分类/类型/搜索筛选命中数正确。
func TestMediaListFilters(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	cat := seedCategory(t, db, "产品图", 0, 1)
	cid := cat.ID
	seedAttachment(t, db, &cid, "product.png", "image", "")
	seedAttachment(t, db, &cid, "video.mp4", "video", "")
	seedAttachment(t, db, nil, "doc.pdf", "document", "")

	t.Run("按分类筛选", func(t *testing.T) {
		resp, err := svc.List(ctx, &mediadto.ListReq{CategoryID: &cid})
		if err != nil || resp.Total != 2 {
			t.Fatalf("按分类筛选应命中 2 项: total=%d err=%v", resp.Total, err)
		}
	})
	t.Run("按类型筛选", func(t *testing.T) {
		resp, err := svc.List(ctx, &mediadto.ListReq{FileType: "video"})
		if err != nil || resp.Total != 1 {
			t.Fatalf("按类型筛选应命中 1 项: total=%d err=%v", resp.Total, err)
		}
	})
	t.Run("按名称搜索", func(t *testing.T) {
		resp, err := svc.List(ctx, &mediadto.ListReq{Search: "doc"})
		if err != nil || resp.Total != 1 {
			t.Fatalf("按名称搜索应命中 1 项: total=%d err=%v", resp.Total, err)
		}
	})
	t.Run("组合筛选", func(t *testing.T) {
		resp, err := svc.List(ctx, &mediadto.ListReq{CategoryID: &cid, FileType: "image"})
		if err != nil || resp.Total != 1 {
			t.Fatalf("组合筛选应命中 1 项: total=%d err=%v", resp.Total, err)
		}
	})
}

// TestMediaListSearchWildcardNotEscaped [BUG-08] 搜索词含 SQL LIKE 通配符（_ / %）未转义。
// 期望：搜索 "img_100" 按字面匹配，只命中 1 条。
// 实际：LIKE '%img_100%' 中 _ 匹配任意单字符，命中 img_100 / imgX100 / img100 共 3 条 → FAIL。
func TestMediaListSearchWildcardNotEscaped(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	seedAttachment(t, db, nil, "img_100.png", "image", "")
	seedAttachment(t, db, nil, "imgX100.png", "image", "")
	seedAttachment(t, db, nil, "img100.png", "image", "")

	resp, err := svc.List(ctx, &mediadto.ListReq{Search: "img_100"})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("搜索 img_100 应按字面匹配仅命中 1 条，实际 %d 条（_ 被当作通配符未转义）: %+v", resp.Total, resp.List)
	}
}

// TestMediaDetailSuccess 详情字段完整（MD5 空串、创建时间格式化）。
func TestMediaDetailSuccess(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	id := seedAttachment(t, db, nil, "a.png", "image", "")
	resp, err := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	if resp.FileName != "a.png" || resp.FileType != "image" {
		t.Fatalf("详情字段不正确: %+v", resp)
	}
	if resp.MD5 != "" {
		t.Fatalf("未设置 MD5 时应为空串: %q", resp.MD5)
	}
	if resp.CreateTime == "" {
		t.Fatalf("创建时间应格式化输出")
	}
}

// TestMediaDetailNotExistRejected 不存在的附件返回"附件不存在"。
func TestMediaDetailNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	_, err := svc.Detail(ctx, &mediadto.DetailReq{ID: 99999})
	if err == nil || !strings.Contains(err.Error(), "附件不存在") {
		t.Fatalf("不存在的附件应返回附件不存在: %v", err)
	}
}

// TestMediaDeleteInvisibleInList 删除后列表不可见（软删除 status=0）。
func TestMediaDeleteInvisibleInList(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	id := seedAttachment(t, db, nil, "a.png", "image", "")
	if err := svc.Delete(ctx, &mediadto.DeleteReq{ID: id}); err != nil {
		t.Fatalf("删除附件失败: %v", err)
	}
	resp, err := svc.List(ctx, &mediadto.ListReq{})
	if err != nil || resp.Total != 0 {
		t.Fatalf("删除后列表应为空: total=%d err=%v", resp.Total, err)
	}
	var got mediamodel.AttachmentEntity
	if err := db.First(&got, id).Error; err != nil {
		t.Fatalf("软删除记录应保留: %v", err)
	}
	if got.Status != mediamodel.AttachmentStatusDisabled {
		t.Fatalf("软删除后 status 应为 0: %d", got.Status)
	}
}

// TestMediaDeleteNotExistRejected 删除不存在的附件返回"附件不存在"。
func TestMediaDeleteNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, &mediadto.DeleteReq{ID: 99999})
	if err == nil || !strings.Contains(err.Error(), "附件不存在") {
		t.Fatalf("不存在的附件删除应被拒绝: %v", err)
	}
}

// TestMediaDetailAfterDeleteVisible [BUG-04] 附件软删除后 Detail 仍返回记录。
// 期望：删除后详情应返回"附件不存在"（与 List 过滤 status=1 保持一致）。
// 实际：GetByID 不过滤 status，删除后仍可读到 → FAIL。
func TestMediaDetailAfterDeleteVisible(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	id := seedAttachment(t, db, nil, "a.png", "image", "")
	if err := svc.Delete(ctx, &mediadto.DeleteReq{ID: id}); err != nil {
		t.Fatalf("删除附件失败: %v", err)
	}
	_, err := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if err == nil {
		t.Errorf("已删除附件仍可通过详情查询到（软删除状态未过滤），应返回附件不存在")
	}
}

// TestMediaUpdateAttachmentMetaMerge 更新分类 + ExtraInfo 合并保留已有键。
func TestMediaUpdateAttachmentMetaMerge(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	cat := seedCategory(t, db, "素材", 0, 1)
	id := seedAttachment(t, db, nil, "a.png", "image", `{"alt":"旧alt"}`)

	title, desc := "标题", "说明"
	cid := cat.ID
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{
		ID: id, CategoryID: &cid, Title: &title, Description: &desc,
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
	extra := map[string]string{}
	if err := json.Unmarshal([]byte(resp.ExtraInfo), &extra); err != nil {
		t.Fatalf("ExtraInfo 解析失败: %v raw=%q", err, resp.ExtraInfo)
	}
	if extra["alt"] != "旧alt" || extra["title"] != "标题" || extra["description"] != "说明" {
		t.Fatalf("ExtraInfo 合并不正确: %+v", extra)
	}

	// 移入未分类（CategoryID=0 → NULL）。
	zero := uint64(0)
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: id, CategoryID: &zero}); err != nil {
		t.Fatalf("移入未分类失败: %v", err)
	}
	resp2, _ := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if resp2.CategoryID != nil {
		t.Fatalf("移入未分类后 CategoryID 应为空: %+v", resp2.CategoryID)
	}
}

// TestMediaUpdateAttachmentExtraInfoArrayOverwritten [BUG-06] 原 ExtraInfo 非 JSON 对象时更新丢数据。
// 期望：更新 alt 不应丢失原有数据（或明确报错）。
// 实际：json.Unmarshal 到 map 失败被忽略（_ =），extra 为空 map，写入仅含新字段的对象，原数据被覆盖 → FAIL。
func TestMediaUpdateAttachmentExtraInfoArrayOverwritten(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	// 合法 JSON 但非对象（数组），模拟历史/外部写入的数据。
	id := seedAttachment(t, db, nil, "a.png", "image", `[1,2]`)
	alt := "新alt"
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: id, Alt: &alt}); err != nil {
		t.Fatalf("更新附件失败: %v", err)
	}

	resp, err := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	var arr []any
	if err := json.Unmarshal([]byte(resp.ExtraInfo), &arr); err != nil {
		t.Errorf("更新 alt 后原 ExtraInfo 数据应保留，实际被覆盖: %q", resp.ExtraInfo)
	}
}

// TestMediaUpdateAttachmentNotExistRejected 更新不存在的附件拒绝。
func TestMediaUpdateAttachmentNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	name := "x"
	err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: 99999, FileName: &name})
	if err == nil || !strings.Contains(err.Error(), "附件不存在") {
		t.Fatalf("不存在的附件更新应被拒绝: %v", err)
	}
}

// TestMediaUpdateAttachmentCategoryNotExistRejected 目标分类不存在拒绝。
func TestMediaUpdateAttachmentCategoryNotExistRejected(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	id := seedAttachment(t, db, nil, "a.png", "image", "")
	missing := uint64(99999)
	err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: id, CategoryID: &missing})
	if err == nil || !strings.Contains(err.Error(), "目标分类不存在") {
		t.Fatalf("目标分类不存在应被拒绝: %v", err)
	}
}

// TestMediaUpdateAttachmentEmptyFieldsIgnored 空白文件名不覆盖；空 alt 删除对应键。
func TestMediaUpdateAttachmentEmptyFieldsIgnored(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	id := seedAttachment(t, db, nil, "a.png", "image", `{"alt":"旧alt"}`)
	blankName, blankAlt := "   ", ""
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{
		ID: id, FileName: &blankName, Alt: &blankAlt,
	}); err != nil {
		t.Fatalf("更新附件失败: %v", err)
	}
	resp, err := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	if resp.FileName != "a.png" {
		t.Fatalf("空白文件名不应覆盖原名: got=%q", resp.FileName)
	}
	extra := map[string]string{}
	if err := json.Unmarshal([]byte(resp.ExtraInfo), &extra); err != nil {
		t.Fatalf("ExtraInfo 解析失败: %v raw=%q", err, resp.ExtraInfo)
	}
	if _, ok := extra["alt"]; ok {
		t.Fatalf("空 alt 应删除对应键: %+v", extra)
	}
}
