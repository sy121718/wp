package unit

import (
	"bytes"
	"context"
	json "go_wp/pkg/jsonx"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mediadto "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/upload"

	"github.com/spf13/viper"
)

func initUploadForTest(t *testing.T) {
	t.Helper()
	cfg := viper.New()
	cfg.Set("upload.base_url", "https://media.example.test")
	cfg.Set("upload.default_provider", "local")
	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() { _ = upload.Close() })
}

func fileFixtureBytes(filename, contentType string) []byte {
	ext := strings.ToLower(filepath.Ext(filename))
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case ext == ".png" || ct == "image/png":

		return []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
			0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
			0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89}
	case ext == ".jpg" || ext == ".jpeg" || ct == "image/jpeg":
		return []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01}
	case ext == ".gif" || ct == "image/gif":
		return []byte("GIF89a\x01\x00\x01\x00\x00\xff\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x00;")
	case ext == ".pdf" || ct == "application/pdf":
		return []byte("%PDF-1.4\n")
	default:
		return []byte("fake-content-bytes")
	}
}

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

	if _, err := part.Write(fileFixtureBytes(filename, contentType)); err != nil {
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

func cleanupUploadedFile(t *testing.T, resp *mediadto.AttachmentResp) {
	t.Helper()
	if resp == nil || resp.URL == "" {
		return
	}
	key := upload.StorageKey(resp.URL)
	t.Cleanup(func() {
		path := filepath.Join("public", "storage", filepath.FromSlash(key))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Errorf("清理上传物理文件失败: %v", err)
		}
	})
}

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
	if resp.URL == "" || !strings.HasPrefix(resp.URL, upload.BaseURL()+"/") {
		t.Fatalf("URL 应指向存储: %q", resp.URL)
	}

	if want := int64(len(fileFixtureBytes("photo.png", "image/png"))); resp.FileSize != want {
		t.Fatalf("文件大小不正确: got=%d want=%d", resp.FileSize, want)
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

func TestMediaUploadNilFileRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	_, err := svc.Upload(ctx, nil, nil)
	if err == nil || !strings.Contains(err.Error(), mediaenums.ErrUploadEmpty) {
		t.Fatalf("nil 文件应被拒绝: %v", err)
	}
}

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

func TestMediaDetailNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	_, err := svc.Detail(ctx, &mediadto.DetailReq{ID: 99999})
	if err == nil || !strings.Contains(err.Error(), mediaenums.ErrAttachmentNotFound) {
		t.Fatalf("不存在的附件应返回附件不存在: %v", err)
	}
}

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

func TestMediaDeleteNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, &mediadto.DeleteReq{ID: 99999})
	if err == nil || !strings.Contains(err.Error(), mediaenums.ErrAttachmentNotFound) {
		t.Fatalf("不存在的附件删除应被拒绝: %v", err)
	}
}

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

	zero := uint64(0)
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: id, CategoryID: &zero}); err != nil {
		t.Fatalf("移入未分类失败: %v", err)
	}
	resp2, _ := svc.Detail(ctx, &mediadto.DetailReq{ID: id})
	if resp2.CategoryID != nil {
		t.Fatalf("移入未分类后 CategoryID 应为空: %+v", resp2.CategoryID)
	}
}

func TestMediaUpdateAttachmentExtraInfoArrayOverwritten(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

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

func TestMediaUpdateAttachmentNotExistRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	ctx := context.Background()

	name := "x"
	err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: 99999, FileName: &name})

	if err == nil || !strings.Contains(err.Error(), "附件不存在") {
		t.Fatalf("不存在的附件更新应被拒绝: %v", err)
	}
}

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
