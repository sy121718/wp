package dashboardhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	"go_wp/pkg/datarule"

	"github.com/gin-gonic/gin"
)

// admin_datarule_handle.go - 后台数据权限规则页（列表 / 编辑页 / 增删改）。

// DatarulesPage 数据权限列表页（GET /admin/datarules）。
// 列表 + 新建（domain 下拉来自已注册数据域）+ 删除；编辑走独立 /edit?id= 页（detail 回显，配置复杂）。
func (h *Handle) DatarulesPage(c *gin.Context) {
	res, err := h.rules.RuleList(c.Request.Context(), &admindto.RuleListReq{Page: 1, Limit: 100})
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgAdminGenericFailed)
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	c.HTML(http.StatusOK, "admin/datarules", withCSRF(c, gin.H{
		"title":   dashboardenums.MsgDatarulesTitle,
		"menu":    "datarules",
		"Rows":    res.List,
		"Total":   res.Total,
		"Domains": domains,
	}))
}

// DatarulesCreate 新建数据规则（POST /admin/datarules/create）。
// config 为可选 JSON 文本（RuleConfig；缺省给空配置 {}）。
func (h *Handle) DatarulesCreate(c *gin.Context) {
	ruleName := fieldValue(c, "rule_name")
	domain := fieldValue(c, "domain")
	if ruleName == "" || domain == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	config, err := configFromJSON(fieldValue(c, "config"))
	if err != nil {
		c.String(http.StatusBadRequest, "配置 JSON 不合法")
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: config,
		Status: parseStatus(c.PostForm("status")), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesEditPage 数据规则编辑页（GET /admin/datarules/edit?id=X）。
// 从 RuleDetail 回显 rule_name/domain/status/remark 与 config JSON。
func (h *Handle) DatarulesEditPage(c *gin.Context) {
	id := parseUint(c.Query("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil || detail == nil {
		c.String(http.StatusNotFound, "数据规则不存在")
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	configJSON, err := configToJSON(detail.Config)
	if err != nil {
		configJSON = ""
	}
	c.HTML(http.StatusOK, "admin/datarule_edit", withCSRF(c, gin.H{
		"title":   dashboardenums.MsgDatarulesTitle,
		"menu":    "datarules",
		"Detail":  detail,
		"Domains": domains,
		"Config":  configJSON,
	}))
}

// DatarulesUpdate 保存数据规则（POST /admin/datarules/update）。
// config 为可选字段：列表抽屉表单不含该字段（列表 dto 无 config），此时沿用库中原配置，
// 避免「只改基础字段」把规则配置清空；显式提交 config（含空串）仍按提交值处理。
func (h *Handle) DatarulesUpdate(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	ruleName := fieldValue(c, "rule_name")
	domain := fieldValue(c, "domain")
	if id == 0 || ruleName == "" || domain == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	config, err := configFromJSON(fieldValue(c, "config"))
	if err != nil {
		c.String(http.StatusBadRequest, "配置 JSON 不合法")
		return
	}
	if _, ok := c.GetPostForm("config"); !ok {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil || detail == nil {
			adminWriteFailed(c, detailErr)
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: parseStatus(c.PostForm("status")), Remark: fieldValue(c, "remark"),
	}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// DatarulesDelete 删除数据规则（POST /admin/datarules/delete）。
func (h *Handle) DatarulesDelete(c *gin.Context) {
	id := parseUint(c.PostForm("id"))
	if id == 0 {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		adminWriteFailed(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/datarules")
}

// configToJSON 序列化 RuleConfig 为缩进 JSON 文本（编辑回显）。
func configToJSON(cfg datarule.RuleConfig) (string, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// configFromJSON 解析表单配置 JSON 文本为 RuleConfig；空文本返回空配置。
func configFromJSON(s string) (datarule.RuleConfig, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return datarule.RuleConfig{}, nil
	}
	var cfg datarule.RuleConfig
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		return datarule.RuleConfig{}, err
	}
	return cfg, nil
}
