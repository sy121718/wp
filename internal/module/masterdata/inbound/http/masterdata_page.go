package masterdatahttp

import (
	"errors"
	"go_wp/internal/middleware/builtin"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const masterDataEntityPageSize = 50

const (
	masterDataViewRecords  = "records"
	masterDataViewEntities = "entities"
)

type masterDataChangePageHandle struct {
	changes  masterdatacontract.MasterDataService
	projects projectcontract.ProjectService
}

func NewMasterDataChangePageHandle(changes masterdatacontract.MasterDataService,
	projects projectcontract.ProjectService) *masterDataChangePageHandle {
	return &masterDataChangePageHandle{changes: changes, projects: projects}
}

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

func (f masterDataFilter) specific() bool {
	return f.EntityType != "" && f.EntityID != ""
}

func (f masterDataFilter) values(projectID string) map[string]string {
	return map[string]string{
		"project": projectID, "entityType": f.EntityType, "entityId": f.EntityID,
		"field": f.Field, "action": f.Action, "keyword": f.Keyword,
		"operatorId": f.OperatorID, "since": f.Since, "until": f.Until,
	}
}

func (f masterDataFilter) entityValues(projectID, entityType, entityID string) map[string]string {
	vals := f.values(projectID)
	vals["entityType"] = entityType
	vals["entityId"] = entityID
	return vals
}

func (h *masterDataChangePageHandle) MasterDataChangesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)

	loadFailed := err != nil
	pageErr := ""
	if loadFailed {
		pageErr = masterDataErrText(c, err)
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	filter := masterDataFilterOf(c)
	page, limit := reqparams.PageParams(c)

	rows := []masterdatadto.ChangeRowResp{}
	var total int64
	var current *masterdatadto.EntityTimelineHeadResp
	entityRows := []masterdatadto.EntityRowResp{}
	var entityTotal int64

	if !loadFailed {
		listReq := &masterdatadto.ListChangeReq{
			ProjectID: selected, EntityType: filter.EntityType, EntityID: filter.EntityID,
			Field: filter.Field, Action: filter.Action, Keyword: filter.Keyword,
			OperatorID: filter.OperatorID, Since: filter.Since, Until: filter.Until,
			Page: page, Size: limit,
		}
		if filter.specific() {

			timeline, terr := h.changes.EntityTimeline(ctx, &masterdatadto.EntityTimelineReq{
				ProjectID: selected, EntityType: filter.EntityType, EntityID: filter.EntityID,
				Page: page, Size: limit,
			})
			if terr != nil {
				pageErr = firstNonEmpty(pageErr, masterDataErrText(c, terr))
			} else {
				total = timeline.Total
				current = &masterdatadto.EntityTimelineHeadResp{
					EntityType: timeline.EntityType,
					EntityTypeLabel: masterDataLabel(i18n.TranslateFunc(response.RequestLanguage(c)),
						masterdataenums.EntityTypeLabel(timeline.EntityType)),
					EntityID:    timeline.EntityID,
					EntityLabel: timeline.EntityLabel,
					Total:       timeline.Total,
				}
				for _, row := range timeline.Changes {
					rows = append(rows, changeRow(i18n.TranslateFunc(response.RequestLanguage(c)), row))
				}
			}
		} else {
			list, lerr := h.changes.ListChanges(ctx, listReq)
			if lerr != nil {
				pageErr = firstNonEmpty(pageErr, masterDataErrText(c, lerr))
			} else {
				for _, row := range list {
					rows = append(rows, changeRow(i18n.TranslateFunc(response.RequestLanguage(c)), row))
				}
			}
			if count, cerr := h.changes.CountChanges(ctx, listReq); cerr == nil {
				total = count
			} else {
				pageErr = firstNonEmpty(pageErr, masterDataErrText(c, cerr))
			}
		}

		entityReq := &masterdatadto.ListEntityReq{
			ProjectID: selected, EntityType: filter.EntityType, EntityID: filter.EntityID,
			Field: filter.Field, Action: filter.Action, Keyword: filter.Keyword,
			OperatorID: filter.OperatorID, Since: filter.Since, Until: filter.Until,
			Page: 1, Size: masterDataEntityPageSize,
		}
		entities, eerr := h.changes.ListEntities(ctx, entityReq)
		if eerr != nil {
			pageErr = firstNonEmpty(pageErr, masterDataErrText(c, eerr))
		} else {
			for _, item := range entities {
				entityRows = append(entityRows, entityRow(i18n.TranslateFunc(response.RequestLanguage(c)), item, filter, selected))
			}
		}
		if count, cerr := h.changes.CountEntities(ctx, entityReq); cerr == nil {
			entityTotal = count
		}
	}

	viewMode := strings.TrimSpace(c.Query("view"))
	if viewMode != masterDataViewEntities || filter.specific() {
		viewMode = masterDataViewRecords
	}

	base := reqparams.FilterBaseURL("/admin/masterdata/changes", filter.values(selected))

	tr := i18n.TranslateFunc(response.RequestLanguage(c))
	view := &masterdatadto.MasterDataChangesPageResp{
		Title:           masterDataLabel(tr, masterdataenums.PageTitleChanges),
		Menu:            "masterdata-changes",
		Projects:        projects,
		SelectedProject: selected,
		EntityTypes:     masterDataEntityTypeOptions(tr),
		Actions:         masterDataActionOptions(tr),

		FilterEntityType: filter.EntityType,
		FilterEntityID:   filter.EntityID,
		FilterField:      filter.Field,
		FilterAction:     filter.Action,
		FilterKeyword:    filter.Keyword,
		FilterOperatorID: filter.OperatorID,
		FilterSince:      filter.Since,
		FilterUntil:      filter.Until,
		FilterSpecific:   filter.specific(),
		ViewMode:         viewMode,
		RecordsViewURL:   base,
		EntitiesViewURL:  base + "&view=" + masterDataViewEntities,
		Current:          current,
		Rows:             rows,
		Total:            total,
		Entities:         entityRows,
		EntityTotal:      entityTotal,
		EntityShown:      len(entityRows),
		EntityPageSize:   masterDataEntityPageSize,

		EntityTruncated: entityTotal > int64(len(entityRows)),
		Err:             pageErr,

		LoadFailed: loadFailed,
	}

	if !adminui.AttachPaginationView(view, "", nil,
		pagination.Page{Total: total, Page: page, Limit: limit, Base: base}) {

		logger.Scene("masterdata").Error(errors.New("变更记录视图缺少 Pagination 字段"), "分页事实挂载失败")
	}

	pageview.Render(c, view)
	return
}

const masterDataErrScene = "masterdata"

var masterDataFacingTexts = map[string]string{
	masterdataenums.ErrInvalidParam:      "参数不合法，请检查筛选条件。",
	masterdataenums.ErrEntityTypeInvalid: "实体类型不在变更记录的白名单内。",
	masterdataenums.ErrActionInvalid:     "变更动作不合法。",
	masterdataenums.ErrEntityIDRequired:  "实体 id 必填。",
	masterdataenums.ErrProjectRequired:   "未指定工程，也无法从数据里解析出唯一工程。",
	masterdataenums.ErrTimeRangeInvalid:  "时间范围不合法：起始时间不能晚于结束时间。",
}

func masterDataErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	key := strings.TrimSpace(err.Error())
	if fallback, ok := masterDataFacingTexts[key]; ok {
		return i18n.TranslateFunc(response.RequestLanguage(c))(key, fallback)
	}
	logger.Scene(masterDataErrScene).
		With("user_id", builtin.CurrentUserID(c)).
		Error(err, "主数据变更记录页取数失败（非业务错误，只对外给归口文案）")
	return adminui.PageInternalText(c)
}

type masterDataTr func(key, fallback string) string

func masterDataLabel(tr masterDataTr, pair masterdataenums.LabelPair) string {
	if strings.TrimSpace(pair.Key) == "" {
		return pair.Fallback
	}
	return tr(pair.Key, pair.Fallback)
}

func changeRow(tr masterDataTr, row *masterdatadto.ChangeResp) masterdatadto.ChangeRowResp {
	if row == nil {
		return masterdatadto.ChangeRowResp{}
	}
	return masterdatadto.ChangeRowResp{
		Time:            masterDataTimeLabel(row.CreatedAt),
		EntityType:      row.EntityType,
		EntityTypeLabel: masterDataLabel(tr, masterdataenums.EntityTypeLabel(row.EntityType)),
		EntityID:        row.EntityID,
		EntityLabel:     row.EntityLabel,
		Action:          row.Action,
		ActionLabel:     masterDataLabel(tr, masterdataenums.ActionLabel(row.Action)),
		Field:           row.Field,
		FieldLabel:      masterDataLabel(tr, masterdataenums.FieldLabel(row.EntityType, row.Field)),
		OldValue:        masterDataValueLabel(row.OldValue),
		NewValue:        masterDataValueLabel(row.NewValue),
		Origin:          row.Origin,
		OperatorID:      masterDataValueLabel(row.OperatorID),
	}
}

func entityRow(tr masterDataTr, item *masterdatadto.EntityHistoryResp, filter masterDataFilter, projectID string) masterdatadto.EntityRowResp {
	if item == nil {
		return masterdatadto.EntityRowResp{}
	}
	return masterdatadto.EntityRowResp{
		EntityType: item.EntityType,
		EntityTypeLabel: masterDataLabel(tr,
			masterdataenums.EntityTypeLabel(item.EntityType)),
		EntityID:    item.EntityID,
		EntityLabel: masterDataValueLabel(item.EntityLabel),
		ChangeCount: item.ChangeCount,
		LastAction:  item.LastAction,
		LastActionLabel: masterDataLabel(tr,
			masterdataenums.ActionLabel(item.LastAction)),
		LastField: item.LastField,
		LastFieldLabel: masterDataLabel(tr,
			masterdataenums.FieldLabel(item.EntityType, item.LastField)),
		LastOperatorID: masterDataValueLabel(item.LastOperatorID),
		LastAt:         masterDataTimeLabel(item.LastAt),
		URL: reqparams.FilterBaseURL("/admin/masterdata/changes",
			filter.entityValues(projectID, item.EntityType, item.EntityID)),
	}
}

func masterDataValueLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func masterDataTimeLabel(at string) string {
	if strings.TrimSpace(at) == "" {
		return "—"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

func masterDataEntityTypeOptions(tr masterDataTr) []masterdatadto.LabelOptionResp {
	options := make([]masterdatadto.LabelOptionResp, 0, len(masterdataenums.EntityTypes()))
	for _, entityType := range masterdataenums.EntityTypes() {
		options = append(options, masterdatadto.LabelOptionResp{
			Value: entityType,
			Label: masterDataLabel(tr, masterdataenums.EntityTypeLabel(entityType)),
		})
	}
	return options
}

func masterDataActionOptions(tr masterDataTr) []masterdatadto.LabelOptionResp {
	options := make([]masterdatadto.LabelOptionResp, 0, len(masterdataenums.Actions()))
	for _, action := range masterdataenums.Actions() {
		options = append(options, masterdatadto.LabelOptionResp{
			Value: action,
			Label: masterDataLabel(tr, masterdataenums.ActionLabel(action)),
		})
	}
	return options
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
