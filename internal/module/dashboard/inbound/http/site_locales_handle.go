package dashboardhttp

// site_locales_handle.go — 站点语言清单管理（多语言 P3，docs/06-D §14 D10）。
//
// 入口：站点设置页的「语言」分组（/admin/settings）。语言清单按工程维度存储，
// 与站点名/简介同属一个工程设置面，故不新开独立页面（也避免多一个侧栏菜单与权限点）。
//
// 契约复用：读写一律经 project 模块既有契约 ListLocales / SaveLocales，
// 本模块不写第二套校验（「至少一种语言、至多一个默认且默认必须启用、语言码白名单」
// 在 project/service/locale_service.go 单点实现）。
//
// 交互（HTMX + Jet 服务端渲染，遵守既有后台页面写法）：
//   - 行片段 POST /admin/settings/locales/rows：增/删一行后由服务端重渲染行片段
//     （hx-target="#locale-rows"），未落库——保存仍由整表提交触发；
//   - 全量保存 POST /admin/settings/locales/save：普通表单 POST + 303 回跳（PRG），
//     与同页「保存设置」一致。
//
// 行的身份用「提交顺序下标」而不是语言码：用户可以在表单里直接改语言码，
// 若用语言码做 radio/checkbox 的 value，改码后默认/启用勾选会静默丢失。

import (
	"net/http"
	"strconv"
	"strings"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// localeRow 语言清单一行（页面编辑态）。
type localeRow struct {
	Lang      string
	IsDefault bool
	Enabled   bool
}

// localeRowsOf 读取工程语言清单并投影为编辑行（读失败按空清单处理，页面仍可用）。
func (h *Handle) localeRowsOf(c *gin.Context, projectID string) []localeRow {
	if h.projects == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	rows, err := h.projects.ListLocales(c.Request.Context(), projectID)
	if err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "读取语言清单失败")
		return nil
	}
	out := make([]localeRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, localeRow{Lang: r.Lang, IsDefault: r.IsDefault, Enabled: r.Enabled})
	}
	return out
}

// parseLocaleRows 从表单解析语言行：
//
//	langs（按提交顺序的多值字段）+ defaultIndex（单个下标）+ enabledIndex（多值下标）。
//
// 下标越界/非法一律忽略，绝不 panic；语言码只做去空格，白名单校验交给 project 契约。
func parseLocaleRows(c *gin.Context) []localeRow {
	langs := c.PostFormArray("langs")
	if len(langs) == 0 {
		return nil
	}
	enabled := map[int]bool{}
	for _, raw := range c.PostFormArray("enabledIndex") {
		if i, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && i >= 0 {
			enabled[i] = true
		}
	}
	defaultIdx := -1
	if raw := strings.TrimSpace(c.PostForm("defaultIndex")); raw != "" {
		if i, err := strconv.Atoi(raw); err == nil && i >= 0 && i < len(langs) {
			defaultIdx = i
		}
	}
	rows := make([]localeRow, 0, len(langs))
	for i, lang := range langs {
		rows = append(rows, localeRow{
			Lang:      strings.TrimSpace(lang),
			IsDefault: i == defaultIdx,
			Enabled:   enabled[i],
		})
	}
	return rows
}

// LocaleRowsFragment POST /admin/settings/locales/rows（HTMX 片段）。
//
// action=add：把 newLang 追加为新行（默认不启用、不默认，避免误改默认语言）；
// action=remove：按 removeIndex 删除该行；删除默认语言行时把默认标记顺延给
// 第一个仍启用的行（保证片段里不会出现「无默认语言」的中间态）。
func (h *Handle) LocaleRowsFragment(c *gin.Context) {
	rows := parseLocaleRows(c)
	switch strings.TrimSpace(c.PostForm("action")) {
	case "add":
		lang := strings.TrimSpace(c.PostForm("newLang"))
		rows = append(rows, localeRow{Lang: lang, Enabled: true})
	case "remove":
		if idx, err := strconv.Atoi(strings.TrimSpace(c.PostForm("removeIndex"))); err == nil && idx >= 0 && idx < len(rows) {
			removedDefault := rows[idx].IsDefault
			rows = append(rows[:idx:idx], rows[idx+1:]...)
			if removedDefault {
				for i := range rows {
					rows[i].IsDefault = false
				}
				for i := range rows {
					if rows[i].Enabled {
						rows[i].IsDefault = true
						break
					}
				}
			}
		}
	}
	c.HTML(http.StatusOK, "admin/partials/locale_rows", withCSRF(c, gin.H{"Locales": rows}))
}

// SaveSiteLocales POST /admin/settings/locales/save：全量保存站点语言清单。
//
// 校验（全部在 project.SaveLocales 内单点实现）：至少一种语言、至多一个默认语言
// 且默认语言必须启用、语言码白名单。校验失败时不落库，回渲染设置页并给出提示。
func (h *Handle) SaveSiteLocales(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if projectID == "" {
		c.String(http.StatusBadRequest, "工程不能为空")
		return
	}
	rows := parseLocaleRows(c)
	req := &projectdto.LocalesSaveReq{ProjectID: projectID, Locales: make([]projectdto.LocaleItem, 0, len(rows))}
	for _, r := range rows {
		enabled := r.Enabled
		req.Locales = append(req.Locales, projectdto.LocaleItem{
			Lang: r.Lang, IsDefault: r.IsDefault, Enabled: &enabled,
		})
	}
	if _, err := h.projects.SaveLocales(c.Request.Context(), req); err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "保存语言清单失败")
		data := h.buildSiteSettingsData(c, projectID)
		data.Locales = rows // 回显用户输入，便于就地修正
		data.LocaleError = dashboardenums.MsgSiteLocalesInvalid
		c.HTML(http.StatusOK, "admin/settings", withCSRF(c, data.templateMap()))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/settings?project="+projectID+"&locales_saved=1")
}
