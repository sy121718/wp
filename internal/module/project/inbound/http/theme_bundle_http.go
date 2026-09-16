package projecthttp

// theme_bundle_http.go — 主题包导入导出接口（审计 VIS-014）。
//
// 两个出口，挂在既有的 /api/theme 组下（与 035 seed 的 project:theme_* 同一命名空间）：
//
//	GET  /api/theme/export   导出包（二进制 zip 流）
//	POST /api/theme/import   导入包（multipart：file + projectId + 可选 name/createPages/activate）
//
// 导入是 multipart 而不是 JSON：包是二进制，base64 塞 JSON 只会把体积放大 33% 并把
// 「文件太大」的判定推到内存里去。

import (
	"errors"
	"io"
	"net/http"
	"strings"

	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	service "go_wp/internal/module/project/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// themeBundleUploadLimit 上传包的字节上限。
//
// 与 service 侧的 themeBundleMaxZipBytes 同值（64MB）：这里做的是「别把超大文件读进内存」的
// 前置拦截，真正的格式与条目上限判定仍在 service（它才是不受 HTTP 层绕过的判据）。
const themeBundleUploadLimit = 64 << 20

// 路由注册不在这里：两条路径挂在 SetupThemeRoutes 的 /api/theme 组上（同一条鉴权链），
// 本文件只放处理器与错误映射。另起一个 Setup 会重复注册同一路径，Gin 直接 panic。

// ExportBundle 导出主题包（GET /api/theme/export）。
func (h *ThemeHandle) ExportBundle(c *gin.Context) {
	var req projectdto.ThemeBundleExportReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, projectenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ExportThemeBundle(c.Request.Context(), &req)
	if err != nil {
		themeBundleError(c, err)
		return
	}
	c.Header("Content-Disposition", contentDispositionAttachment(res.FileName))
	c.Data(http.StatusOK, "application/zip", res.Bytes)
}

// ImportBundle 导入主题包（POST /api/theme/import，multipart/form-data）。
func (h *ThemeHandle) ImportBundle(c *gin.Context) {
	file, ferr := c.FormFile("file")
	if ferr != nil {
		themeBundleError(c, service.ErrThemeBundleFileRequired)
		return
	}
	if file.Size > themeBundleUploadLimit {
		themeBundleError(c, service.ErrThemeBundleTooLarge)
		return
	}
	reader, oerr := file.Open()
	if oerr != nil {
		themeBundleError(c, service.ErrThemeBundleFileRequired)
		return
	}
	defer func() { _ = reader.Close() }()
	data, rerr := io.ReadAll(io.LimitReader(reader, themeBundleUploadLimit+1))
	if rerr != nil {
		themeBundleError(c, service.ErrThemeBundleFileRequired)
		return
	}
	if len(data) > themeBundleUploadLimit {
		themeBundleError(c, service.ErrThemeBundleTooLarge)
		return
	}

	res, err := h.svc.ImportThemeBundle(c.Request.Context(), &projectdto.ThemeBundleImportReq{
		ProjectID:   c.PostForm("projectId"),
		Name:        c.PostForm("name"),
		CreatePages: parseThemeBundleBool(c.PostForm("createPages")),
		Activate:    parseThemeBundleBool(c.PostForm("activate")),
		FileName:    file.Filename,
		Zip:         data,
	})
	if err != nil {
		themeBundleError(c, err)
		return
	}
	// 媒体缺失时用不同的成功文案：调用方即使只看 message 也知道站点会有引用不到的文件。
	if len(res.Media.Missing) > 0 {
		response.SuccessWithMessage(c, projectenums.MsgThemeBundleImportedPartial, res)
		return
	}
	response.SuccessWithMessage(c, projectenums.MsgThemeBundleImported, res)
}

// themeBundleError 把主题包业务错误映射为响应状态码与文案（文案统一取 enums）。
func themeBundleError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, projectenums.ErrThemeInternal
	switch {
	case errors.Is(err, service.ErrThemeNotFound), errors.Is(err, service.ErrProjectNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, service.ErrThemeBundleAssetMissing):
		status, message = http.StatusNotFound, projectenums.ErrThemeBundleAssetMissing
	case errors.Is(err, service.ErrThemeBundlePortUnavailable):
		// 服务端装配缺失（端口未注入）：这不是调用方的参数问题。
		status, message = http.StatusServiceUnavailable, projectenums.ErrThemeBundlePortUnavailable
	case errors.Is(err, service.ErrThemeBundleFileRequired),
		errors.Is(err, service.ErrThemeBundleFormatUnknown),
		errors.Is(err, service.ErrThemeBundleMissingManifest),
		errors.Is(err, service.ErrThemeBundleManifestInvalid),
		errors.Is(err, service.ErrThemeBundleVersionTooNew),
		errors.Is(err, service.ErrThemeBundleVersionInvalid),
		errors.Is(err, service.ErrThemeBundleUnsafeEntry),
		errors.Is(err, service.ErrThemeBundleTooLarge),
		errors.Is(err, service.ErrThemeBundleTokensInvalid),
		errors.Is(err, service.ErrThemeBundleBlockMissing),
		errors.Is(err, service.ErrThemeBundleBlockCycle),
		errors.Is(err, service.ErrInvalidParam),
		errors.Is(err, service.ErrThemeProjectIDEmpty):
		status, message = http.StatusBadRequest, err.Error()
	default:
		logger.Scene("theme").Error(err, "主题包导入导出失败")
	}
	response.ErrorWithMessage(c, status, message)
}

// parseThemeBundleBool 解析表单布尔值（原生表单只会给 on/1 这类值）。
func parseThemeBundleBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// contentDispositionAttachment 生成 Content-Disposition（文件名只保留安全字符，见 service 侧）。
func contentDispositionAttachment(name string) string {
	target := strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range target {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	plain := b.String()
	if !strings.HasSuffix(plain, ".zip") {
		plain = "theme.skintheme.zip"
	}
	quote := string(rune(0x22))
	return "attachment; filename=" + quote + plain + quote
}
