package adminhttp

// admin_pages_datarule.go — 数据权限域管理页：规则列表、新建、编辑页与保存、单条与批量删除（/admin/datarules）。

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
)

// --- 数据权限 datarules ---

// DatarulesPage 数据权限列表页（GET /admin/datarules）。
// 列表 + 新建（domain 下拉来自已注册数据域）+ 删除；编辑走独立 /edit?id= 页（detail 回显，配置复杂）。
//
// 服务端筛选：domain 进 SQL（与分页同源）；回显键名对齐模板 datarules.html 的
// value="{{.["FilterDomain"]}}"。注意这是**精确匹配**（service 侧 `domain = ?`）：
// 输入的真实域值必须与 sys_data_rule.domain 完全一致（如 ADMIN），
// 部分串不会命中 —— 改动匹配语义在 service，不在本页。
func (h *AdminPagesHandle) DatarulesPage(c *gin.Context) {
	page, limit := shell.PageParams(c)
	domain := strings.TrimSpace(c.Query("domain"))
	res, err := h.rules.RuleList(c.Request.Context(), &admindto.RuleListReq{
		Page: page, Limit: limit, Domain: domain,
	})
	if err != nil {
		res = &admindto.RuleListResp{}
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	data := shell.Prepare(c, gin.H{
		"title":        pagesMsgDatarulesTitle,
		"menu":         "datarules",
		"Rows":         res.List,
		"Total":        res.Total,
		"Domains":      domains,
		"FilterDomain": domain,
		"Err":          adminErrOrLoad(c, err),
		"Done":         adminPageDone(c, c.Query("done")),
	})
	base := shell.FilterBaseURL("/admin/datarules", map[string]string{"domain": domain})
	for k, v := range shell.BuildPagination(res.Total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/system/datarules", data)
}

// DatarulesCreate 新建数据规则（POST /admin/datarules/create）。
//
// 创建只收基础字段：规则配置在编辑页按该数据域的白名单逐项填写 —— 列表页的抽屉表单是
// 静态模板、拿不到白名单，让它在没有字段清单的情况下收配置等于把 JSON 换了个地方手写。
func (h *AdminPagesHandle) DatarulesCreate(c *gin.Context) {
	ruleName := shell.FieldValue(c, "rule_name")
	domain := shell.FieldValue(c, "domain")
	if ruleName == "" || domain == "" {
		adminPageParamFail(c, "/admin/datarules")
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: admindto.RuleConfigDTO{},
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, "/admin/datarules", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesEditPage 数据规则编辑页（GET /admin/datarules/edit?id=X）。
// 从 RuleDetail 回显 rule_name/domain/status/remark 与 config JSON。
func (h *AdminPagesHandle) DatarulesEditPage(c *gin.Context) {
	id := shell.ParseUint(c.Query("id"))
	if id == 0 {
		// 页面请求的失败出口是**页面**：303 回列表页并把原因经 ?err= 回带（读侧
		// adminPageErrText 白名单放行）。原先是 c.String(400, pagesMsgFieldRequired) ——
		// 响应体是 i18n 的 key 本身（用户看到内部标识符），而且脱离页壳。
		// 走与其它写 handler 同一个参数级出口（文案取 MsgBadRequest 的译文，在白名单里）。
		adminPageParamFail(c, "/admin/datarules")
		return
	}
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil || detail == nil {
		// id 存在但查不到（已被别人删掉 / 不在本工程作用域）：与上面「缺 id」同一形状的失败 ——
		// 也不能直出裸文本。原先是 c.String(404, "数据规则不存在")：响应体是那句中文、
		// **没有页壳**，用户在编辑页上点了半天链接后落到一个纯文本页面。
		// 文案取 adminenums.ErrRuleNotFound（在 AdminFacingMessages 白名单里，读侧候选天然覆盖）。
		adminPageWriteFail(c, "/admin/datarules", errors.New(adminenums.ErrRuleNotFound))
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	// 条件编辑器按该数据域的白名单渲染（字段 / 操作符下拉都来自域声明）。
	editor := h.dataruleEditorContext(c, detail.Domain, detail.Config)
	c.HTML(http.StatusOK, "admin/system/datarule_edit", shell.Prepare(c, gin.H{
		"title":   pagesMsgDatarulesTitle,
		"menu":    "datarules",
		"Detail":  detail,
		"Domains": domains,
		"Editor":  editor,
		// 错误槽位：保存失败会 303 回本页并带 ?err=（见 DatarulesUpdate 的分流），
		// 这一页必须能把它渲染出来 —— 否则用户看到的是「点了保存、页面刷新了一下、
		// 什么都没发生」，比回到列表页更难判断。
		"Err": adminPageErrText(c, c.Query("err")),
	}))
}

// adminDataruleBackURL /admin/datarules/update 的失败回跳目标：按提交**来源**分流。
//
// 这是本域唯一「一个端点两个入口」的写操作 —— 列表页的抽屉表单（datarule.html，只改
// rule_name/domain/status/remark）与编辑页的完整表单（datarule_edit.html，还带条件配置）
// 都 POST 到 /admin/datarules/update。分流依据**本来就有**：编辑页提交时带
// dataruleEditorMarker（config_editor 隐藏域，见 datarule_config_form.go 的表单约定），
// 抽屉不带 —— 用它判来源，不新增协议、不猜别的字段。
//
// 缺 id（0）时没有可回的编辑页（它由 id 决定），一律回列表页。
func adminDataruleBackURL(c *gin.Context, id uint64) string {
	if id == 0 || c.PostForm(dataruleEditorMarker) == "" {
		return "/admin/datarules"
	}
	return fmt.Sprintf("/admin/datarules/edit?id=%d", id)
}

// DatarulesUpdate 保存数据规则（POST /admin/datarules/update）。
//
// 配置来源按提交内容判定：带编辑器标记的（编辑页）从表单重建配置，否则沿用库中原配置 ——
// 列表抽屉只改基础字段，不该把规则配置清空。空行与空条件组在这里被清掉：字段为空的行会被
// 引擎直接丢弃、值为空的行会变成 field = ”，两种都会让界面上的条数与实际生效的条数对不上。
func (h *AdminPagesHandle) DatarulesUpdate(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	ruleName := shell.FieldValue(c, "rule_name")
	domain := shell.FieldValue(c, "domain")
	// 失败出口回**来源页**（判定依据见 adminDataruleBackURL）：编辑页提交就回编辑页，
	// 抽屉提交就回列表页 —— 编辑页那份表单填一次成本很高，甩回列表页等于让他重填。
	back := adminDataruleBackURL(c, id)
	if id == 0 || ruleName == "" || domain == "" {
		adminPageParamFail(c, back)
		return
	}
	config := admindto.RuleConfigDTO{}
	if c.PostForm(dataruleEditorMarker) != "" {
		config = dataruleDropEmptyGroups(dataruleConfigFromForm(c))
	} else {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil {
			adminPageWriteFail(c, back, detailErr)
			return
		}
		if detail == nil {
			// 「查不到但也没报错」必须给一条可行动的业务文案（规则不存在 / 不在本工程作用域内），
			// 不能像修复前那样拿 nil 去调错误出口（旧出口对 nil 直接 return → 200 空体，
			// 前端看到「点了没反应」而日志里什么都没有）。
			adminPageWriteFail(c, back, errors.New(adminenums.ErrRuleNotFound))
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: shell.ParseStatus(c.PostForm("status")), Remark: shell.FieldValue(c, "remark"),
	}); err != nil {
		adminPageWriteFail(c, back, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesDelete 删除数据规则（POST /admin/datarules/delete）。
func (h *AdminPagesHandle) DatarulesDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPageParamFail(c, "/admin/datarules")
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		adminPageWriteFail(c, "/admin/datarules", err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesBulkDelete 批量删除数据规则（POST /admin/datarules/bulk-delete）。
//
// 逐条走同一条单条删除路径（含该规则的分配记录清理）；单条失败只计数不中断整批。
func (h *AdminPagesHandle) DatarulesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是受控错误（理由见 adminBulkResultURL 上方）：文案走 shell 的受控出口，不直传原文。
		c.Redirect(http.StatusSeeOther, "/admin/datarules?err="+url.QueryEscape(shell.BulkIDsFacingText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	c.Redirect(http.StatusSeeOther, adminBulkResultURL(c, "/admin/datarules", adminBulkNounDatarule, deleted, skipped))
}
