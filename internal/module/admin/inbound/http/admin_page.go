package adminhttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
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
	"go_wp/pkg/captcha"
	"go_wp/pkg/datarule"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
	pageview "go_wp/pkg/view"
	"go_wp/pkg/facing"
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
	username := reqparams.FieldValue(c, "username")
	email := reqparams.FieldValue(c, "email")
	password := reqparams.FieldValue(c, "password")
	if username == "" || email == "" || password == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if _, err := h.admins.AdminCreate(c.Request.Context(), &admindto.AdminCreateReq{
		Username: username, Email: email, Password: password,
		Phone: reqparams.FieldValue(c, "phone"), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) AdministratorsUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if _, err := h.admins.AdminEdit(c.Request.Context(), &admindto.AdminEditReq{
		Id: id, Username: reqparams.FieldValue(c, "username"), Phone: reqparams.FieldValue(c, "phone"),
		Email: reqparams.FieldValue(c, "email"), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) AdministratorsDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{
		Id: []uint64{id}, OperatorID: builtin.CurrentUserID(c),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) AdministratorsBulkDelete(c *gin.Context) {
	// 批量删除要么全成要么全败：一次进 service，同事务校验 + 删除。
	normalized, berr := adminservice.NormalizeBulkIDs(c.PostFormArray("ids"))
	if berr != nil {
		response.ErrorStatusAuto(c, adminErrScene, berr)
		return
	}
	res, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{
		Id: normalized, OperatorID: builtin.CurrentUserID(c),
	})
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
	ruleName := reqparams.FieldValue(c, "rule_name")
	domain := reqparams.FieldValue(c, "domain")
	if ruleName == "" || domain == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: admindto.RuleConfigDTO{},
		Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
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
	id := reqparams.ParseUint(c.PostForm("id"))
	ruleName := reqparams.FieldValue(c, "rule_name")
	domain := reqparams.FieldValue(c, "domain")

	if id == 0 || ruleName == "" || domain == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	config := admindto.RuleConfigDTO{}
	if c.PostForm(dataruleEditorMarker) != "" {
		config = dataruleDropEmptyGroups(dataruleConfigFromForm(c))
	} else {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil {
			response.ErrorStatusAuto(c, "admin.datarule", detailErr)
			return
		}
		if detail == nil {

			response.ErrorStatusAuto(c, "admin.datarule", adminenums.Err(adminenums.ErrRuleNotFound))
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.datarule", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DatarulesDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DatarulesBulkDelete(c *gin.Context) {
	// 批量删除要么全成要么全败：一次进 service，同事务校验 + 删除。
	normalized, berr := adminservice.NormalizeBulkIDs(c.PostFormArray("ids"))
	if berr != nil {
		response.ErrorStatusAuto(c, adminErrScene, berr)
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: normalized}); err != nil {
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
	deptName := reqparams.FieldValue(c, "dept_name")
	deptCode := reqparams.FieldValue(c, "dept_code")
	if deptName == "" || deptCode == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.depts.DeptCreate(c.Request.Context(), &admindto.DeptCreateReq{
		ParentID: reqparams.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DepartmentsUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	deptName := reqparams.FieldValue(c, "dept_name")
	deptCode := reqparams.FieldValue(c, "dept_code")
	if id == 0 || deptName == "" || deptCode == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.depts.DeptUpdate(c.Request.Context(), &admindto.DeptUpdateReq{
		ID: id, ParentID: reqparams.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DepartmentsDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) DepartmentsBulkDelete(c *gin.Context) {
	// 批量删除要么全成要么全败：一次进 service，同事务校验 + 删除。
	normalized, berr := adminservice.NormalizeBulkIDs(c.PostFormArray("ids"))
	if berr != nil {
		response.ErrorStatusAuto(c, adminErrScene, berr)
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{IDs: normalized}); err != nil {
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
	title := reqparams.FieldValue(c, "title")
	if title == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.menus.MenuCreate(c.Request.Context(), &admindto.MenuCreateReq{
		Title: title, ParentID: reqparams.ParseUint(c.PostForm("parent_id")),
		Type: reqparams.ParseStatus(c.PostForm("type")), Path: reqparams.FieldValue(c, "path"),
		Status: reqparams.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: reqparams.FieldValue(c, "remark"),
		PermissionCodes: menuPermissionCodes(c),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

// MenusUpdate 只绑定抽屉可编辑字段，未编辑字段保持原值。
func (h *AdminPagesHandle) MenusUpdate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.MenuUpdateFormReq
	if err := c.ShouldBindWith(&req, binding.FormPost); err != nil {
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
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	nodes, err := h.menus.MenuTree(c.Request.Context(), &admindto.MenuTreeReq{})
	if err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	if !adminMenuTreeHas(nodes, id) {
		response.ErrorStatusAuto(c, "admin.write", adminenums.Err(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) MenusBulkDelete(c *gin.Context) {
	// 批量删除要么全成要么全败：一次进 service，同事务校验 + 删除。
	normalized, berr := adminservice.NormalizeBulkIDs(c.PostFormArray("ids"))
	if berr != nil {
		response.ErrorStatusAuto(c, adminErrScene, berr)
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: normalized}); err != nil {
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
	code := reqparams.FieldValue(c, "permission_code")
	name := reqparams.FieldValue(c, "permission_name")
	module := reqparams.FieldValue(c, "module")
	apiPath := reqparams.FieldValue(c, "api_path")
	apiMethod := strings.ToUpper(reqparams.FieldValue(c, "api_method"))
	if code == "" || name == "" || module == "" || apiPath == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if apiMethod != "GET" && apiMethod != "POST" {
		apiMethod = "POST"
	}
	if _, err := h.perms.PermCreate(c.Request.Context(), &admindto.PermCreateReq{
		PermissionCode: code, PermissionName: name, Module: module,
		APIPath: apiPath, APIMethod: apiMethod, Status: reqparams.ParseStatus(c.PostForm("status")),
		Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

// PermissionsUpdate 原生绑定完整编辑表单，结果由 Jet 输出。
func (h *AdminPagesHandle) PermissionsUpdate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req admindto.PermUpdateReq
	if err := c.ShouldBindWith(&req, binding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, adminErrScene, err)
		return
	}
	if _, err := h.perms.PermUpdate(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, "admin.write", err)
		return
	}
	pageview.Success(c)
}

func (h *AdminPagesHandle) PermissionsDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) PermissionsBulkDelete(c *gin.Context) {
	// 批量删除要么全成要么全败：一次进 service，同事务校验 + 删除。
	normalized, berr := adminservice.NormalizeBulkIDs(c.PostFormArray("ids"))
	if berr != nil {
		response.ErrorStatusAuto(c, adminErrScene, berr)
		return
	}
	res, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: normalized})
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
	roleCode := reqparams.FieldValue(c, "role_code")
	roleName := reqparams.FieldValue(c, "role_name")
	if roleCode == "" || roleName == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.roles.RoleCreate(c.Request.Context(), &admindto.RoleCreateReq{
		RoleCode: roleCode, RoleName: roleName, Status: reqparams.ParseStatusPtr(c.PostForm("status")),
		SortOrder: sortOrder, Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) RolesUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	roleName := reqparams.FieldValue(c, "role_name")
	if id == 0 || roleName == "" {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.roles.RoleUpdate(c.Request.Context(), &admindto.RoleUpdateReq{
		ID: id, RoleName: roleName, Status: reqparams.ParseStatus(c.PostForm("status")),
		SortOrder: sortOrder, Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) RolesDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		response.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
		response.ErrorStatusAuto(c, "admin.write", err)
		return
	}
	response.Success(c)
}

func (h *AdminPagesHandle) RolesBulkDelete(c *gin.Context) {
	// 批量删除要么全成要么全败：一次进 service，同事务校验 + 删除。
	normalized, berr := adminservice.NormalizeBulkIDs(c.PostFormArray("ids"))
	if berr != nil {
		response.ErrorStatusAuto(c, adminErrScene, berr)
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{IDs: normalized}); err != nil {
		response.ErrorStatusAuto(c, adminErrScene, err)
		return
	}
	response.Success(c)
}

const dataruleEditorMarker = "config_editor"

var dataruleFormRowRe = regexp.MustCompile(`^groups\[(\d+)\]\.conditions\[(\d+)\]\.(field|op|value)$`)

var dataruleFormLogicRe = regexp.MustCompile(`^groups\[(\d+)\]\.logic$`)

func dataruleConfigFromForm(c *gin.Context) admindto.RuleConfigDTO {

	_ = c.Request.ParseForm()

	cfg := admindto.RuleConfigDTO{OmitFields: c.PostFormArray("omit_fields")}
	if cfg.OmitFields == nil {
		cfg.OmitFields = []string{}
	}

	type rowKey struct{ group, row int }
	rows := map[rowKey]*admindto.RuleConditionDTO{}
	logics := map[int]string{}
	maxGroup, maxRow := -1, map[int]int{}
	for key, values := range c.Request.PostForm {
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		if m := dataruleFormRowRe.FindStringSubmatch(key); m != nil {
			g, _ := strconv.Atoi(m[1])
			i, _ := strconv.Atoi(m[2])
			item, ok := rows[rowKey{g, i}]
			if !ok {
				item = &admindto.RuleConditionDTO{}
				rows[rowKey{g, i}] = item
			}
			switch m[3] {
			case "field":
				item.Field = strings.TrimSpace(value)
			case "op":
				item.Op = strings.TrimSpace(value)
			case "value":
				item.Value = strings.TrimSpace(value)
			}
			if g > maxGroup {
				maxGroup = g
			}
			if i+1 > maxRow[g] {
				maxRow[g] = i + 1
			}
			continue
		}
		if m := dataruleFormLogicRe.FindStringSubmatch(key); m != nil {
			g, _ := strconv.Atoi(m[1])
			logics[g] = strings.TrimSpace(value)
			if g > maxGroup {
				maxGroup = g
			}
		}
	}

	groups := make([]admindto.RuleConditionGroupDTO, 0, maxGroup+1)
	for g := 0; g <= maxGroup; g++ {
		group := admindto.RuleConditionGroupDTO{Logic: logics[g]}
		for i := 0; i < maxRow[g]; i++ {
			if item, ok := rows[rowKey{g, i}]; ok {
				group.Conditions = append(group.Conditions, *item)
			}
		}
		groups = append(groups, group)
	}
	cfg.ConditionGroups = groups
	return cfg
}

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
