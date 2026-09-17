// masterdata_change_page_handle.go — 后台「变更记录」页（issue #19 验收 3 / 4）。
//
// 只读页面：GET 渲染完整页，没有写表单 —— 变更记录由业务模块在写操作里追加，
// 后台不提供「手工补一条记录」的口子（审计的入口越少越可信）。
//
// 页面承担两条验收：
//
//	· 验收 3：记录含操作人与时间，append-only 不可改写（页面按时间倒序展示，
//	  没有任何编辑 / 删除入口）；
//	· 验收 4：可按实体查询变更历史 —— 顶部是「哪些实体被改过」的聚合清单
//	  （点一行即筛出该实体的完整时间线），下面是字段级变更列表（可按实体类型 / 实体 id /
//	  字段 / 动作 / 操作人 / 时间区间组合筛选）。
//
// 页面同时把「与库存流水的分工」写在正文里：这张表只记配置变更，
// 数量增减在库存流水页 —— 两张表回答的是不同的问题。
package masterdatahttp

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

// masterDataEntityPageSize 实体清单一次展示的条数（后台核对用；完整清单走接口分页）。
const masterDataEntityPageSize = 50

// masterDataChangePageHandle 变更记录页处理器。
type masterDataChangePageHandle struct {
	changes  masterdatacontract.MasterDataService
	projects projectcontract.ProjectService
}

// NewMasterDataChangePageHandle 构造。
func NewMasterDataChangePageHandle(changes masterdatacontract.MasterDataService,
	projects projectcontract.ProjectService) *masterDataChangePageHandle {
	return &masterDataChangePageHandle{changes: changes, projects: projects}
}

// masterDataFilter 页面筛选条件（GET 参数，全部可选）。
type masterDataFilter struct {
	EntityType string
	EntityID   string
	Field      string
	Action     string
	Keyword    string
	OperatorID string
	Since      string
	Until      string
}

// masterDataFilterOf 读取筛选参数（空串 = 该维度不过滤）。
func masterDataFilterOf(c *gin.Context) masterDataFilter {
	return masterDataFilter{
		EntityType: strings.TrimSpace(c.Query("entityType")),
		EntityID:   strings.TrimSpace(c.Query("entityId")),
		Field:      strings.TrimSpace(c.Query("field")),
		Action:     strings.TrimSpace(c.Query("action")),
		Keyword:    strings.TrimSpace(c.Query("keyword")),
		OperatorID: strings.TrimSpace(c.Query("operatorId")),
		Since:      strings.TrimSpace(c.Query("since")),
		Until:      strings.TrimSpace(c.Query("until")),
	}
}

// specific 是否已锁定单个实体（锁定后走「单实体完整时间线」）。
func (f masterDataFilter) specific() bool {
	return f.EntityType != "" && f.EntityID != ""
}

// values 非空筛选条件（分页链接与实体链接共用）。
func (f masterDataFilter) values(projectID string) map[string]string {
	return map[string]string{
		"project": projectID, "entityType": f.EntityType, "entityId": f.EntityID,
		"field": f.Field, "action": f.Action, "keyword": f.Keyword,
		"operatorId": f.OperatorID, "since": f.Since, "until": f.Until,
	}
}

// entityValues 在筛选条件上锁定一个实体（实体清单里点一行）。
func (f masterDataFilter) entityValues(projectID, entityType, entityID string) map[string]string {
	vals := f.values(projectID)
	vals["entityType"] = entityType
	vals["entityId"] = entityID
	return vals
}

// MasterDataChangesPage 变更记录页（GET /admin/masterdata/changes）。
func (h *masterDataChangePageHandle) MasterDataChangesPage(c *gin.Context) {
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
	filter := masterDataFilterOf(c)
	page, limit := shell.PageParams(c)

	pageErr := strings.TrimSpace(c.Query("err"))
	rows := []gin.H{}
	var total int64
	var current *gin.H

	listReq := &masterdatadto.ListChangeReq{
		ProjectID: selected, EntityType: filter.EntityType, EntityID: filter.EntityID,
		Field: filter.Field, Action: filter.Action, Keyword: filter.Keyword,
		OperatorID: filter.OperatorID, Since: filter.Since, Until: filter.Until,
		Page: page, Size: limit,
	}
	if filter.specific() {
		// 锁定单个实体：一次拿到「是谁 + 共几条 + 这一页时间线」。
		timeline, terr := h.changes.EntityTimeline(ctx, &masterdatadto.EntityTimelineReq{
			ProjectID: selected, EntityType: filter.EntityType, EntityID: filter.EntityID,
			Page: page, Size: limit,
		})
		if terr != nil {
			pageErr = firstNonEmpty(pageErr, terr.Error())
		} else {
			total = timeline.Total
			current = &gin.H{
				"EntityType": timeline.EntityType, "EntityTypeLabel": timeline.EntityTypeLabel,
				"EntityID": timeline.EntityID, "EntityLabel": timeline.EntityLabel, "Total": timeline.Total,
			}
			for _, row := range timeline.Changes {
				rows = append(rows, changeRow(row))
			}
		}
	} else {
		list, lerr := h.changes.ListChanges(ctx, listReq)
		if lerr != nil {
			pageErr = firstNonEmpty(pageErr, lerr.Error())
		} else {
			for _, row := range list {
				rows = append(rows, changeRow(row))
			}
		}
		if count, cerr := h.changes.CountChanges(ctx, listReq); cerr == nil {
			total = count
		} else {
			pageErr = firstNonEmpty(pageErr, cerr.Error())
		}
	}

	// 实体清单（验收 4 的入口）：同一组筛选条件下被改过的实体与各自次数。
	entityRows := []gin.H{}
	var entityTotal int64
	entityReq := &masterdatadto.ListEntityReq{
		ProjectID: selected, EntityType: filter.EntityType, EntityID: filter.EntityID,
		Field: filter.Field, Action: filter.Action, Keyword: filter.Keyword,
		OperatorID: filter.OperatorID, Since: filter.Since, Until: filter.Until,
		Page: 1, Size: masterDataEntityPageSize,
	}
	entities, eerr := h.changes.ListEntities(ctx, entityReq)
	if eerr != nil {
		pageErr = firstNonEmpty(pageErr, eerr.Error())
	} else {
		for _, item := range entities {
			entityRows = append(entityRows, entityRow(item, filter, selected))
		}
	}
	if count, cerr := h.changes.CountEntities(ctx, entityReq); cerr == nil {
		entityTotal = count
	}

	data := shell.Prepare(c, gin.H{
		"title":           masterdataenums.MsgMasterDataChangesTitle,
		"menu":            "masterdata-changes",
		"Projects":        projects,
		"SelectedProject": selected,
		"EntityTypes":     masterDataEntityTypeOptions(),
		"Actions":         masterDataActionOptions(),
		// 筛选条件逐项传给模板（Jet 对「map 再索引」的链式访问不确定，与货源页同一手法）。
		"FilterEntityType": filter.EntityType,
		"FilterEntityID":   filter.EntityID,
		"FilterField":      filter.Field,
		"FilterAction":     filter.Action,
		"FilterKeyword":    filter.Keyword,
		"FilterOperatorID": filter.OperatorID,
		"FilterSince":      filter.Since,
		"FilterUntil":      filter.Until,
		"FilterSpecific":   filter.specific(),
		"Current":          current,
		"Rows":             rows,
		"Total":            total,
		"Entities":         entityRows,
		"EntityTotal":      entityTotal,
		"EntityShown":      len(entityRows),
		"EntityPageSize":   masterDataEntityPageSize,
		// 实体清单被截断时给一句提示（比较运算留在 handler，模板只做判断）。
		"EntityTruncated": entityTotal > int64(len(entityRows)),
		"Err":             pageErr,
	})
	base := shell.FilterBaseURL("/admin/masterdata/changes", filter.values(selected))
	for k, v := range shell.BuildPagination(total, page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/masterdata_changes.html", data)
}

// changeRow 变更记录行 → 模板视图（时间与取值都先格式化，模板不做逻辑）。
func changeRow(row *masterdatadto.ChangeResp) gin.H {
	if row == nil {
		return gin.H{}
	}
	return gin.H{
		"Time":       masterDataTimeLabel(row.CreatedAt),
		"EntityType": row.EntityType, "EntityTypeLabel": row.EntityTypeLabel,
		"EntityID": row.EntityID, "EntityLabel": row.EntityLabel,
		"Action": row.Action, "ActionLabel": row.ActionLabel,
		"Field": row.Field, "FieldLabel": row.FieldLabel,
		"OldValue": masterDataValueLabel(row.OldValue),
		"NewValue": masterDataValueLabel(row.NewValue),
		"Origin":   row.Origin, "OperatorID": masterDataValueLabel(row.OperatorID),
	}
}

// entityRow 实体清单行 → 模板视图（带一条锁定该实体的筛选链接）。
func entityRow(item *masterdatadto.EntityHistoryResp, filter masterDataFilter, projectID string) gin.H {
	if item == nil {
		return gin.H{}
	}
	return gin.H{
		"EntityType": item.EntityType, "EntityTypeLabel": item.EntityTypeLabel,
		"EntityID": item.EntityID, "EntityLabel": masterDataValueLabel(item.EntityLabel),
		"ChangeCount": item.ChangeCount,
		"LastAction":  item.LastAction, "LastActionLabel": item.LastActionLabel,
		"LastField": item.LastField, "LastFieldLabel": item.LastFieldLabel,
		"LastOperatorID": masterDataValueLabel(item.LastOperatorID),
		"LastAt":         masterDataTimeLabel(item.LastAt),
		"URL": shell.FilterBaseURL("/admin/masterdata/changes",
			filter.entityValues(projectID, item.EntityType, item.EntityID)),
	}
}

// masterDataValueLabel 空值显示成「—」：审计里「空」与「没有这个字段」靠列名区分，
// 空白单元格没法读。
func masterDataValueLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// masterDataTimeLabel RFC3339 → 后台展示文本（本地时区，分钟精度）。
func masterDataTimeLabel(at string) string {
	if strings.TrimSpace(at) == "" {
		return "—"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// masterDataEntityTypeOptions 实体类型下拉（白名单来自模块 enums，单一来源）。
func masterDataEntityTypeOptions() []gin.H {
	options := make([]gin.H, 0, len(masterdataenums.EntityTypes()))
	for _, entityType := range masterdataenums.EntityTypes() {
		options = append(options, gin.H{"Value": entityType, "Label": masterdataenums.EntityTypeLabel(entityType)})
	}
	return options
}

// masterDataActionOptions 动作下拉（新增 / 修改 / 删除）。
func masterDataActionOptions() []gin.H {
	options := make([]gin.H, 0, len(masterdataenums.Actions()))
	for _, action := range masterdataenums.Actions() {
		options = append(options, gin.H{"Value": action, "Label": masterdataenums.ActionLabel(action)})
	}
	return options
}
