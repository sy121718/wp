// membership_page_handle.go — membership 模块的后台页面（BIZ-3）。
//
// 两个页面，各管一件事：
//
//	/admin/membership              —— 等级与权益：门槛、排序、默认等级、两种权益的取值
//	/admin/membership/assignments  —— 会员归属：谁是什么等级、手工指定、解除手工锁定
//
// 为什么不合成一页：等级是**配置**（改一次全站生效，改的人少、次数少），
// 归属是**日常操作**（按客户查、按等级查、手工调整，天天用）。两者挤在一页的形态
// 会让「改门槛」与「查某个人」互相干扰 —— 与库存域把「变动原因字典」从库存页拆出去
// 是同一轮设计评审的结论。
//
// 页面形态照本仓既有后台页：GET 渲染整页、POST 走表单 302 回列表（?err= / ?done= 回带），
// 数据由 templateMap 组装（模板只渲染、不查询）。错误一律经 membershipErr.go 的三件套。
package membershiphttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

// 页面路径常量（回跳与分页基址都用同一份字面量，避免两处各写一遍而慢慢漂移）。
const (
	membershipTiersPath       = "/admin/membership"
	membershipAssignmentsPath = "/admin/membership/assignments"
)

// membershipPageHandle 后台页面处理器。
type membershipPageHandle struct {
	svc      membershipcontract.MembershipService
	projects projectcontract.ProjectService
}

// NewMembershipPageHandle 构造。
func NewMembershipPageHandle(svc membershipcontract.MembershipService,
	projects projectcontract.ProjectService) *membershipPageHandle {
	return &membershipPageHandle{svc: svc, projects: projects}
}

// membershipTierRow 等级列表的一行（模板直接渲染，不在模板里做换算或判断）。
type membershipTierRow struct {
	ID            int64
	Name          string
	SortOrder     int
	ThresholdYuan string
	IsDefault     bool
	Remark        string
	FreeShipping  bool
	DiscountText  string
	// DiscountRaw 折扣的原始数字（编辑表单回填 input 用；展示用 DiscountText）。
	DiscountRaw string
	// HasEntitlement 该等级是否配了至少一种权益（空态文案与「两种都是关」要能分辨）。
	HasEntitlement bool
}

// membershipTierEdit 等级编辑区的回填数据（?edit=<id> 时渲染）。
//
// 用「服务端按 query 参数渲染编辑区」而不是抽屉 + JS 回填：本仓不新增散落业务 JS
// （AGENTS.md §交互方式），而编辑区要回填的值（门槛的元表示、折扣的原始数字）
// 从 HTML 属性里读出来再塞进 input 是一段没人维护的胶水代码。
type membershipTierEdit struct {
	ID              int64
	Name            string
	SortOrder       int
	ThresholdYuan   string
	Remark          string
	FreeShipping    bool
	DiscountPercent string
	IsDefault       bool
}

// membershipAssignRow 归属列表的一行。
type membershipAssignRow struct {
	ID         int64
	UserID     uint64
	TierID     int64
	TierName   string
	Source     string
	SourceText string
	IsManual   bool
	AssignedAt string
}

// —— 等级与权益页 ——

// membershipTiersPageData 等级页的模板数据（正常渲染与降级渲染共用一份拼装）。
//
// 单独一个结构体的理由：降级分支若另抄一份 gin.H，两处的键集必然分叉，
// 而模板缺 key 的后果是 renderError → HTTP 500（buffer 里的半截内容被丢弃）。
type membershipTiersPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Tiers           []membershipTierRow
	// Edit 当前正在编辑的等级（nil = 不显示编辑区）。
	Edit *membershipTierEdit
	// KindFreeShippingValue / KindDiscountValue 权益类型的表单取值（模板不用硬编码字符串）。
	KindFreeShippingValue string
	KindDiscountValue     string
	Err                   string
	Done                  string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 与货源页同名的判据：模板里没有可靠办法分辨「Tiers 为空」是「这个工程真的没有等级」
	// 还是「这一次没读出来」—— 前者要引导去建一个，后者只能说「稍后重试」。
	LoadFailed bool
}

// templateMap 转 Jet 模板键（页面框架字段以小写 title / menu 取值）。
func (d *membershipTiersPageData) templateMap(c *gin.Context) gin.H {
	tr := shell.TranslateFor(c)
	return gin.H{
		"title":                 d.Title,
		"menu":                  d.Menu,
		"Projects":              d.Projects,
		"SelectedProject":       d.SelectedProject,
		"Tiers":                 d.Tiers,
		"Edit":                  d.Edit,
		"HasTiers":              len(d.Tiers) > 0,
		"KindFreeShippingValue": d.KindFreeShippingValue,
		"KindDiscountValue":     d.KindDiscountValue,
		"KindFreeShippingLabel": tr(membershipenums.KindLabelFreeShipping, "免运费"),
		"KindDiscountLabel":     tr(membershipenums.KindLabelDiscount, "折扣"),
		"Err":                   d.Err,
		"Done":                  d.Done,
		"LoadFailed":            d.LoadFailed,
	}
}

// renderTiersPage 等级页的唯一渲染出口（正常与降级两条路都从这里出）。
func (h *membershipPageHandle) renderTiersPage(c *gin.Context, d *membershipTiersPageData) {
	c.HTML(http.StatusOK, "admin/membership/membership.html", shell.Prepare(c, d.templateMap(c)))
}

// MembershipPage 等级与权益页。
func (h *membershipPageHandle) MembershipPage(c *gin.Context) {
	ctx := c.Request.Context()
	pageErr := membershipPageErr(c)

	projects, loadErr := h.listProjects(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		// 装载失败压过 ?err=：它是这次请求真实发生的事，URL 里那条是上一次写失败的旧提示。
		pageErr = membershipErrPageText(c, loadErr)
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
			if pageErr == "" {
				pageErr = membershipErrPageText(c, lerr)
			}
		} else {
			for _, tier := range list {
				rows = append(rows, membershipTierRowOf(tier))
			}
			// ?edit=<id> 指向本工程的某个等级时渲染编辑区；指向不存在 / 别的工程的 id
			// 时**静默不显示**（列表里那个 id 本来就不该被点开，报错反而把「别人贴了个
			// 过期链接」变成一次可见的故障）。
			edit = membershipTierEditOf(list, formInt64(c.Query("edit"), 0))
		}
	}

	h.renderTiersPage(c, &membershipTiersPageData{
		Title:                 membershipenums.PageTiersTitle,
		Menu:                  "membership",
		Projects:              projects,
		SelectedProject:       selected,
		Tiers:                 rows,
		Edit:                  edit,
		KindFreeShippingValue: membershipenums.KindFreeShipping,
		KindDiscountValue:     membershipenums.KindDiscount,
		Err:                   pageErr,
		Done:                  membershipPageDone(c),
		LoadFailed:            loadFailed,
	})
}

// —— 归属页 ——

// membershipAssignmentsPageData 归属页的模板数据。
type membershipAssignmentsPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	TierOptions     []gin.H
	SourceOptions   []gin.H
	FilterTier      int64
	FilterSource    string
	FilterUser      uint64
	Rows            []membershipAssignRow
	Total           int64
	Err             string
	Done            string
	LoadFailed      bool
	// RecalcAvailable 消费额批量端口是否已接入（决定页面上「立即重算」按钮是否可用）。
	//
	// 显示而不是隐藏：运营需要知道「这个功能还没接线」，而不是点了按钮拿到一句
	// 「重算不可用」的报错 —— 后者会被当成故障上报。
	RecalcAvailable bool
	Pagination      *shell.PaginationData
}

// templateMap 转 Jet 模板键。
func (d *membershipAssignmentsPageData) templateMap(c *gin.Context) gin.H {
	tr := shell.TranslateFor(c)
	m := gin.H{
		"title":            d.Title,
		"menu":             d.Menu,
		"Projects":         d.Projects,
		"SelectedProject":  d.SelectedProject,
		"TierOptions":      d.TierOptions,
		"SourceOptions":    d.SourceOptions,
		"FilterTier":       d.FilterTier,
		"FilterSource":     d.FilterSource,
		"FilterUser":       d.FilterUser,
		"Rows":             d.Rows,
		"Total":            d.Total,
		"Err":              d.Err,
		"Done":             d.Done,
		"LoadFailed":       d.LoadFailed,
		"RecalcAvailable":  d.RecalcAvailable,
		"SourceManual":     membershipmodel.SourceManual,
		"SourceAuto":       membershipmodel.SourceAuto,
		"SourceManualText": tr(membershipenums.SourceLabelManual, "手工指定"),
		"SourceAutoText":   tr(membershipenums.SourceLabelAuto, "自动重算"),
	}
	// 分页条键（PaginationInfo / PaginationLinks）：nil 时给空 map，模板的
	// {{if .["PaginationLinks"]}} 自然跳过 —— 单页与装载失败两条路都不渲染分页条。
	for k, v := range d.Pagination.TemplateKeys() {
		m[k] = v
	}
	return m
}

// renderAssignmentsPage 归属页的唯一渲染出口。
func (h *membershipPageHandle) renderAssignmentsPage(c *gin.Context, d *membershipAssignmentsPageData) {
	c.HTML(http.StatusOK, "admin/membership/membership_assignments.html", shell.Prepare(c, d.templateMap(c)))
}

// MembershipAssignmentsPage 会员归属页。
func (h *membershipPageHandle) MembershipAssignmentsPage(c *gin.Context) {
	ctx := c.Request.Context()
	pageErr := membershipPageErr(c)

	projects, loadErr := h.listProjects(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = membershipErrPageText(c, loadErr)
	}

	selected := ""
	if !loadFailed {
		selected = h.pickProject(c.Query("project"), projects)
	}

	filterTier := formInt64(c.Query("tierId"), 0)
	filterSource := c.Query("source")
	filterUser := uint64(formInt64(c.Query("userId"), 0))

	rows := []membershipAssignRow{}
	tierOptions := []gin.H{}
	var total int64
	var pagination *shell.PaginationData

	if !loadFailed && selected != "" {
		// 等级下拉与行上的等级名都来自这一份清单：一次查询同时支撑筛选栏与列表渲染。
		tiers, terr := h.svc.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: selected})
		if terr != nil {
			if pageErr == "" {
				pageErr = membershipErrPageText(c, terr)
			}
		} else {
			tierOptions = membershipTierOptions(c, tiers, filterTier)
		}

		page, limit := normalizePageParams(c)
		n, cerr := h.svc.CountAssignments(ctx, &membershipdto.CountAssignmentsReq{
			ProjectID: selected, TierID: filterTier, Source: filterSource, UserID: filterUser,
		})
		if cerr != nil {
			if pageErr == "" {
				pageErr = membershipErrPageText(c, cerr)
			}
		} else {
			total = n
			page = clampPage(page, limit, total)
			list, lerr := h.svc.ListAssignments(ctx, &membershipdto.ListAssignmentsReq{
				ProjectID: selected, TierID: filterTier, Source: filterSource, UserID: filterUser,
				Page: page, Size: limit,
			})
			if lerr != nil {
				if pageErr == "" {
					pageErr = membershipErrPageText(c, lerr)
				}
			} else {
				for _, item := range list {
					rows = append(rows, membershipAssignRowOf(shell.TranslateFor(c), item))
				}
			}
			pagination = shell.BuildPagination(total, page, limit, shell.FilterBaseURL(
				membershipAssignmentsPath, map[string]string{
					"project": selected,
					"tierId":  itoa64(filterTier),
					"source":  filterSource,
					"userId":  itoaU64(filterUser),
				}), shell.TranslateFor(c))
		}
	}

	h.renderAssignmentsPage(c, &membershipAssignmentsPageData{
		Title:           membershipenums.PageAssignmentsTitle,
		Menu:            "membership-assignments",
		Projects:        projects,
		SelectedProject: selected,
		TierOptions:     tierOptions,
		SourceOptions:   membershipSourceOptions(c, filterSource),
		FilterTier:      filterTier,
		FilterSource:    filterSource,
		FilterUser:      filterUser,
		Rows:            rows,
		Total:           total,
		Err:             pageErr,
		Done:            membershipPageDone(c),
		LoadFailed:      loadFailed,
		RecalcAvailable: h.recalcAvailable(),
		Pagination:      pagination,
	})
}

// —— 写动作（表单 POST → 302 回列表）——
//
// 每个动作都只做「解析表单 → 调 service → 回跳」，业务判据全在 service：
// 页面这一层不该有第二份「谁能改什么」的规则（两份必然漂移，且漂移方向不可预测）。

// TierCreate 新建等级（可同时带权益）。
func (h *membershipPageHandle) TierCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	thresholdFen, aerr := parseYuanToFen(c.PostForm("thresholdYuan"))
	if aerr != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipTiersPath, projectID, aerr))
		return
	}
	req := &membershipdto.CreateTierReq{
		ProjectID:       projectID,
		Name:            c.PostForm("name"),
		SortOrder:       formInt(c.PostForm("sortOrder"), 0),
		ThresholdAmount: thresholdFen,
		IsDefault:       formBool(c.PostForm("isDefault")),
		Remark:          c.PostForm("remark"),
		Entitlements:    entitlementsFromForm(c),
	}
	if _, err := h.svc.CreateTier(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipTiersPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, membershipOKURL(membershipTiersPath, projectID, membershipNotice(c, membershipenums.MsgTierCreated)))
}

// TierUpdate 更新等级（表单**不含** is_default —— 那是独立的「设为默认」动作）。
func (h *membershipPageHandle) TierUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("id"), 0)
	thresholdFen, aerr := parseYuanToFen(c.PostForm("thresholdYuan"))
	if aerr != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipTiersPath, projectID, aerr))
		return
	}
	name := c.PostForm("name")
	remark := c.PostForm("remark")
	sortOrder := formInt(c.PostForm("sortOrder"), 0)
	req := &membershipdto.UpdateTierReq{
		ProjectID:       projectID,
		TierID:          tierID,
		Name:            &name,
		SortOrder:       &sortOrder,
		ThresholdAmount: &thresholdFen,
		Remark:          &remark,
	}
	if _, err := h.svc.UpdateTier(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipTiersPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, membershipOKURL(membershipTiersPath, projectID, membershipNotice(c, membershipenums.MsgTierUpdated)))
}

// TierDefault 把某个等级设为默认等级（原默认自动让位）。
//
// 独立动作而不是编辑表单里的一个复选框：复选框的「未勾选」会与「取消当前默认等级」
// 撞在一起（后者必须被拒绝），于是编辑备注这类无关操作会因为没勾那个框而失败。
// 独立成动作后语义是单向的（「设为默认」），不存在被误触发的取消路径。
func (h *membershipPageHandle) TierDefault(c *gin.Context) {
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("id"), 0)
	yes := true
	req := &membershipdto.UpdateTierReq{ProjectID: projectID, TierID: tierID, IsDefault: &yes}
	if _, err := h.svc.UpdateTier(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipTiersPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, membershipOKURL(membershipTiersPath, projectID, membershipNotice(c, membershipenums.MsgTierUpdated)))
}

// TierDelete 删除等级。
func (h *membershipPageHandle) TierDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("id"), 0)
	if err := h.svc.DeleteTier(c.Request.Context(), &membershipdto.DeleteTierReq{
		ProjectID: projectID, TierID: tierID,
	}); err != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipTiersPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, membershipOKURL(membershipTiersPath, projectID, membershipNotice(c, membershipenums.MsgTierDeleted)))
}

// EntitlementSave 全量保存某等级的权益（表单即最终状态）。
func (h *membershipPageHandle) EntitlementSave(c *gin.Context) {
	projectID := c.PostForm("projectId")
	tierID := formInt64(c.PostForm("tierId"), 0)
	req := &membershipdto.SaveEntitlementsReq{
		ProjectID: projectID,
		TierID:    tierID,
		Items:     entitlementsFromForm(c),
	}
	if _, err := h.svc.SaveEntitlements(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipTiersPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, membershipOKURL(membershipTiersPath, projectID, membershipNotice(c, membershipenums.MsgEntitlementSaved)))
}

// AssignSet 手工指定某访客的等级。
func (h *membershipPageHandle) AssignSet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &membershipdto.AssignManualReq{
		ProjectID: projectID,
		UserID:    uint64(formInt64(c.PostForm("userId"), 0)),
		TierID:    formInt64(c.PostForm("tierId"), 0),
	}
	if _, err := h.svc.AssignManual(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipAssignmentsPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, membershipOKURL(membershipAssignmentsPath, projectID, membershipNotice(c, membershipenums.MsgAssignSet)))
}

// AssignUnlock 取消手工锁定。
func (h *membershipPageHandle) AssignUnlock(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &membershipdto.UnlockManualReq{
		ProjectID: projectID,
		UserID:    uint64(formInt64(c.PostForm("userId"), 0)),
	}
	if err := h.svc.UnlockManual(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, membershipErrURL(c, membershipAssignmentsPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, membershipOKURL(membershipAssignmentsPath, projectID, membershipNotice(c, membershipenums.MsgAssignUnlocked)))
}

// —— 内部助手 ——

// listProjects 读工程清单（projects 未注入时返回空清单而不是错误 —— 页面仍可渲染）。
func (h *membershipPageHandle) listProjects(ctx context.Context) ([]projectcontract.ProjectResp, error) {
	if h.projects == nil {
		return nil, nil
	}
	return h.projects.List(ctx)
}

// pickProject 定出当前工程：URL 指定优先，否则取第一个。
func (h *membershipPageHandle) pickProject(raw string, projects []projectcontract.ProjectResp) string {
	if v := trimSpace(raw); v != "" {
		return v
	}
	if len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

// recalcAvailable 消费额批量端口是否已接入（未接入时页面上的「立即重算」按钮置灰并说明）。
func (h *membershipPageHandle) recalcAvailable() bool {
	if h.svc == nil {
		return false
	}
	// 用一次空工程的 RecalcProject 试是不行的（它有副作用）。
	// 端口是否注入只有实现知道，因此经一个**只读**的窄接口问它 —— 见 recalcProbe。
	if probe, ok := h.svc.(recalcProbe); ok {
		return probe.RecalcReady()
	}
	return false
}

// recalcProbe 判定「归属重算是否可用」的窄接口（由 service 实现）。
//
// 为什么不直接判端口类型：handler 拿不到 service 的私有字段，
// 而在 handler 里重新拿一次端口等于维护第二份状态来源。
type recalcProbe interface {
	RecalcReady() bool
}
