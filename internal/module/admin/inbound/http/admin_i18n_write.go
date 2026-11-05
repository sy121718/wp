package adminhttp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"
	"gorm.io/gorm"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/view"
)

var adminI18nHandleInst = NewAdminI18nEntryHandle()

func adminI18nHandle() *adminI18nEntryHandle { return adminI18nHandleInst }

// I18nEntryWrite 绑定页面表单，调用词库端口并交 Jet 输出操作结果。
func (h *adminI18nEntryHandle) I18nEntryWrite(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.I18nWriteReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		view.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	if err := req.Validate(); err != nil {
		view.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	switch req.Action {
	case "create", "update":
		h.i18nWriteUpsert(c, req)
	case "delete":
		h.i18nWriteDelete(c, req)
	case "bulkDelete":
		h.i18nWriteBulkDelete(c, req)
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

// i18nWriteBulkDelete 先校验完整标识集合，再调用词库的原子批量删除端口。
func (h *adminI18nEntryHandle) i18nWriteBulkDelete(c *gin.Context, req admindto.I18nWriteReq) {
	ids, err := bulkids.Normalize(req.IDs)
	if err != nil {
		view.FailScene(c, adminErrScene, err)
		return
	}
	refs := make([]i18n.EntryRef, 0, len(ids))
	for _, raw := range ids {
		key, lang, ok := strings.Cut(strings.TrimSpace(raw), adminI18nEntryKeySeparator)
		key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
		if !ok || key == "" || lang == "" {
			view.FailStatus(c, http.StatusBadRequest, adminErrScene, fmt.Errorf("词条标识 %q 不是 key%slang 形状", raw, adminI18nEntryKeySeparator))
			return
		}
		refs = append(refs, i18n.EntryRef{Key: key, Lang: lang})
	}
	deleted, err := i18n.DeleteEntries(c.Request.Context(), refs)
	if err != nil {
		view.FailScene(c, adminErrScene, adminI18nBulkDeleteError(err))
		return
	}
	h.markI18nStale(c)
	c.HTML(http.StatusOK, "admin/system/i18n_write_success.html", struct{ Deleted int }{Deleted: deleted})
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
