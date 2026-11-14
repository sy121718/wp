package adminhttp

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/view"
)

var adminI18nHandleInst = NewAdminI18nEntryHandle()

func adminI18nHandle() *adminI18nEntryHandle { return adminI18nHandleInst }

// I18nEntryWrite 绑定页面表单，调用词库端口并交 Jet 输出操作结果。
func (h *adminI18nEntryHandle) I18nEntryWrite(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[admindto.I18nWriteReq](c)
	if err != nil {
		view.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	if err := req.Validate(); err != nil {
		view.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	switch req.Action {
	case "create", "update":
		h.i18nWriteUpsert(c, *req)
	case "delete":
		h.i18nWriteDelete(c, *req)
	case "bulkDelete":
		h.i18nWriteBulkDelete(c, *req)
	}
}

func (h *adminI18nEntryHandle) i18nWriteUpsert(c *gin.Context, req admindto.I18nWriteReq) {
	entry := i18n.Entry{
		Key: req.Key, Lang: req.Lang, Value: req.Value,
		Category: req.Category, Remark: req.Remark, Status: 1,
	}
	var err error
	if req.Action == "update" {
		err = i18n.UpdateEntry(c.Request.Context(), entry)
	} else {
		err = i18n.SaveEntry(c.Request.Context(), entry)
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			view.FailStatus(c, http.StatusNotFound, adminErrScene, adminenums.BadRequest())
			return
		}
		view.FailScene(c, adminErrScene, err)
		return
	}
	h.markI18nStale(c)
	view.Success(c)
}

func (h *adminI18nEntryHandle) i18nWriteDelete(c *gin.Context, req admindto.I18nWriteReq) {
	if err := i18n.DeleteEntry(c.Request.Context(), req.Key, req.Lang); err != nil {
		view.FailScene(c, adminErrScene, err)
		return
	}
	h.markI18nStale(c)
	view.Success(c)
}

// i18nWriteBulkDelete 先校验数量上限，再逐条校验**原始提交**的复合标识，
// 最后一次性交给词库的原子批量删除端口。
//
// 上限沿用 bulkids.Normalize 的去重口径；但形状校验必须遍历原始 ids：
// Normalize 会静默滤掉空项，只依赖它等于把「空项」变成一次成功删除。
// 空项与非 key|lang 形状同属「请求本身不可执行」，整批拒绝、一条都不写。
func (h *adminI18nEntryHandle) i18nWriteBulkDelete(c *gin.Context, req admindto.I18nWriteReq) {
	if _, err := bulkids.Normalize(req.IDs); err != nil {
		view.FailScene(c, adminErrScene, err)
		return
	}
	refs := make([]i18n.EntryRef, 0, len(req.IDs))
	seen := make(map[i18n.EntryRef]struct{}, len(req.IDs))
	for _, raw := range req.IDs {
		key, lang, ok := strings.Cut(strings.TrimSpace(raw), adminI18nEntryKeySeparator)
		key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
		// 复合标识只能是恰好一段 key + 一段 lang；lang 里再出现分隔符说明提交的不是二元组。
		if !ok || key == "" || lang == "" || strings.Contains(lang, adminI18nEntryKeySeparator) {
			// 只给一句参数错误：坏标识本身不回显进响应。
			view.FailStatus(c, http.StatusBadRequest, adminErrScene, adminenums.BadRequest())
			return
		}
		item := i18n.EntryRef{Key: key, Lang: lang}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		refs = append(refs, item)
	}
	if len(refs) == 0 {
		view.FailScene(c, adminErrScene, adminenums.Err(adminenums.ErrI18nSelectionEmpty))
		return
	}
	if _, err := i18n.DeleteEntries(c.Request.Context(), refs); err != nil {
		view.FailScene(c, adminErrScene, adminI18nBulkDeleteError(err))
		return
	}
	h.markI18nStale(c)
	// 纯成功只给公共一句提示：删除条数属明细，不进入成功消息。
	view.Success(c)
}

// adminI18nBulkDeleteError 把词库层的缺失事实映射为模块错误声明。
func adminI18nBulkDeleteError(err error) error {
	var missing *i18n.MissingEntriesError
	if errors.As(err, &missing) {
		refs := make([]adminenums.I18nEntryRef, 0, len(missing.Items))
		for _, m := range missing.Items {
			refs = append(refs, adminenums.I18nEntryRef{Key: m.Key, Lang: m.Lang})
		}
		return adminenums.MissingI18nEntries(refs)
	}
	if errors.Is(err, i18n.ErrI18nSelectionEmpty) {
		return adminenums.Err(adminenums.ErrI18nSelectionEmpty)
	}
	return err
}
