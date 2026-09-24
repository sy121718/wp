package navigationhttp

import (
	"net/http"
	"strings"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// navEditEcho is the submitted edit state, separate from the current database row.
type navEditEcho struct {
	ID, ProjectID, Kind, Title, Path, Target, UpdatedAt string
	PanelBlockID, PanelWidth                            string
}

func (h *navigationPageHandle) NavigationEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("id"))
	projectID := strings.TrimSpace(c.Query("project"))
	kind := strings.TrimSpace(c.Query("kind"))
	if id == "" || projectID == "" || kind != normalizeNavKind(kind) {
		c.String(http.StatusBadRequest, "%s", navInvalidParamText(c))
		return
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: id})
	if err != nil {
		if err.Error() == navigationenums.ErrNotFound {
			c.String(http.StatusNotFound, "%s", navigationErrPageText(c, err))
			return
		}
		logger.Scene("page").With("id", id).Error(err, "加载导航编辑片段失败")
		c.String(http.StatusInternalServerError, "%s", shell.PageInternalText(c))
		return
	}
	if item == nil || item.ProjectID != projectID || item.Kind != kind {
		c.String(http.StatusNotFound, "%s", shell.TranslateFor(c)(navigationenums.ErrNotFound, "菜单项不存在"))
		return
	}
	panelID := ""
	if item.PanelBlockID != nil {
		panelID = *item.PanelBlockID
	}
	h.renderNavigationEdit(c, navEditEcho{
		ID: item.ID, ProjectID: projectID, Kind: kind, Title: item.Title,
		Path: item.Path, Target: item.Target, UpdatedAt: item.UpdatedAt,
		PanelBlockID: panelID, PanelWidth: item.PanelWidth,
	}, "")
}

func (h *navigationPageHandle) renderNavigationEdit(c *gin.Context, echo navEditEcho, errorText string) {
	c.Header("Cache-Control", "no-store")
	blocks := make([]panelBlockOption, 0)
	panelAvail := h.blocks != nil
	if panelAvail {
		list, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: echo.ProjectID})
		if err != nil {
			logger.Scene("page").With("project", echo.ProjectID).Error(err, "加载导航面板块候选失败")
			panelAvail = false
		} else {
			for _, b := range list {
				blocks = append(blocks, panelBlockOption{ID: b.ID, Name: b.Name})
			}
		}
	}
	data := gin.H{"NavEditEcho": echo, "NavEditError": errorText, "NavEditPanelAvail": panelAvail, "NavEditPanelBlocks": blocks}
	// A fragment has no page shell: supply only translation and session CSRF data.
	tr := shell.TranslateFor(c)
	data["t"] = tr
	token, err := builtin.GetCSRFToken(c)
	if err != nil || token == "" {
		logger.Scene("page").With("id", echo.ID).Error(err, "加载导航编辑 CSRF token 失败")
		c.String(http.StatusInternalServerError, "%s", shell.PageInternalText(c))
		return
	}
	data["csrf_token"] = token
	c.HTML(http.StatusOK, "admin/navigation/navigation_edit_form", data)
}

func navEditHTMX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

func navEditRedirect(c *gin.Context, target string) {
	if navEditHTMX(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusSeeOther, target)
}

func (h *navigationPageHandle) navEditFailure(c *gin.Context, err error, echo navEditEcho) {
	if !navEditHTMX(c) {
		c.Redirect(http.StatusSeeOther, navListURLMenu(echo.ProjectID, echo.Kind, echo.ID, navigationErrPageText(c, err), ""))
		return
	}
	if echo.ID == "" || echo.ProjectID == "" || echo.Kind != normalizeNavKind(echo.Kind) {
		c.Header("HX-Redirect", navListURLMenu(echo.ProjectID, echo.Kind, "", navInvalidParamText(c), ""))
		c.Status(http.StatusOK)
		return
	}
	// Empty submitted values are intentional; a stale database row must never
	// overwrite user input after a failed optimistic-lock update.
	h.renderNavigationEdit(c, echo, navigationErrPageText(c, err))
}

// navEditOwnedRow checks the submitted context before a write addressed by id alone.
func (h *navigationPageHandle) navEditOwnedRow(c *gin.Context, echo navEditEcho) bool {
	if echo.ID == "" || echo.ProjectID == "" || echo.Kind != normalizeNavKind(echo.Kind) {
		c.String(http.StatusBadRequest, "%s", navInvalidParamText(c))
		return false
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: echo.ID})
	if err != nil && err.Error() != navigationenums.ErrNotFound {
		logger.Scene("page").With("id", echo.ID).Error(err, "核对导航编辑归属失败")
		c.String(http.StatusInternalServerError, "%s", shell.PageInternalText(c))
		return false
	}
	if item == nil || item.ProjectID != echo.ProjectID || item.Kind != echo.Kind {
		c.String(http.StatusNotFound, "%s", shell.TranslateFor(c)(navigationenums.ErrNotFound, "菜单项不存在"))
		return false
	}
	return true
}
