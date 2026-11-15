package mediahttp

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediaservice "go_wp/internal/module/media/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"
)

type Handle struct {
	svc mediacontract.MediaService
}

func NewHandle(svc mediacontract.MediaService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) Upload(c *gin.Context) {
	var form mediadto.UploadReq
	if err := c.ShouldBindWith(&form, ginbinding.FormMultipart); err != nil || form.File == nil {
		response.ParamError(c, mediaenums.ErrUploadEmpty)
		return
	}
	att, err := h.svc.Upload(c.Request.Context(), form.File, form.CategoryIDValue())
	if err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, att)
}

func (h *Handle) List(c *gin.Context) {
	req, err := binding.ParseQuery[mediadto.ListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}

	resp, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "media", err)
		return
	}
	response.Success(c, resp)
}

func (h *Handle) Detail(c *gin.Context) {
	req, err := binding.ParseQuery[mediadto.DetailReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}

	att, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.Success(c, att)
}

func (h *Handle) Delete(c *gin.Context) {
	var req mediadto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}

	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, nil)
}

func (h *Handle) Replace(c *gin.Context) {
	// 替换是明确的 multipart 上传：文件头与 id 一起绑定，只解析一次请求体。
	var form mediadto.ReplaceReq
	if err := c.ShouldBindWith(&form, ginbinding.FormMultipart); err != nil {
		response.ParamError(c, mediaenums.MsgBadRequest)
		return
	}
	if form.ID == 0 {
		response.ParamError(c, mediaenums.MsgBadRequest)
		return
	}
	if form.File == nil {
		response.ParamError(c, mediaenums.ErrUploadEmpty)
		return
	}
	id := form.ID
	file := form.File
	att, err := h.svc.Replace(c.Request.Context(), id, file)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, att)
}

func (h *Handle) References(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("id")), 10, 64)
	if err != nil || id == 0 {
		response.ParamError(c, mediaenums.MsgBadRequest)
		return
	}
	refs, err := h.svc.References(c.Request.Context(), id)
	if err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.Success(c, refs)
}

func (h *Handle) CategoryTree(c *gin.Context) {
	tree, err := h.svc.CategoryTree(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "media", err)
		return
	}
	response.Success(c, tree)
}

func (h *Handle) CategoryCreate(c *gin.Context) {
	var req mediadto.CategoryCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.CreateCategory(c.Request.Context(), &req)
	if err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) CategoryUpdate(c *gin.Context) {
	var req mediadto.CategoryUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.UpdateCategory(c.Request.Context(), &req); err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) CategoryDelete(c *gin.Context) {
	var req mediadto.CategoryDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), &req); err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) UpdateAttachment(c *gin.Context) {
	var req mediadto.AttachmentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.UpdateAttachment(c.Request.Context(), &req); err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.Success(c, nil)
}

func (h *Handle) GenerateVariants(c *gin.Context) {
	var req mediadto.VariantsGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	variants, err := h.svc.GenerateVariants(c.Request.Context(), req.ID)
	if err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, variants)
}

func (h *Handle) Download(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("id")), 10, 64)
	if err != nil || id == 0 {
		response.ParamError(c, mediaenums.MsgBadRequest)
		return
	}

	plan, err := h.svc.BuildDownloadPlan(c.Request.Context(), id, response.RequestLanguage(c))
	if err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	writeZipResponse(c, plan)
}

func (h *Handle) DownloadBatch(c *gin.Context) {
	ids, err := parseIDList(c.Query("ids"))
	if err != nil || len(ids) == 0 {
		response.ParamError(c, mediaenums.ErrDownloadEmpty)
		return
	}
	plan, err := h.svc.BuildBatchDownloadPlan(c.Request.Context(), ids, response.RequestLanguage(c))
	if err != nil {
		response.ErrorStatusAuto(c, "media", err)
		return
	}
	writeZipResponse(c, plan)
}

func parseIDList(raw string) ([]uint64, error) {
	var ids []uint64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func writeZipResponse(c *gin.Context, plan *mediadto.DownloadPlan) {

	ascii := sanitizeAsciiFilename(plan.FileName)
	c.Header("Content-Type", "application/zip")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s",
		collapseFilename(ascii), url.PathEscape(plan.FileName)))

	zw := zip.NewWriter(c.Writer)
	defer func() { _ = zw.Close() }()
	for _, entry := range plan.Entries {
		fh := &zip.FileHeader{
			Name:     entry.Name,
			Method:   zip.Deflate,
			Modified: time.Now(),
		}
		w, err := zw.CreateHeader(fh)
		if err != nil {

			return
		}
		if entry.Content != "" {
			if _, err := io.WriteString(w, entry.Content); err != nil {
				return
			}
			continue
		}
		f, err := os.Open(entry.Path)
		if err != nil {

			_, _ = io.WriteString(w, mediaservice.PackageReadFailedText(entry.Name, response.RequestLanguage(c)))
			continue
		}
		_, err = io.Copy(w, f)
		_ = f.Close()
		if err != nil {
			return
		}
	}
}

func sanitizeAsciiFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e {
			b.WriteRune('_')
			continue
		}
		if r == '"' || r == '\\' {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "media_package.zip"
	}
	return out
}

func collapseFilename(name string) string {
	if len(name) <= 120 {
		return name
	}
	ext := name[len(name)-intMin(20, len(name)):]
	return name[:100] + "..." + ext
}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func paramBindFail(c *gin.Context, err error) {
	if err != nil {

		logger.Scene("media").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("media 接口请求绑定失败")
	}
	response.ParamError(c)
}
