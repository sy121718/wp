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
	"context"
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
//
// 保存成功后按「清单内容是否变化」决定是否标记全站待重建（见
// markPagesStaleForLocaleChange）：切换器链接与 hreflang 是构建期写进产物字节的，
// 清单变了不重建，前台看不出变化（docs/06-D §15.9 遗留第 1 条）。
func (h *Handle) SaveSiteLocales(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if projectID == "" {
		c.String(http.StatusBadRequest, "工程不能为空")
		return
	}
	// 变更前清单（project 契约的规范输出）：仅用于「是否变化」判定。
	// 读失败不阻断保存：before 为空切片，与保存结果比较必然判定为变化，保守触发重建。
	before, err := h.projects.ListLocales(c.Request.Context(), projectID)
	if err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "读取语言清单失败，按已变更处理")
	}
	rows := parseLocaleRows(c)
	req := &projectdto.LocalesSaveReq{ProjectID: projectID, Locales: make([]projectdto.LocaleItem, 0, len(rows))}
	for _, r := range rows {
		enabled := r.Enabled
		req.Locales = append(req.Locales, projectdto.LocaleItem{
			Lang: r.Lang, IsDefault: r.IsDefault, Enabled: &enabled,
		})
	}
	after, err := h.projects.SaveLocales(c.Request.Context(), req)
	if err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "保存语言清单失败")
		data := h.buildSiteSettingsData(c, projectID)
		data.Locales = rows // 回显用户输入，便于就地修正
		data.LocaleError = dashboardenums.MsgSiteLocalesInvalid
		c.HTML(http.StatusOK, "admin/settings", withCSRF(c, data.templateMap()))
		return
	}
	h.markPagesStaleForLocaleChange(c.Request.Context(), projectID, before, after)
	c.Redirect(http.StatusSeeOther, "/admin/settings?project="+projectID+"&locales_saved=1")
}

// markPagesStaleForLocaleChange 语言清单内容确实变化后，把全站页面标记为待重建。
//
// 为什么落在 dashboard handler（而不是 project 模块）：语言清单归 project，
// 「全站标记待重建」的能力归 page，且依赖方向是 page → project；让 project 反向
// 依赖 page 会成环。dashboard 同时持有 project 与 page 两个契约，本就是本项目既有的
// 跨模块编排点（theme_handle 的整站换皮、block_handle 的 stale 传播同理），
// 因此沿用「handler 编排 + 双方契约」的做法，不新增回调/事件机制。
//
// 触发条件：before/after 按「构建可见内容」比较不等（语言码集合与顺序、默认标记、
// 启用状态）。单语言站点原样再保存一次清单内容不变 → 不触发，避免无意义的全站重建。
//
// 失败只记日志：清单已落库（不因标记失败而回滚），下次保存会重新判定并再试。
func (h *Handle) markPagesStaleForLocaleChange(ctx context.Context, projectID string, before, after []projectdto.LocaleResp) {
	if localesEqual(before, after) {
		return
	}
	if h.pages == nil {
		return
	}
	if err := h.pages.MarkStaleForI18n(ctx); err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "语言清单变更后标记全站待重建失败")
	}
}

// localesEqual 比较两次语言清单是否「构建可见」一致。
//
// 以 project.ListLocales 的规范输出为准（默认语言在前，其余按 sort_order、语言升序）：
// 该顺序正是构建期 EnabledLangs 的取用顺序，也就是产物里语言切换器与 hreflang 的顺序。
// 逐项比较语言码 + 默认标记 + 启用状态，任一不同即视为清单变化。
func localesEqual(a, b []projectdto.LocaleResp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Lang != b[i].Lang || a[i].IsDefault != b[i].IsDefault || a[i].Enabled != b[i].Enabled {
			return false
		}
	}
	return true
}
