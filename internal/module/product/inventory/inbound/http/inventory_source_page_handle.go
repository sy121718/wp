// inventory_source_page_handle.go — 后台货源管理页（issue #17）。
//
// 与库存管理页同一模式：GET 渲染完整页，POST 处理完 302 回列表
// （原生表单 + csrf_token 隐藏域），错误经 ?err= 回显。
//
// 页面承担四条验收：
//
//	· 验收 1：可建货源，类型区分内部与外部，可标记关联方；
//	· 验收 2：货源可配置异构的对接扩展信息（config，JSON 对象）；
//	· 验收 3：后台可管理货源（建 / 改 / 停启用 / 删 / 筛选）；
//	· 验收 4：关联方标志可用于报表区分 —— 顶部给出「类型 × 关联方」的交叉统计，
//	  筛选栏的关联方维度直接落到查询上。
//
// 表单提交语义：这一页的编辑表单就是**最终状态**（字段全部回填），
// 因此「结算价留空」= 清空结算价，而不是「本次不改」。
package inventoryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

// sourceStatusFilterAll 状态筛选的「全部（含停用）」取值。
const sourceStatusFilterAll = "all"

// inventorySourcePageSize 货源页一次列出的条数（后台核对用；完整清单走接口分页）。
const inventorySourcePageSize = 200

// inventorySourcePageHandle 货源管理页处理器。
type inventorySourcePageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService
}

// NewInventorySourcePageHandle 构造。
func NewInventorySourcePageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService) *inventorySourcePageHandle {
	return &inventorySourcePageHandle{inventory: inventory, projects: projects}
}

// InventorySourcesPage 货源管理页：工程切换 + 关联方统计 + 筛选 + 新建 + 列表（可编辑）。
func (h *inventorySourcePageHandle) InventorySourcesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	filterType := strings.TrimSpace(c.Query("type"))
	filterRelated := strings.TrimSpace(c.Query("relatedParty"))
	filterStatus := strings.TrimSpace(c.Query("status"))
	filterKeyword := strings.TrimSpace(c.Query("keyword"))

	// 状态筛选三态：""（默认只列启用中）/ active / disabled / all（连停用的一起列）。
	// 用 all 而不是让空值同时表示「全部」，否则「默认视图」与「全量视图」无法区分。
	includeDisabled := filterStatus == sourceStatusFilterAll
	if includeDisabled {
		filterStatus = ""
	}

	sources := []gin.H{}
	pageErr := strings.TrimSpace(c.Query("err"))
	list, lerr := h.inventory.ListSources(ctx, &inventorydto.ListSourceReq{
		ProjectID: selected, Type: filterType, RelatedParty: filterRelated,
		Status: filterStatus, Keyword: filterKeyword,
		IncludeDisabled: includeDisabled, Size: inventorySourcePageSize,
	})
	if lerr != nil {
		// 筛选参数不合法等：把业务错误回显到页面，不把内部细节直出。
		if pageErr == "" {
			pageErr = lerr.Error()
		}
	} else {
		for _, s := range list {
			sources = append(sources, sourceRow(s))
		}
	}

	summary, hasSummary := h.sourceSummary(ctx, selected, &pageErr)

	c.HTML(http.StatusOK, "admin/inventory_sources.html", shell.Prepare(c, gin.H{
		"title":           inventoryenums.MsgInventorySourcesTitle,
		"menu":            "inventory-sources",
		"Projects":        projects,
		"SelectedProject": selected,
		"Sources":         sources,
		"HasSummary":      hasSummary,
		"Summary":         summary,
		"TypeOptions":     sourceTypeOptions(),
		"StatusOptions":   sourceStatusOptions(),
		"RelatedOptions":  sourceRelatedOptions(),
		"FilterType":      filterType,
		"FilterRelated":   filterRelated,
		"FilterStatus":    filterStatus,
		"FilterStatusAll": sourceStatusFilterAll,
		"FilterKeyword":   filterKeyword,
		"Err":             pageErr,
		"Ok":              strings.TrimSpace(c.Query("ok")),
		"Done":            strings.TrimSpace(c.Query("done")),
	}))
}

// InventorySourceCreate 新建货源（内部类型自动成为关联方）。
func (h *inventorySourcePageHandle) InventorySourceCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.CreateSourceReq{
		ProjectID:    projectID,
		Code:         strings.TrimSpace(c.PostForm("code")),
		Name:         strings.TrimSpace(c.PostForm("name")),
		Type:         strings.TrimSpace(c.PostForm("type")),
		Status:       strings.TrimSpace(c.PostForm("status")),
		RelatedParty: sourceRelatedForm(c.PostForm("relatedParty")),
		Config:       sourceConfigForm(c.PostForm("config")),
		Sort:         parseIntOr(c.PostForm("sort"), 0),
	}
	price, perr := sourceSettleForm(c.PostForm("settlePrice"))
	if perr != nil {
		redirectSourceErr(c, projectID, perr)
		return
	}
	req.SettlePrice = price
	if _, err := h.inventory.CreateSource(c.Request.Context(), req); err != nil {
		redirectSourceErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventorySourceUpdate 修改货源（表单即最终状态：结算价留空 = 清空）。
func (h *inventorySourcePageHandle) InventorySourceUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	code := strings.TrimSpace(c.PostForm("code"))
	name := strings.TrimSpace(c.PostForm("name"))
	sourceType := strings.TrimSpace(c.PostForm("type"))
	status := strings.TrimSpace(c.PostForm("status"))
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	req := &inventorydto.UpdateSourceReq{
		ID:           c.PostForm("id"),
		Code:         &code,
		Name:         &name,
		Type:         &sourceType,
		Status:       &status,
		Sort:         &sortValue,
		RelatedParty: sourceRelatedForm(c.PostForm("relatedParty")),
		Config:       sourceConfigForm(c.PostForm("config")),
	}
	// 表单是最终状态：留空即「这条货源没有结算价」，显式交给 service 清空。
	price, perr := sourceSettleForm(c.PostForm("settlePrice"))
	if perr != nil {
		redirectSourceErr(c, projectID, perr)
		return
	}
	if price == nil {
		req.ClearSettlePrice = true
	} else {
		req.SettlePrice = price
	}
	if _, err := h.inventory.UpdateSource(c.Request.Context(), req); err != nil {
		redirectSourceErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventorySourceDelete 删除货源。
func (h *inventorySourcePageHandle) InventorySourceDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.inventory.DeleteSource(c.Request.Context(), &inventorydto.DeleteSourceReq{
		ID: c.PostForm("id"),
	}); err != nil {
		redirectSourceErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventorySourcesBulkDelete 批量删除货源。
//
// 逐条走同一条删除路径：被采购单等历史数据引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带货源管理页，避免静默的部分成功。
func (h *inventorySourcePageHandle) InventorySourcesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&err="+url.QueryEscape(berr.Error()))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.inventory.DeleteSource(c.Request.Context(), &inventorydto.DeleteSourceReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	target := "/admin/inventory/sources?project=" + urlQueryEscape(projectID)
	switch {
	case skipped > 0:
		target += "&err=" + url.QueryEscape(fmt.Sprintf("已删除 %d 个，%d 个未能删除（仍被采购单或历史流水引用）", deleted, skipped))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf("已删除 %d 个货源", deleted))
	}
	c.Redirect(http.StatusFound, target)
}

// sourceSummary 关联方统计（验收 4）。统计失败不阻断页面：置空并附带提示。
func (h *inventorySourcePageHandle) sourceSummary(ctx context.Context, projectID string, pageErr *string) (out gin.H, ok bool) {
	out = gin.H{"Groups": []gin.H{}}
	if projectID == "" {
		return out, false
	}
	res, err := h.inventory.SourceSummary(ctx, &inventorydto.SourceSummaryReq{ProjectID: projectID})
	if err != nil {
		if *pageErr == "" {
			*pageErr = err.Error()
		}
		return out, false
	}
	groups := make([]gin.H, 0, len(res.Groups))
	for _, g := range res.Groups {
		groups = append(groups, gin.H{
			"Type": g.Type, "TypeLabel": sourceTypeLabel(g.Type),
			"RelatedParty": g.RelatedParty, "RelatedPartyLabel": sourceRelatedLabel(g.RelatedParty),
			"Count": g.Count,
		})
	}
	return gin.H{
		"Total": res.Total, "Internal": res.Internal, "External": res.External,
		"RelatedParty": res.RelatedParty, "Unrelated": res.Unrelated,
		"SettlePriced": res.SettlePriced, "Groups": groups,
	}, true
}

// sourceRow 货源行 → 模板视图（金额与配置都先格式化，模板不做逻辑）。
func sourceRow(s *inventorydto.SourceResp) gin.H {
	settlePrice := "—"
	if s.SettlePrice != nil {
		settlePrice = strconv.FormatFloat(*s.SettlePrice, 'f', 2, 64)
	}
	return gin.H{
		"ID": s.ID, "Code": s.Code, "Name": s.Name,
		"Type": s.Type, "TypeLabel": sourceTypeLabel(s.Type),
		"RelatedParty": s.RelatedParty, "RelatedPartyLabel": sourceRelatedLabel(s.RelatedParty),
		"Status": s.Status, "StatusLabel": sourceStatusLabel(s.Status),
		"SettlePrice": settlePrice, "HasSettlePrice": s.SettlePrice != nil,
		"Config": prettyJSON(s.Config), "Sort": s.Sort, "UpdatedAt": s.UpdatedAt,
	}
}

// sourceTypeOptions 类型下拉（外部 / 内部）。
func sourceTypeOptions() []gin.H {
	return []gin.H{
		{"Value": inventoryenums.SourceTypeExternal, "Label": sourceTypeLabel(inventoryenums.SourceTypeExternal)},
		{"Value": inventoryenums.SourceTypeInternal, "Label": sourceTypeLabel(inventoryenums.SourceTypeInternal)},
	}
}

// sourceStatusOptions 状态下拉（启用 / 停用）。
func sourceStatusOptions() []gin.H {
	return []gin.H{
		{"Value": inventoryenums.SourceStatusActive, "Label": sourceStatusLabel(inventoryenums.SourceStatusActive)},
		{"Value": inventoryenums.SourceStatusDisabled, "Label": sourceStatusLabel(inventoryenums.SourceStatusDisabled)},
	}
}

// sourceRelatedOptions 关联方三态下拉：空值 = 按类型默认（编辑时 = 不改）。
func sourceRelatedOptions() []gin.H {
	return []gin.H{
		{"Value": "", "Label": "按类型默认 / 不改"},
		{"Value": "true", "Label": sourceRelatedLabel(true)},
		{"Value": "false", "Label": sourceRelatedLabel(false)},
	}
}

// sourceTypeLabel 货源类型 → 展示文案。
func sourceTypeLabel(sourceType string) string {
	switch sourceType {
	case inventoryenums.SourceTypeInternal:
		return "内部（集团内 / 自家工厂）"
	case inventoryenums.SourceTypeExternal:
		return "外部供应商"
	default:
		return sourceType
	}
}

// sourceRelatedLabel 关联方标志 → 展示文案。
func sourceRelatedLabel(related bool) string {
	if related {
		return "关联方"
	}
	return "非关联方"
}

// sourceStatusLabel 货源状态 → 展示文案。
func sourceStatusLabel(status string) string {
	if status == inventoryenums.SourceStatusDisabled {
		return "已停用"
	}
	return "启用中"
}

// sourceRelatedForm 解析关联方下拉：空串 = 未指定（新建按类型默认 / 编辑不改）。
func sourceRelatedForm(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		yes := true
		return &yes
	case "false", "0":
		no := false
		return &no
	default:
		return nil
	}
}

// sourceSettleForm 解析结算价输入：空串 = 无值（更新路径据此清空）。
func sourceSettleForm(value string) (out *float64, err error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, nil
	}
	parsed, perr := strconv.ParseFloat(raw, 64)
	if perr != nil {
		return nil, errors.New(inventoryenums.ErrSourceSettleInvalid)
	}
	return &parsed, nil
}

// sourceConfigForm 解析对接配置输入：空串表示未填（service 落成 {}）。
func sourceConfigForm(value string) json.RawMessage {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}

// prettyJSON 把 jsonb 缩进成可读文本（解析失败时原样回填，不吞掉用户数据）。
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// redirectSourceErr 回列表并把业务错误经 ?err= 回显。
func redirectSourceErr(c *gin.Context, projectID string, err error) {
	c.Redirect(http.StatusFound, "/admin/inventory/sources?project="+urlQueryEscape(projectID)+"&err="+url.QueryEscape(err.Error()))
}
