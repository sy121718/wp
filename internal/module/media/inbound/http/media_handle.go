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
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 媒体模块 HTTP 处理器。
type Handle struct {
	svc mediacontract.MediaService
}

// NewHandle 创建 HTTP 处理器。
func NewHandle(svc mediacontract.MediaService) *Handle {
	return &Handle{svc: svc}
}

// Upload 上传文件。
func (h *Handle) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.ParamError(c, mediaenums.ErrUploadEmpty)
		return
	}

	var categoryID *uint64
	if raw := c.PostForm("category_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err == nil {
			categoryID = &id
		}
	}

	att, err := h.svc.Upload(c.Request.Context(), file, categoryID)
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, att)
}

// List 分页查询附件列表。
func (h *Handle) List(c *gin.Context) {
	var req mediadto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return
	}

	resp, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "media", err)
		return
	}
	response.Success(c, resp)
}

// Detail 查询附件详情。
func (h *Handle) Detail(c *gin.Context) {
	var req mediadto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return
	}

	att, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "media", err)
		return
	}
	response.Success(c, att)
}

// Delete 删除附件。
func (h *Handle) Delete(c *gin.Context) {
	var req mediadto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}

	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, nil)
}

// Replace 换图（POST /api/media/replace，multipart：id + file）。
// 保持 /storage/<id>.<ext> 不变、内容替换、generation+1；被引用不影响换图（只影响删除）。
func (h *Handle) Replace(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("id")), 10, 64)
	if err != nil || id == 0 {
		response.ParamError(c, mediaenums.MsgBadRequest)
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.ParamError(c, mediaenums.ErrUploadEmpty)
		return
	}
	att, err := h.svc.Replace(c.Request.Context(), id, file)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, att)
}

// References 查询附件引用来源（GET /api/media/references?id=1）。
func (h *Handle) References(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("id")), 10, 64)
	if err != nil || id == 0 {
		response.ParamError(c, mediaenums.MsgBadRequest)
		return
	}
	refs, err := h.svc.References(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "media", err)
		return
	}
	response.Success(c, refs)
}

// CategoryTree 获取文件分类树。
func (h *Handle) CategoryTree(c *gin.Context) {
	tree, err := h.svc.CategoryTree(c.Request.Context())
	if err != nil {
		response.ErrorAuto(c, http.StatusInternalServerError, "media", err)
		return
	}
	response.Success(c, tree)
}

// CategoryCreate 新建分类（POST /api/media/category/create）。
func (h *Handle) CategoryCreate(c *gin.Context) {
	var req mediadto.CategoryCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	res, err := h.svc.CreateCategory(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	response.Success(c, res)
}

// CategoryUpdate 更新分类（POST /api/media/category/update）。
func (h *Handle) CategoryUpdate(c *gin.Context) {
	var req mediadto.CategoryUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	if err := h.svc.UpdateCategory(c.Request.Context(), &req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	response.Success(c, nil)
}

// CategoryDelete 删除分类（POST /api/media/category/delete）。
func (h *Handle) CategoryDelete(c *gin.Context) {
	var req mediadto.CategoryDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), &req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	response.Success(c, nil)
}

// UpdateAttachment 更新附件元数据（POST /api/media/update）。
func (h *Handle) UpdateAttachment(c *gin.Context) {
	var req mediadto.AttachmentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	if err := h.svc.UpdateAttachment(c.Request.Context(), &req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	response.Success(c, nil)
}

// GenerateVariants 同步生成/重新生成指定附件的全部变体（POST /api/media/variants/generate）。
// 返回生成后的变体状态列表（含 failed 降级结果），前端据此刷新徽标。
func (h *Handle) GenerateVariants(c *gin.Context) {
	var req mediadto.VariantsGenerateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		paramBindFail(c, err)
		return
	}
	variants, err := h.svc.GenerateVariants(c.Request.Context(), req.ID)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	response.SuccessWithMessage(c, mediaenums.MsgSuccess, variants)
}

// Download 单图资源包下载（GET /api/media/download?id=1）。
// zip 内部分目录 original/、webp/、thumb/、medium/（变体未 ready 的目录为 README.txt）。
func (h *Handle) Download(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("id")), 10, 64)
	if err != nil || id == 0 {
		response.ParamError(c, mediaenums.MsgBadRequest)
		return
	}
	// 产物语言 = 请求语言：README.txt 会跟着 zip 落到用户机器上。
	plan, err := h.svc.BuildDownloadPlan(c.Request.Context(), id, response.RequestLanguage(c))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	writeZipResponse(c, plan)
}

// DownloadBatch 多图资源包打包下载（GET /api/media/download/batch?ids=1,2,3）。
// 每图一个 <文件名>_<id>/ 子文件夹，子文件夹内同四目录。
func (h *Handle) DownloadBatch(c *gin.Context) {
	ids, err := parseIDList(c.Query("ids"))
	if err != nil || len(ids) == 0 {
		response.ParamError(c, mediaenums.ErrDownloadEmpty)
		return
	}
	plan, err := h.svc.BuildBatchDownloadPlan(c.Request.Context(), ids, response.RequestLanguage(c))
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "media", err)
		return
	}
	writeZipResponse(c, plan)
}

// parseIDList 解析 Query ids=1,2,3 为 uint64 列表（忽略非法段）。
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

// writeZipResponse 把打包计划流式写为 zip 响应：
// archive/zip.NewWriter 直挂 c.Writer（不落盘临时文件），文件内容用 io.Copy 逐条拷贝。
func writeZipResponse(c *gin.Context, plan *mediadto.DownloadPlan) {
	// Content-Disposition：ASCII fallback + RFC 5987 filename*（中文文件名兼容）。
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
			// 客户端断开等写失败：响应已开始，无法回写 JSON 错误，终止即可。
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
			// 单文件缺失：写入占位说明，不阻断整包。
			// 命名占位符 + 显式替换：词条里出现裸 % 也不会被当成格式化动词。
			text, ok := i18n.FillNamedPlaceholders(
				i18n.Translate(mediaenums.PackageReadFailed, "文件读取失败： {name}\n", response.RequestLanguage(c)),
				map[string]string{"name": entry.Name})
			if !ok {
				text = "文件读取失败： " + entry.Name + "\n"
			}
			_, _ = io.WriteString(w, text)
			continue
		}
		_, err = io.Copy(w, f)
		_ = f.Close()
		if err != nil {
			return
		}
	}
}

// sanitizeAsciiFilename 生成 Content-Disposition 的 ASCII fallback 文件名：
// 非 ASCII 字符替换为下划线，控制字符剔除。
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

// collapseFilename 收敛 ASCII fallback 的长度（超长截断，保留扩展名）。
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

// paramBindFail 请求绑定失败的统一出口（400 + 受控文案）。
//
// 不把绑定错误原文拼进响应：gin 的绑定错误会带上 Go 结构体与字段名
// （如 `json: cannot unmarshal string into Go struct field CreateReq.title of type string`），
// 那是实现细节 —— 对外只说「参数不合法」，原文进日志供排障。
//
// 原先这里是 `response.ParamError(c, err.Error())`。它一直待在门禁盲区里：形态 ① 只认
// `*.ErrorWithMessage(` 与 `c.String(`，而 `ParamError` 是同一个包里的同族出口却不在判据里。
// 2026-09 第三批把 ParamError 纳入判据后，本模块这几处立刻被扫出来 —— 判据是**形状**，
// 不是字面量；同族出口漏一个就等于那一族都没管住。
func paramBindFail(c *gin.Context, err error) {
	if err != nil {
		// 绑定失败是客户端输入问题，按 warn 记（不污染错误日志）。
		logger.Scene("media").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("media 接口请求绑定失败")
	}
	response.ParamError(c)
}
