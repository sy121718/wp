package membershiphttp

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

func formInt(raw string, fallback int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func formInt64(raw string, fallback int64) int64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

const (
	membershipTiersPath       = "/admin/membership"
	membershipAssignmentsPath = "/admin/membership/assignments"
)

type membershipPageHandle struct {
	svc      membershipcontract.MembershipService
	projects projectcontract.ProjectService
}

func NewMembershipPageHandle(svc membershipcontract.MembershipService,
	projects projectcontract.ProjectService) *membershipPageHandle {
	return &membershipPageHandle{svc: svc, projects: projects}
}

type membershipTierRow struct {
	ID        int64
	Name      string
	SortOrder int

	ThresholdAmount int64
	IsDefault       bool
	Remark          string

	FreeShipping bool

	HasDiscount     bool
	DiscountPercent int64

	HasEntitlement bool
}

type membershipTierEdit struct {
	ID              int64
	Name            string
	SortOrder       int
	ThresholdAmount int64
	Remark          string
	FreeShipping    bool
	HasDiscount     bool
	DiscountPercent int64
	IsDefault       bool
}

type membershipAssignRow struct {
	ID         int64
	UserID     uint64
	TierID     int64
	TierName   string
	Source     string
	AssignedAt utils.JSONTime
}

type membershipTiersPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Tiers           []membershipTierRow

	HasTiers bool

	Edit *membershipTierEdit

	KindFreeShippingValue string
	KindDiscountValue     string

	ListQuery string

	LoadErr string

	LoadFailed bool
}

func (h *membershipPageHandle) renderTiersPage(c *gin.Context, d *membershipTiersPageData) {
	pageview.Render(c, d)
}

func (h *membershipPageHandle) MembershipPage(c *gin.Context) {
	ctx := c.Request.Context()

	loadErrText := ""

	projects, loadErr := h.listProjects(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		loadErrText = membershipText(c, loadErr)
	}

	selected := ""
	if !loadFailed {
		selected = h.pickProject(c.Query("project"), projects)
	}

	rows := []membershipTierRow{}
	var edit *membershipTierEdit
	if !loadFailed && selected != "" {
		list, lerr := h.svc.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: selected})
		if lerr != nil {
			if loadErrText == "" {
				loadErrText = membershipText(c, lerr)
			}
		} else {
			for _, tier := range list {
				rows = append(rows, membershipTierRowOf(tier))
			}

			edit = membershipTierEditOf(list, formInt64(c.Query("edit"), 0))
		}
	}

	h.renderTiersPage(c, &membershipTiersPageData{
		Title:                 membershipenums.PageTiersTitle,
		Menu:                  "membership",
		Projects:              projects,
		SelectedProject:       selected,
		Tiers:                 rows,
		HasTiers:              len(rows) > 0,
		Edit:                  edit,
		KindFreeShippingValue: membershipenums.KindFreeShipping,
		KindDiscountValue:     membershipenums.KindDiscount,
		ListQuery:             membershipListQuery(map[string]string{"project": selected}),
		LoadErr:               loadErrText,
		LoadFailed:            loadFailed,
	})
}

type membershipAssignmentsPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	TierOptions     []membershipdto.TierOption
	SourceOptions   []membershipdto.ValueOption
	FilterTier      int64
	FilterSource    string
	FilterUser      uint64
	Rows            []membershipAssignRow

	Page  int
	Total int64

	SourceManual string

	ListQuery string

	LoadErr    string
	LoadFailed bool

	RecalcAvailable bool
	Pagination      pagination.Page
}

func (h *membershipPageHandle) renderAssignmentsPage(c *gin.Context, d *membershipAssignmentsPageData) {
	pageview.Render(c, d)
}

func (h *membershipPageHandle) MembershipAssignmentsPage(c *gin.Context) {
	ctx := c.Request.Context()

	loadErrText := ""

	projects, loadErr := h.listProjects(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		loadErrText = membershipText(c, loadErr)
	}

	selected := ""
	if !loadFailed {
		selected = h.pickProject(c.Query("project"), projects)
	}

	filterTier := formInt64(c.Query("tierId"), 0)
	filterSource := c.Query("source")
	filterUser := uint64(formInt64(c.Query("userId"), 0))
	page, limit := normalizePageParams(c)

	rows := []membershipAssignRow{}
	tierOptions := []membershipdto.TierOption{}
	var total int64
	var pg pagination.Page

	if !loadFailed && selected != "" {

		tiers, terr := h.svc.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: selected})
		if terr != nil {
			if loadErrText == "" {
				loadErrText = membershipText(c, terr)
			}
		} else {
			tierOptions = membershipTierOptions(tiers, filterTier)
		}

		n, cerr := h.svc.CountAssignments(ctx, &membershipdto.CountAssignmentsReq{
			ProjectID: selected, TierID: filterTier, Source: filterSource, UserID: filterUser,
		})
		if cerr != nil {
			if loadErrText == "" {
				loadErrText = membershipText(c, cerr)
			}
		} else {
			total = n
			page = clampPage(page, limit, total)
			list, lerr := h.svc.ListAssignments(ctx, &membershipdto.ListAssignmentsReq{
				ProjectID: selected, TierID: filterTier, Source: filterSource, UserID: filterUser,
				Page: page, Size: limit,
			})
			if lerr != nil {
				if loadErrText == "" {
					loadErrText = membershipText(c, lerr)
				}
			} else {
				for _, item := range list {
					rows = append(rows, membershipAssignRowOf(item))
				}
			}
			p := pagination.Page{Total: total, Page: page, Limit: limit}
			p.Base = reqparams.FilterBaseURL(membershipAssignmentsPath, map[string]string{
				"project": selected,
				"tierId":  itoa64(filterTier),
				"source":  filterSource,
				"userId":  itoaU64(filterUser),
			})
			pg = p
		}
	}

	h.renderAssignmentsPage(c, &membershipAssignmentsPageData{
		Title:           membershipenums.PageAssignmentsTitle,
		Menu:            "membership-assignments",
		Projects:        projects,
		SelectedProject: selected,
		TierOptions:     tierOptions,
		SourceOptions:   membershipSourceOptions(filterSource),
		FilterTier:      filterTier,
		FilterSource:    filterSource,
		FilterUser:      filterUser,
		Rows:            rows,
		Total:           total,
		ListQuery: membershipListQuery(map[string]string{
			"project": selected,
			"tierId":  itoa64(filterTier),
			"source":  filterSource,
			"userId":  itoaU64(filterUser),
			"page":    strconv.Itoa(page),
		}),
		LoadErr:         loadErrText,
		LoadFailed:      loadFailed,
		RecalcAvailable: h.recalcAvailable(),
		Pagination:      pg,
	})
}

func (h *membershipPageHandle) listProjects(ctx context.Context) ([]projectcontract.ProjectResp, error) {
	if h.projects == nil {
		return nil, nil
	}
	return h.projects.List(ctx)
}

func (h *membershipPageHandle) pickProject(raw string, projects []projectcontract.ProjectResp) string {
	if v := trimSpace(raw); v != "" {
		return v
	}
	if len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

func (h *membershipPageHandle) recalcAvailable() bool {
	if h.svc == nil {
		return false
	}

	if probe, ok := h.svc.(recalcProbe); ok {
		return probe.RecalcReady()
	}
	return false
}

type recalcProbe interface {
	RecalcReady() bool
}

const (
	assignmentsPageSize    = 20
	assignmentsMaxPageSize = 200
)

func membershipListQuery(params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	return q.Encode()
}

func membershipTierRowOf(tier *membershipdto.TierResp) membershipTierRow {
	row := membershipTierRow{
		ID:              tier.ID,
		Name:            tier.Name,
		SortOrder:       tier.SortOrder,
		ThresholdAmount: tier.ThresholdAmount,
		IsDefault:       tier.IsDefault,
		Remark:          tier.Remark,
	}
	for _, ent := range tier.Entitlements {
		switch ent.Kind {
		case membershipenums.KindFreeShipping:
			row.FreeShipping = ent.ValueInt == 1
			row.HasEntitlement = true
		case membershipenums.KindDiscount:
			row.HasDiscount = true
			row.DiscountPercent = ent.ValueInt
			row.HasEntitlement = true
		}
	}
	return row
}

func membershipAssignRowOf(item *membershipdto.AssignmentResp) membershipAssignRow {
	return membershipAssignRow{
		ID:         item.ID,
		UserID:     item.UserID,
		TierID:     item.TierID,
		TierName:   item.TierName,
		Source:     item.Source,
		AssignedAt: item.AssignedAt,
	}
}

func membershipSourceOptions(selected string) []membershipdto.ValueOption {
	values := []string{"", membershipmodel.SourceAuto, membershipmodel.SourceManual}
	out := make([]membershipdto.ValueOption, 0, len(values))
	for _, v := range values {
		out = append(out, membershipdto.ValueOption{Value: v, Selected: v == selected})
	}
	return out
}

func membershipTierOptions(tiers []*membershipdto.TierResp, selected int64) []membershipdto.TierOption {
	out := make([]membershipdto.TierOption, 0, len(tiers))
	for _, tier := range tiers {
		out = append(out, membershipdto.TierOption{Value: tier.ID, Label: tier.Name, Selected: tier.ID == selected})
	}
	return out
}

func normalizePageParams(c *gin.Context) (page, limit int) {
	page = formInt(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	limit = formInt(c.Query("limit"), assignmentsPageSize)
	if limit < 1 {
		limit = assignmentsPageSize
	}
	if limit > assignmentsMaxPageSize {
		limit = assignmentsMaxPageSize
	}
	return page, limit
}

func clampPage(page, limit int, total int64) int {
	if limit <= 0 {
		return 1
	}
	maxPage := int((total + int64(limit) - 1) / int64(limit))
	if maxPage < 1 {
		return 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

func itoa64(v int64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}

func itoaU64(v uint64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(v, 10)
}

func trimSpace(raw string) string { return strings.TrimSpace(raw) }

func membershipTierEditOf(list []*membershipdto.TierResp, tierID int64) *membershipTierEdit {
	if tierID <= 0 {
		return nil
	}
	for _, tier := range list {
		if tier.ID != tierID {
			continue
		}
		edit := &membershipTierEdit{
			ID:              tier.ID,
			Name:            tier.Name,
			SortOrder:       tier.SortOrder,
			ThresholdAmount: tier.ThresholdAmount,
			Remark:          tier.Remark,
			IsDefault:       tier.IsDefault,
		}
		for _, ent := range tier.Entitlements {
			switch ent.Kind {
			case membershipenums.KindFreeShipping:
				edit.FreeShipping = ent.ValueInt == 1
			case membershipenums.KindDiscount:
				edit.HasDiscount = true
				edit.DiscountPercent = ent.ValueInt
			}
		}
		return edit
	}
	return nil
}
