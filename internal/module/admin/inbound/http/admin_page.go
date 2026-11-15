package adminhttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/auth"
	"go_wp/pkg/binding"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/captcha"
	"go_wp/pkg/datarule"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
	pageview "go_wp/pkg/view"
)

type administratorRolesDrawerView struct {
	Account *admindto.AdminDetailResp
	Rows    []adminRoleChoice
}

type administratorMenusDrawerView struct {
	Account *admindto.AdminDetailResp
	Rows    []adminMenuTreeRow
}

type rolePermissionsDrawerView struct {
	Role *admindto.RolePermissionTreeResp
	Rows []permTreeRow
}

type adminRoleChoice struct {
	Code   string
	Name   string
	Status int

	Missing bool

	Checked bool
}

// AdministratorRolesDrawer 返回管理员角色分配的 Jet 表单。
func (h *AdminPagesHandle) AdministratorRolesDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.AdminRoleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	data, err := h.administratorRolesDrawerData(c, req.UserID, nil)
	if err != nil {
		pageview.FailScene(c, "admin.drawer", err)
		return
	}
	c.HTML(http.StatusOK, "admin/system/administrator_roles_drawer.html", data)
}

func (h *AdminPagesHandle) administratorRolesDrawerData(c *gin.Context, userID uint64, checkedOverride []string) (*administratorRolesDrawerView, error) {
	ctx := c.Request.Context()
	account, err := h.admins.AdminDetail(ctx, &admindto.AdminDetailReq{Id: userID})
	if err != nil {
		return nil, err
	}
	if account == nil || account.ID != userID {
		return nil, adminenums.Err(adminenums.ErrAdminNotFound)
	}

	all, err := h.listAllRoles(ctx)
	if err != nil {
		return nil, err
	}
	bound, err := h.admins.AdminRoleList(ctx, &admindto.AdminRoleListReq{UserID: userID})
	if err != nil {
		return nil, err
	}

	checked := make(map[string]struct{}, len(bound.List))
	for _, item := range bound.List {
		checked[item.RoleCode] = struct{}{}
	}
	if checkedOverride != nil {
		checked = make(map[string]struct{}, len(checkedOverride))
		for _, code := range checkedOverride {
			if code != "" {
				checked[code] = struct{}{}
			}
		}
	}

	rows := make([]adminRoleChoice, 0, len(all)+len(checked))
	seen := make(map[string]struct{}, len(all))
	for _, role := range all {
		_, on := checked[role.RoleCode]
		seen[role.RoleCode] = struct{}{}
		rows = append(rows, adminRoleChoice{Code: role.RoleCode, Name: role.RoleName, Status: role.Status, Checked: on})
	}

	for code := range checked {
		if _, ok := seen[code]; ok {
			continue
		}
		rows = append(rows, adminRoleChoice{Code: code, Checked: true, Missing: true})
	}

	sortRoleChoices(rows)

	return &administratorRolesDrawerView{Account: account, Rows: rows}, nil
}

func (h *AdminPagesHandle) listAllRoles(ctx context.Context) ([]admindto.RoleItem, error) {
	const pageSize = 100
	out := make([]admindto.RoleItem, 0, pageSize)
	for page := 1; page <= 50; page++ {
		res, err := h.roles.RoleList(ctx, &admindto.RoleListReq{
			RolePageReq: admindto.RolePageReq{Page: page, Limit: pageSize},
		})
		if err != nil {
			return nil, err
		}
		if res == nil || len(res.List) == 0 {
			break
		}
		out = append(out, res.List...)
		if int64(len(out)) >= res.Total {
			break
		}
	}
	return out, nil
}

func sortRoleChoices(rows []adminRoleChoice) {

	known := make([]adminRoleChoice, 0, len(rows))
	missing := make([]adminRoleChoice, 0, 2)
	for _, r := range rows {
		if r.Missing {
			missing = append(missing, r)
			continue
		}
		known = append(known, r)
	}
	copy(rows, known)
	copy(rows[len(known):], missing)
}

type adminMenuTreeRow struct {
	ID       uint64
	Title    string
	Type     int
	Depth    int
	ParentID uint64

	Checked bool

	Inherited       bool
	HasChildren     bool
	PermissionCodes []string
}

// AdministratorMenusDrawer 返回管理员直接授权与角色继承事实的 Jet 表单。
func (h *AdminPagesHandle) AdministratorMenusDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.AdminMenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	data, err := h.administratorMenusDrawerData(c, req.UserID, nil)
	if err != nil {
		pageview.FailScene(c, "admin.drawer", err)
		return
	}
	c.HTML(http.StatusOK, "admin/system/administrator_menus_drawer.html", data)
}

func (h *AdminPagesHandle) administratorMenusDrawerData(c *gin.Context, userID uint64, directOverride []uint64) (*administratorMenusDrawerView, error) {
	ctx := c.Request.Context()
	account, err := h.admins.AdminDetail(ctx, &admindto.AdminDetailReq{Id: userID})
	if err != nil {
		return nil, err
	}
	if account == nil || account.ID != userID {
		return nil, adminenums.Err(adminenums.ErrAdminNotFound)
	}

	trees, err := h.menus.MenuTree(ctx, &admindto.MenuTreeReq{})
	if err != nil {
		return nil, err
	}
	granted, err := h.admins.AdminMenuList(ctx, &admindto.AdminMenuListReq{UserID: userID})
	if err != nil {
		return nil, err
	}

	direct := idSet(granted.DirectMenuIDs)
	effective := idSet(granted.EffectiveMenuIDs)
	roleOnly := make(map[uint64]struct{}, len(effective))
	for id := range effective {
		if _, ok := direct[id]; !ok {
			roleOnly[id] = struct{}{}
		}
	}
	if directOverride != nil {
		direct = idSet(directOverride)
	}
	for id := range direct {
		effective[id] = struct{}{}
	}
	for id := range roleOnly {
		effective[id] = struct{}{}
	}

	rows := make([]adminMenuTreeRow, 0, 128)
	flattenGrantMenuTree(trees, 1, direct, effective, &rows)
	return &administratorMenusDrawerView{Account: account, Rows: rows}, nil
}

func idSet(ids []uint64) map[uint64]struct{} {
	out := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func flattenGrantMenuTree(nodes []admindto.MenuTreeNode, depth int, direct, effective map[uint64]struct{}, out *[]adminMenuTreeRow) {
	for _, n := range nodes {
		_, isDirect := direct[n.ID]
		_, isEffective := effective[n.ID]
		*out = append(*out, adminMenuTreeRow{
			ID: n.ID, Title: n.Title, Type: n.Type,
			Depth: depth, ParentID: n.ParentID,
			Checked: isDirect || isEffective, Inherited: !isDirect && isEffective,
			HasChildren: len(n.Children) > 0, PermissionCodes: n.PermissionCodes,
		})
		if len(n.Children) > 0 {
			flattenGrantMenuTree(n.Children, depth+1, direct, effective, out)
		}
	}
}

func (h *AdminPagesHandle) AdministratorsPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)
	name := strings.TrimSpace(c.Query("name"))
	email := strings.TrimSpace(c.Query("email"))
	res, err := h.admins.AdminList(c.Request.Context(), &admindto.AdminListReq{
		AdminPageReq: admindto.AdminPageReq{Page: page, Limit: limit},
		Name:         name,
		Email:        email,
	})
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	view := &admindto.AdminListPageResp{
		Menu:        "admins",
		Rows:        res.List,
		Total:       res.Total,
		FilterName:  name,
		FilterEmail: email,

		ListQuery: adminAdminsPage.listQuery(c),
	}

	if !pageview.AttachPaginationView(view, "/admin/administrators",
		map[string]string{"name": name, "email": email},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("管理员列表视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

func (h *AdminPagesHandle) AdministratorsCreate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.AdminCreateReq](c)
	if !ok {
		return
	}
	if _, err := h.admins.AdminCreate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) AdministratorsUpdate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.AdminEditReq](c)
	if !ok {
		return
	}
	if _, err := h.admins.AdminEdit(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) AdministratorsDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.AdminDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	if _, err := h.admins.AdminDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) AdministratorsBulkDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.AdminDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	req.OperatorID = builtin.CurrentUserID(c)
	res, err := h.admins.AdminDelete(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *AdminPagesHandle) DatarulesPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)
	domain := strings.TrimSpace(c.Query("domain"))
	res, err := h.rules.RuleList(c.Request.Context(), &admindto.RuleListReq{
		Page: page, Limit: limit, Domain: domain,
	})
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	domains, schemaErr := h.rules.RuleSchemaList(c.Request.Context())
	if schemaErr != nil {
		response.ErrorStatusAuto(c, adminErrScene, schemaErr)
		return
	}
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}
	view := &admindto.RuleListPageResp{
		Menu:         "datarules",
		Rows:         res.List,
		Total:        res.Total,
		Domains:      domains,
		FilterDomain: domain,

		ListQuery: adminDatarulesPage.listQuery(c),
	}
	if !pageview.AttachPaginationView(view, "/admin/datarules",
		map[string]string{"domain": domain},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("数据规则视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

func (h *AdminPagesHandle) DatarulesCreate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RuleCreateReq](c)
	if !ok {
		return
	}
	// 新建表单不带数据域配置：配置从空结构开始，由编辑页补齐。
	req.Config = admindto.RuleConfigDTO{}
	if err := h.rules.RuleCreate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DatarulesEditPage(c *gin.Context) {
	id := reqparams.ParseUint(c.Query("id"))
	if id == 0 {

		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	// err 与「查无此行」是两件事：err 非 nil 可能是数据库/连接故障，
	// 把它统一压成 ErrRuleNotFound 会让用户以为规则被删了，真正的原因却没有出口。
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil {
		response.ErrorStatusAuto(c, "admin.datarule", err)
		return
	}
	if detail == nil {
		response.ErrorStatusAuto(c, "admin.datarule", adminenums.Err(adminenums.ErrRuleNotFound))
		return
	}

	// 域选项读失败同样不能假成功：页面会渲染成「没有可选数据域」的空下拉，
	// 用户改了提交上去才发现域不存在。失败就按原 err 走统一出口。
	domains, err := h.rules.RuleSchemaList(c.Request.Context())
	if err != nil {
		response.ErrorStatusAuto(c, "admin.datarule", err)
		return
	}
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}

	schema, err := h.rules.RuleSchemaDetail(c.Request.Context(), &admindto.RuleSchemaDetailReq{Domain: detail.Domain})
	if err != nil {
		response.ErrorStatusAuto(c, "admin.datarule", err)
		return
	}

	pageview.Render(c, &admindto.DataruleEditPageResp{
		Menu:    "datarules",
		Detail:  detail,
		Domains: domains,
		Editor:  schema,
	})
}

func (h *AdminPagesHandle) DatarulesUpdate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RuleUpdateReq](c)
	if !ok {
		return
	}
	config, cerr := h.dataruleConfigFor(c, req.ID)
	if cerr != nil {
		response.ErrorStatusAuto(c, "admin.datarule", cerr)
		return
	}
	req.Config = config
	if err := h.rules.RuleUpdate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.datarule", err)
		return
	}
	response.Success(c)
}

// dataruleConfigFor 决定本次提交用哪份配置：
// 编辑器在场就解析整份表单提交，否则沿用库里已有配置（清空整个配置是另一件事）。
func (h *AdminPagesHandle) dataruleConfigFor(c *gin.Context, id uint64) (admindto.RuleConfigDTO, error) {
	if c.PostForm(dataruleEditorMarker) != "" {
		cfg, err := dataruleConfigFromForm(c)
		if err != nil {
			return admindto.RuleConfigDTO{}, adminenums.BadRequest()
		}
		return dataruleDropEmptyGroups(cfg), nil
	}
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil {
		return admindto.RuleConfigDTO{}, err
	}
	if detail == nil {
		return admindto.RuleConfigDTO{}, adminenums.Err(adminenums.ErrRuleNotFound)
	}
	return detail.Config, nil
}

func (h *AdminPagesHandle) DatarulesDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RuleDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DatarulesBulkDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RuleDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	response.Success(c)
}

type adminDeptRow struct {
	ID        uint64
	DeptName  string
	DeptCode  string
	ParentID  uint64
	Status    int
	SortOrder int
	Remark    string
	Indent    string
}

func flattenAdminDeptTree(nodes []admindto.DeptTreeNode, depth int, out *[]adminDeptRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		*out = append(*out, adminDeptRow{
			ID: n.ID, DeptName: n.DeptName, DeptCode: n.DeptCode, ParentID: n.ParentID,
			Status: n.Status, SortOrder: n.SortOrder, Remark: n.Remark, Indent: indent,
		})
		if len(n.Children) > 0 {
			flattenAdminDeptNodePtrs(n.Children, depth+1, out)
		}
	}
}

func flattenAdminDeptNodePtrs(nodes []*admindto.DeptTreeNode, depth int, out *[]adminDeptRow) {
	indent := strings.Repeat("　", (depth-1)*2)
	for _, n := range nodes {
		if n == nil {
			continue
		}
		*out = append(*out, adminDeptRow{
			ID: n.ID, DeptName: n.DeptName, DeptCode: n.DeptCode, ParentID: n.ParentID,
			Status: n.Status, SortOrder: n.SortOrder, Remark: n.Remark, Indent: indent,
		})
		if len(n.Children) > 0 {
			flattenAdminDeptNodePtrs(n.Children, depth+1, out)
		}
	}
}

func (h *AdminPagesHandle) DepartmentsPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	res, err := h.depts.DeptPage(c.Request.Context(), page, limit, keyword)
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	view := &admindto.DepartmentListPageResp{
		Menu:          "depts",
		Rows:          res.Rows,
		Parents:       res.Parents,
		Total:         res.Total,
		FilterKeyword: keyword,

		ListQuery: adminDeptsPage.listQuery(c),
	}
	if !pageview.AttachPaginationView(view, "/admin/departments",
		map[string]string{"keyword": keyword},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("部门视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

func (h *AdminPagesHandle) DepartmentsCreate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.DeptCreateReq](c)
	if !ok {
		return
	}
	if err := h.depts.DeptCreate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DepartmentsUpdate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.DeptUpdateReq](c)
	if !ok {
		return
	}
	if err := h.depts.DeptUpdate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DepartmentsDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.DeptDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DepartmentsBulkDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.DeptDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	response.Success(c)
}

type AdminPagesHandle struct {
	admins admincontract.AdminService
	roles  admincontract.RoleService
	perms  admincontract.PermService
	menus  admincontract.MenuService
	depts  admincontract.DeptService
	rules  admincontract.RuleService
}

func NewAdminPagesHandle(admins admincontract.AdminService, roles admincontract.RoleService,
	perms admincontract.PermService, menus admincontract.MenuService,
	depts admincontract.DeptService, rules admincontract.RuleService) *AdminPagesHandle {
	return &AdminPagesHandle{admins: admins, roles: roles, perms: perms, menus: menus, depts: depts, rules: rules}
}

const adminI18nEntryPageSize = 50

type i18nListView struct {
	Entries     []i18n.Entry
	Keyword     string
	LangFilter  string
	CatFilter   string
	Categories  []string
	LangOptions []sysconfigcontract.DictOption
	Pagination  pagination.Page
}

type adminI18nEntryHandle struct {
	pages adminI18nPageMarker

	dict sysconfigcontract.DictReader
}

type adminI18nPageMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

func (h *adminI18nEntryHandle) SetPageMarker(m adminI18nPageMarker) { h.pages = m }

func (h *adminI18nEntryHandle) SetDictReader(r sysconfigcontract.DictReader) { h.dict = r }

func NewAdminI18nEntryHandle() *adminI18nEntryHandle { return &adminI18nEntryHandle{} }

func (h *adminI18nEntryHandle) markI18nStale(c *gin.Context) {
	if h == nil || h.pages == nil {
		return
	}
	if err := h.pages.MarkStaleForI18n(c.Request.Context()); err != nil {
		logger.Scene(adminErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "词条变更后标记站点待重建失败")
	}
}

func adminI18nEntryFilterOf(c *gin.Context) i18n.EntryFilter {
	page, _ := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	if page < 1 {
		page = 1
	}
	return i18n.EntryFilter{
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Lang:     strings.TrimSpace(c.Query("lang")),
		Category: strings.TrimSpace(c.Query("category")),
		Limit:    adminI18nEntryPageSize,
		Offset:   (page - 1) * adminI18nEntryPageSize,
	}
}

func (h *adminI18nEntryHandle) I18nEntriesPage(c *gin.Context) {
	filter := adminI18nEntryFilterOf(c)
	items, total, err := i18n.ListEntries(c.Request.Context(), filter)
	if err != nil {

		pageview.FailStatus(c, http.StatusInternalServerError, "i18n", err)
		return
	}
	// 分类下拉读失败不能假成功：忽略 err 会渲染成「没有任何分类」，
	// 用户以为词库空了。列表本身已经读出来了，但页面是整体事实，失败就整页失败。
	categories, err := i18n.Categories(c.Request.Context())
	if err != nil {
		pageview.FailStatus(c, http.StatusInternalServerError, "i18n", err)
		return
	}
	page := filter.Offset/adminI18nEntryPageSize + 1
	if page < 1 {
		page = 1
	}
	data := &i18nListView{
		Entries:    items,
		Keyword:    filter.Keyword,
		LangFilter: filter.Lang,
		CatFilter:  filter.Category,
		Categories: categories,
	}

	if h.dict != nil {
		if opts, derr := h.dict.ListDictOptions(c.Request.Context(), "language"); derr == nil {
			data.LangOptions = opts
		}
	}

	base := reqparams.FilterBaseURL("/admin/i18n", map[string]string{
		"keyword":  filter.Keyword,
		"lang":     filter.Lang,
		"category": filter.Category,
	})

	if !pageview.AttachPaginationView(data, "", nil, pagination.Page{
		Total: total, Page: page, Limit: adminI18nEntryPageSize, Base: base,
	}) {
		logger.Scene("admin").Error(errors.New("文案词条视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, data)
}

// I18nEntryEditFragment 返回精确 key 与语言的 Jet 编辑表单。
func (h *adminI18nEntryHandle) I18nEntryEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.I18nEditReq
	if err := c.ShouldBindQuery(&req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Lang) == "" {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, adminenums.BadRequest())
		return
	}
	entry, err := i18n.GetEntry(c.Request.Context(), req.Key, req.Lang)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			pageview.FailStatus(c, http.StatusNotFound, adminErrScene, adminenums.BadRequest())
		} else {
			pageview.FailScene(c, "admin.i18nEntry", err)
		}
		return
	}
	c.HTML(http.StatusOK, "admin/system/i18n_edit_form.html", entry)
}

const adminI18nEntryKeySeparator = "|"

func AdminLangSwitch(c *gin.Context) {
	lang, _ := response.NormalizeLang(c.Query("lang"))
	response.SetLangCookie(c, lang)

	c.Redirect(http.StatusFound, adminSafeLangRedirect(c.Query("redirect")))
}

const adminHomePath = "/admin"

func adminSafeLangRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if p := safepath.LangRedirectPath(raw); p != "" {
		return p
	}
	return adminHomePath
}

func AdminLoginPage(c *gin.Context) {
	id, image := captcha.Get().GenerateImage()

	pageview.Render(c, &admindto.LoginPageResp{
		Title:        response.MsgText(c, facing.Msg{Key: adminenums.ShellLoginTitle, Fallback: "登录"}),
		CaptchaID:    id,
		CaptchaImage: image,

		DevLogin: AdminDevLoginEnabled(),
	})
}

func AdminDevLoginEnabled() bool {
	v, err := config.GetViper()
	return err == nil && strings.EqualFold(v.GetString("server.mode"), "debug")
}

func adminMenuTreeHas(nodes []admindto.MenuTreeNode, id uint64) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
		if adminMenuTreeHas(n.Children, id) {
			return true
		}
	}
	return false
}

func (h *AdminPagesHandle) MenusPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	res, err := h.menus.MenuPage(c.Request.Context(), page, limit, keyword)
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	// 列表页的行内编辑同样要带权限点清单：读失败就让整页失败，
	// 否则每一行打开抽屉都看不到可绑定的权限点。
	permChoices, err := h.buildPermChoices(c, nil)
	if err != nil {
		response.ErrorStatusAuto(c, "admin.menus", err)
		return
	}
	view := &admindto.MenuListPageResp{
		Menu:          "menus",
		Rows:          res.Rows,
		Parents:       res.Parents,
		Total:         res.Total,
		FilterKeyword: keyword,

		ListQuery: adminMenusPage.listQuery(c),

		SearchMode: keyword != "",

		PermChoices: permChoices,
	}
	if !pageview.AttachPaginationView(view, "/admin/menus",
		map[string]string{"keyword": keyword},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("菜单视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

// MenusEditFragment 返回由 Jet 渲染的菜单编辑抽屉。
func (h *AdminPagesHandle) MenusEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.MenuDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	row, err := h.menus.MenuDetail(c.Request.Context(), &req)
	if err != nil {
		pageview.FailScene(c, "admin.menuEdit", err)
		return
	}
	if row == nil || row.ID != req.ID {
		pageview.FailScene(c, "admin.menuEdit", adminenums.Err(adminenums.ErrMenuNotFound))
		return
	}
	data, err := menuEditData(c, h, row)
	if err != nil {
		pageview.FailScene(c, "admin.menuEdit", err)
		return
	}
	c.HTML(http.StatusOK, "admin/system/menu_edit_drawer.html", data)
}

type menuEditView struct {
	MenuEdit    *admindto.MenuDetailResp
	PermChoices []permChoiceGroup
	Parents     []admindto.MenuParentChoice
}

// menuEditData 交付字段与候选事实；任何候选读取失败均拒绝整个抽屉。
func menuEditData(c *gin.Context, h *AdminPagesHandle, row *admindto.MenuDetailResp) (*menuEditView, error) {
	choices, err := h.buildPermChoices(c, row.PermissionCodes)
	if err != nil {
		return nil, err
	}
	parents, err := h.menus.MenuParentOptions(c.Request.Context(), row.ID)
	if err != nil {
		return nil, err
	}
	return &menuEditView{
		MenuEdit:    row,
		PermChoices: choices,
		Parents:     parents,
	}, nil
}

type permChoiceItem struct {
	Code    string
	Name    string
	Checked bool
}

type permChoiceGroup struct {
	Module string
	Items  []permChoiceItem
}

// buildPermChoices 把权限点按模块分组，并标出这一行已勾选的那些。
//
// 读失败必须上抛：返回 nil 会被模板当成「没有可绑定的权限点」，用户看不到任何
// 勾选清单，保存时就把原有权限悄悄清掉了。
func (h *AdminPagesHandle) buildPermChoices(c *gin.Context, selected []string) ([]permChoiceGroup, error) {
	res, err := h.perms.PermOptions(c.Request.Context(), &admindto.PermOptionsReq{})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	picked := make(map[string]struct{}, len(selected))
	for _, code := range selected {
		picked[code] = struct{}{}
	}

	groups := make([]permChoiceGroup, 0, 8)
	index := make(map[string]int, 8)
	for _, opt := range res.List {
		i, ok := index[opt.Module]
		if !ok {
			i = len(groups)
			index[opt.Module] = i
			groups = append(groups, permChoiceGroup{Module: opt.Module})
		}
		_, on := picked[opt.PermissionCode]
		groups[i].Items = append(groups[i].Items, permChoiceItem{
			Code: opt.PermissionCode, Name: opt.PermissionName, Checked: on,
		})
	}
	return groups, nil
}

func menuPermissionCodes(c *gin.Context) []string {
	raw := c.PostFormArray("permission_codes")
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (h *AdminPagesHandle) MenusCreate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.MenuCreateReq](c)
	if !ok {
		return
	}
	if err := h.menus.MenuCreate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

// MenusUpdate 只绑定抽屉可编辑字段，未编辑字段保持原值。
func (h *AdminPagesHandle) MenusUpdate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[admindto.MenuUpdateFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: req.ID})
	if err != nil {
		pageview.FailScene(c, "admin.write", err)
		return
	}
	if row == nil || row.ID != req.ID {
		pageview.FailScene(c, "admin.write", adminenums.Err(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuUpdate(c.Request.Context(), &admindto.MenuUpdateReq{
		ID: req.ID, Title: req.Title, ParentID: req.ParentID,
		Type: req.Type, Path: req.Path, Status: req.Status,
		SortOrder: req.SortOrder, Remark: req.Remark,
		PermissionCodes: req.PermissionCodes, Icon: req.Icon,
		TitleKey: row.TitleKey, ExternalURL: row.ExternalURL,
		IsHidden: row.IsHidden, IsPublic: row.IsPublic,
	}); err != nil {
		pageview.FailScene(c, "admin.write", err)
		return
	}
	pageview.Success(c)
}

func (h *AdminPagesHandle) MenusDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.MenuDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	id := req.IDs[0]
	nodes, err := h.menus.MenuTree(c.Request.Context(), &admindto.MenuTreeReq{})
	if err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	if !adminMenuTreeHas(nodes, id) {
		response.ErrorStatusAuto(c, "admin.write", adminenums.Err(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) MenusBulkDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.MenuDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) PermissionsPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)

	code := strings.TrimSpace(c.Query("code"))
	module := strings.TrimSpace(c.Query("module"))
	res, err := h.perms.PermList(c.Request.Context(), &admindto.PermListReq{
		PermPageReq: admindto.PermPageReq{Page: page, Limit: limit},
		Code:        code,
		Module:      module,
	})
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	view := &admindto.PermListPageResp{
		Menu:         "permissions",
		Rows:         res.List,
		Total:        res.Total,
		FilterCode:   code,
		FilterModule: module,

		ListQuery: adminPermsPage.listQuery(c),
	}
	if !pageview.AttachPaginationView(view, "/admin/permissions",
		map[string]string{"code": code, "module": module},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("权限点列表视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

// PermissionsEditFragment 返回由 Jet 渲染的权限编辑抽屉。
func (h *AdminPagesHandle) PermissionsEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.PermDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	row, err := h.perms.PermDetail(c.Request.Context(), &req)
	if err != nil {
		pageview.FailScene(c, "admin.permEdit", err)
		return
	}
	if row == nil || row.ID != req.ID {
		pageview.FailScene(c, "admin.permEdit", adminenums.Err(adminenums.ErrPermissionNotFound))
		return
	}
	c.HTML(http.StatusOK, "admin/system/permission_edit_drawer.html", row)
}

func (h *AdminPagesHandle) PermissionsCreate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.PermCreateReq](c)
	if !ok {
		return
	}
	// 方法只接受 GET / POST：表单可能提交小写或空值，归一后再交给 service。
	req.APIMethod = strings.ToUpper(strings.TrimSpace(req.APIMethod))
	if req.APIMethod != "GET" && req.APIMethod != "POST" {
		req.APIMethod = "POST"
	}
	if _, err := h.perms.PermCreate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

// PermissionsUpdate 原生绑定完整编辑表单，结果由 Jet 输出。
func (h *AdminPagesHandle) PermissionsUpdate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[admindto.PermUpdateReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	if _, err := h.perms.PermUpdate(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, "admin.write", err)
		return
	}
	pageview.Success(c)
}

func (h *AdminPagesHandle) PermissionsDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.PermDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if _, err := h.perms.PermDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) PermissionsBulkDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.PermDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	res, err := h.perms.PermDelete(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *AdminPagesHandle) RolesPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	res, err := h.roles.RoleList(c.Request.Context(), &admindto.RoleListReq{
		RolePageReq: admindto.RolePageReq{Page: page, Limit: limit},
		Keyword:     keyword,
	})
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	view := &admindto.RoleListPageResp{
		Menu:          "roles",
		Rows:          res.List,
		Total:         res.Total,
		FilterKeyword: keyword,

		ListQuery: adminRolesPage.listQuery(c),
	}
	if !pageview.AttachPaginationView(view, "/admin/roles",
		map[string]string{"keyword": keyword},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("角色视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

type permTreeRow struct {
	ID    uint64
	Title string
	Type  int
	Depth int

	ParentID uint64

	PermissionCodes []string
	Status          int
	Checked         bool
	HasChildren     bool
}

func flattenPermissionTree(nodes []admindto.MenuTreeNode, depth int, checked map[uint64]struct{}, out *[]permTreeRow) {
	for _, n := range nodes {
		_, ok := checked[n.ID]
		*out = append(*out, permTreeRow{
			ID: n.ID, Title: n.Title, Type: n.Type,
			Depth:    depth,
			ParentID: n.ParentID, PermissionCodes: n.PermissionCodes,
			Status: n.Status, Checked: ok, HasChildren: len(n.Children) > 0,
		})
		if len(n.Children) > 0 {
			flattenPermissionTree(n.Children, depth+1, checked, out)
		}
	}
}

func (h *AdminPagesHandle) rolePermissionsDrawerData(c *gin.Context, roleID uint64) (*rolePermissionsDrawerView, error) {
	tree, err := h.roles.RolePermissionTree(c.Request.Context(), roleID)
	if err != nil {
		return nil, err
	}
	checkedIDs := tree.MenuIDs
	checked := make(map[uint64]struct{}, len(checkedIDs))
	for _, id := range checkedIDs {
		checked[id] = struct{}{}
	}
	rows := make([]permTreeRow, 0, len(checked)+64)
	flattenPermissionTree(tree.Tree, 1, checked, &rows)
	return &rolePermissionsDrawerView{Role: tree, Rows: rows}, nil
}

// RolePermissionsDrawer 返回角色权限树的 Jet 表单。
func (h *AdminPagesHandle) RolePermissionsDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.RoleMenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	data, err := h.rolePermissionsDrawerData(c, req.RoleID)
	if err != nil {
		pageview.FailScene(c, "admin.rolePermissions", err)
		return
	}
	c.HTML(http.StatusOK, "admin/system/role_permissions_drawer.html", data)
}

func (h *AdminPagesHandle) RolesCreate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RoleCreateReq](c)
	if !ok {
		return
	}
	if err := h.roles.RoleCreate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) RolesUpdate(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RoleUpdateReq](c)
	if !ok {
		return
	}
	if err := h.roles.RoleUpdate(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) RolesDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RoleDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) RolesBulkDelete(c *gin.Context) {
	req, ok := bindAdminForm[admindto.RoleDeleteReq](c)
	if !ok {
		return
	}
	if rejectBulkDeleteOverLimit(c, req.IDList()) {
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	response.Success(c)
}

const dataruleEditorMarker = "config_editor"

// dataruleConfigFromForm 把条件编辑器的**静态平行数组**还原成条件组。
//
// 线上键是 groupLogic（每组一个）与 condGroup / condField / condOp / condValue
// （每行各一个）：同名控件按 DOM 出现顺序保留多值，因此按顺序把状态、字段、
// 运算符与值配回同一行。配对失败一律返回错误，**不返回空配置** —— 空配置能通过
// service 的「空组」校验，会把畸形提交保存成「没有任何条件」，等于静默放宽数据权限。
//
// 组号只允许落在本次 groupLogic 声明的范围内：越界不按请求数字无限扩组。
func dataruleConfigFromForm(c *gin.Context) (admindto.RuleConfigDTO, error) {
	_ = c.Request.ParseForm()

	cfg := admindto.RuleConfigDTO{OmitFields: c.PostFormArray("omit_fields")}
	if cfg.OmitFields == nil {
		cfg.OmitFields = []string{}
	}

	logics := c.PostFormArray("groupLogic")
	groupIdx := c.PostFormArray("condGroup")
	fields := c.PostFormArray("condField")
	ops := c.PostFormArray("condOp")
	values := c.PostFormArray("condValue")

	// 行数组必须等长：缺一个控件会让后面的值串到前一条件上，整份配置直接拒绝。
	if len(groupIdx) != len(fields) || len(groupIdx) != len(ops) || len(groupIdx) != len(values) {
		return admindto.RuleConfigDTO{}, errDataruleConditionMismatch
	}
	// 没有条件行时允许没有组状态（清空配置的合法路径）；有条件行则每组必须有状态。
	if len(groupIdx) > 0 && len(logics) == 0 {
		return admindto.RuleConfigDTO{}, errDataruleConditionMismatch
	}

	groups := make([]admindto.RuleConditionGroupDTO, 0, len(logics))
	for _, logic := range logics {
		groups = append(groups, admindto.RuleConditionGroupDTO{Logic: strings.TrimSpace(logic)})
	}
	for i, rawGroup := range groupIdx {
		g, err := strconv.Atoi(strings.TrimSpace(rawGroup))
		if err != nil || g < 0 || g >= len(groups) {
			return admindto.RuleConfigDTO{}, errDataruleConditionMismatch
		}
		groups[g].Conditions = append(groups[g].Conditions, admindto.RuleConditionDTO{
			Field: strings.TrimSpace(fields[i]),
			Op:    strings.TrimSpace(ops[i]),
			Value: strings.TrimSpace(values[i]),
		})
	}
	cfg.ConditionGroups = groups
	return cfg, nil
}

// errDataruleConditionMismatch 是条件行配对的受控错误：控制器据此拒绝保存。
var errDataruleConditionMismatch = errors.New("datarule condition rows are not pairable")

func dataruleUsableConditions(rows []admindto.RuleConditionDTO) []admindto.RuleConditionDTO {
	kept := make([]admindto.RuleConditionDTO, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Field) == "" || strings.TrimSpace(row.Value) == "" {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

func dataruleDropEmptyGroups(cfg admindto.RuleConfigDTO) admindto.RuleConfigDTO {
	groups := make([]admindto.RuleConditionGroupDTO, 0, len(cfg.ConditionGroups))
	for _, group := range cfg.ConditionGroups {
		if conditions := dataruleUsableConditions(group.Conditions); len(conditions) > 0 {
			group.Conditions = conditions
			groups = append(groups, group)
		}
	}
	cfg.ConditionGroups = groups
	return cfg
}

func bootstrapDataRule(svc *adminservice.Service, db *gorm.DB) {
	domain, err := adminmodel.AdminDataRuleDomain()
	if err != nil {
		panic("admin 数据域声明不合法: " + err.Error())
	}
	if err := datarule.RegisterDomain(domain); err != nil {
		panic("admin 数据域注册失败: " + err.Error())
	}

	datarule.SetProvider(svc)
	if err := datarule.RegisterPluginWithDB(db); err != nil {
		logger.Scene("init").Error(err, "注册 datarule GORM 插件失败")
	}
}

func (h *Handle) DevLogin(c *gin.Context) {

	if !isLoopbackRemote(c.Request.RemoteAddr) {
		logger.Scene("admin").With("remote", c.Request.RemoteAddr).Warn("拒绝非本机的一键登录请求")

		c.String(http.StatusForbidden, response.MsgText(c, facing.Msg{Key: adminenums.DevLoginLoopbackOnly, Fallback: "开发登录仅限本机访问"}))
		return
	}

	res, err := h.admin.DevLogin(c.Request.Context(), strings.TrimSpace(c.Query("user")))
	if err != nil {
		logger.Scene("admin").Error(err, "开发登录失败")
		c.String(http.StatusInternalServerError, response.MsgText(c, facing.Msg{Key: adminenums.DevLoginFailed, Fallback: "开发登录失败，详情见服务端日志"}))
		return
	}

	if err := auth.SaveCookieSession(c, &auth.CookieSession{
		SessionID: res.SessionID,
	}, false); err != nil {
		logger.Scene("admin").Error(err, "开发登录写会话失败")
		c.String(http.StatusInternalServerError, response.MsgText(c, facing.Msg{Key: adminenums.DevLoginSessionFailed, Fallback: "写会话失败，详情见服务端日志"}))
		return
	}
	if _, err := builtin.RotateCSRFToken(c); err != nil {
		logger.Scene("admin").Error(err, "开发登录轮换 CSRF token 失败")
		c.String(http.StatusInternalServerError, response.MsgText(c, facing.Msg{Key: adminenums.DevLoginCSRFFailed, Fallback: "轮换 CSRF token 失败，详情见服务端日志"}))
		return
	}

	to := safeRedirect(c.Query("to"))
	logger.Scene("admin").With("username", res.Username).With("to", to).
		Info("开发阶段一键登录（仅 debug 模式可用）")
	c.Redirect(http.StatusFound, to)
}

func DevLoginHandler(db *gorm.DB) gin.HandlerFunc {
	return NewHandle(adminservice.NewService(db)).DevLogin
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func safeRedirect(to string) string {
	to = strings.TrimSpace(to)
	if to == "" || !strings.HasPrefix(to, "/") {
		return "/admin"
	}

	if strings.HasPrefix(to, "//") || strings.HasPrefix(to, "/\\") {
		return "/admin"
	}

	if safepath.HasControlRune(to) {
		return "/admin"
	}
	return to
}

// parseBulkIDs 把表单来的 id 字符串转成 uint64，并为非法 id 给出一条拒绝。
//
// 旧的批量循环用 `if id == 0 { continue }` 静默跳过：用户提交了 "abc" 或 "0" 时
// 什么都不会发生，界面上既不报错也不删除，只能自己猜。批量要么全成要么全败，
// 所以这里把非法 id 原样收集起来交点名 —— 由调用方翻成 data.items，不当成没提交过。
func parseBulkIDs(raw []string) (ids []uint64, invalid []string) {
	ids = make([]uint64, 0, len(raw))
	for _, item := range raw {
		id := reqparams.ParseUint(item)
		if id == 0 {
			invalid = append(invalid, item)
			continue
		}
		ids = append(ids, id)
	}
	return ids, invalid
}

// bindAdminForm 是后台页面写端点的统一表单绑定：原生 FormPost + DTO 的 tag 校验。
//
// 失败已由这里写出 401/400：参数错误的文案由词库给，控制器不自己拼。
// 这些端点在页面里是标准表单提交，绑定必须显式声明 FormPost —— 不能靠
// ShouldBind 的 Content-Type 自适应，否则同一个端点会悄悄接受两种形状。
// rejectBulkDeleteOverLimit 在绑定之后、调用业务之前拦下超限的批量删除。
//
// 上限是**请求形状**而不是业务规则：超限整批拒绝、只回一句受控提示，
// 不返回逐条明细。放在这里是为了在触碰 service 之前就失败；
// service 侧的 checkBulkDeleteLimit 是同一判据的第二道，防止别的调用方绕过 HTTP 层。
func rejectBulkDeleteOverLimit(c *gin.Context, ids []uint64) bool {
	raw := make([]string, 0, len(ids))
	for _, id := range ids {
		raw = append(raw, strconv.FormatUint(id, 10))
	}
	if _, err := bulkids.Normalize(raw); err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return true
	}
	return false
}

func bindAdminForm[T any](c *gin.Context) (*T, bool) {
	req, err := binding.ParseForm[T](c)
	if err != nil {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return nil, false
	}
	return req, true
}
