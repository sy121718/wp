package adminhttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/auth"
	"go_wp/pkg/backpath"
	"go_wp/pkg/captcha"
	"go_wp/pkg/datarule"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
	pageview "go_wp/pkg/view"
)

type adminRoleChoice struct {
	Code   string
	Name   string
	Status int

	Missing bool

	Checked bool
}

func (h *AdminPagesHandle) AdministratorRolesDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID := reqparams.ParseUint(c.Query("user_id"))
	if userID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	data, err := h.administratorRolesDrawerData(c, userID, nil)
	if err != nil {
		adminErrMsg(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	pageview.Render(c, data)
}

func (h *AdminPagesHandle) administratorRolesDrawerData(c *gin.Context, userID uint64, checkedOverride []string) (*admindto.RoleDrawerResp, error) {
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

	return &admindto.RoleDrawerResp{Account: account, Rows: rows}, nil
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

func (h *AdminPagesHandle) AdministratorRolesSave(c *gin.Context) {
	userID := reqparams.ParseUint(c.PostForm("user_id"))
	if userID == 0 {
		adminAdminsPage.paramFail(c)
		return
	}
	codes := make([]string, 0, 8)
	for _, v := range c.PostFormArray("role_codes") {
		if v = strings.TrimSpace(v); v != "" {
			codes = append(codes, v)
		}
	}
	if _, err := h.admins.AdminRoleSave(c.Request.Context(), &admindto.AdminRoleSaveReq{
		UserID: userID, RoleCodes: codes, OperatorID: builtin.CurrentUserID(c),
	}); err != nil {
		msg := adminErrMsg(c, err)
		data, loadErr := h.administratorRolesDrawerData(c, userID, codes)
		if !adminDrawerHX(c) || loadErr != nil {
			adminAdminsPage.fail(c, msg)
			return
		}

		data.Err = adminui.MsgText(c, msg)
		pageview.Render(c, data)
		return
	}
	if adminDrawerHX(c) {
		pageview.Render(c, &admindto.DrawerSavedResp{Saved: true})
		return
	}
	adminAdminsPage.done(c)
}

type adminMenuTreeRow struct {
	ID        uint64
	Title     string
	Type      int
	TypeLabel string
	Depth     int
	PadLeft   int
	ParentID  uint64

	Checked bool

	Inherited   bool
	HasChildren bool
}

func (h *AdminPagesHandle) AdministratorMenusDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID := reqparams.ParseUint(c.Query("user_id"))
	if userID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	data, err := h.administratorMenusDrawerData(c, userID, nil)
	if err != nil {
		adminErrMsg(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	pageview.Render(c, data)
}

func (h *AdminPagesHandle) administratorMenusDrawerData(c *gin.Context, userID uint64, directOverride []uint64) (*admindto.MenuDrawerResp, error) {
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
	flattenGrantMenuTree(adminLabelTranslator(c), trees, 1, direct, effective, &rows)
	return &admindto.MenuDrawerResp{Account: account, Rows: rows}, nil
}

func idSet(ids []uint64) map[uint64]struct{} {
	out := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func flattenGrantMenuTree(tr adminLabelTranslate, nodes []admindto.MenuTreeNode, depth int, direct, effective map[uint64]struct{}, out *[]adminMenuTreeRow) {
	for _, n := range nodes {
		_, isDirect := direct[n.ID]
		_, isEffective := effective[n.ID]
		*out = append(*out, adminMenuTreeRow{
			ID: n.ID, Title: n.Title, Type: n.Type, TypeLabel: adminMenuTypeLabel(tr, n.Type),
			Depth: depth, PadLeft: (depth-1)*permRowIndentStep + 10, ParentID: n.ParentID,
			Checked: isDirect || isEffective, Inherited: !isDirect && isEffective,
			HasChildren: len(n.Children) > 0,
		})
		if len(n.Children) > 0 {
			flattenGrantMenuTree(tr, n.Children, depth+1, direct, effective, out)
		}
	}
}

func (h *AdminPagesHandle) AdministratorMenusSave(c *gin.Context) {
	userID := reqparams.ParseUint(c.PostForm("user_id"))
	if userID == 0 {
		adminAdminsPage.paramFail(c)
		return
	}
	raw := c.PostFormArray("menu_ids")
	menuIDs := make([]uint64, 0, len(raw))
	for _, v := range raw {
		if id := reqparams.ParseUint(v); id != 0 {
			menuIDs = append(menuIDs, id)
		}
	}
	if _, err := h.admins.AdminMenuSave(c.Request.Context(), &admindto.AdminMenuSaveReq{
		UserID: userID, MenuIDs: menuIDs, OperatorID: builtin.CurrentUserID(c),
	}); err != nil {
		msg := adminErrMsg(c, err)
		data, loadErr := h.administratorMenusDrawerData(c, userID, menuIDs)
		if !adminDrawerHX(c) || loadErr != nil {
			adminAdminsPage.fail(c, msg)
			return
		}

		data.Err = adminui.MsgText(c, msg)
		pageview.Render(c, data)
		return
	}
	if adminDrawerHX(c) {
		pageview.Render(c, &admindto.DrawerSavedResp{Saved: true})
		return
	}
	adminAdminsPage.done(c)
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

		res = &admindto.AdminListResp{}
	}
	view := &admindto.AdminListPageResp{
		Title:       pagesMsgAdministratorsTitle,
		Menu:        "admins",
		Rows:        res.List,
		Total:       res.Total,
		FilterName:  name,
		FilterEmail: email,
		Err:         adminListLoadErr(c, err),
		ListQuery:   adminAdminsPage.listQuery(c),
	}

	if !adminui.AttachPaginationView(view, "/admin/administrators",
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
		adminAdminsPage.paramFail(c)
		return
	}
	if _, err := h.admins.AdminCreate(c.Request.Context(), &admindto.AdminCreateReq{
		Username: username, Email: email, Password: password,
		Phone: reqparams.FieldValue(c, "phone"), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminAdminsPage.writeFail(c, err)
		return
	}
	adminAdminsPage.done(c)
}

func (h *AdminPagesHandle) AdministratorsUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminAdminsPage.paramFail(c)
		return
	}
	if _, err := h.admins.AdminEdit(c.Request.Context(), &admindto.AdminEditReq{
		Id: id, Username: reqparams.FieldValue(c, "username"), Phone: reqparams.FieldValue(c, "phone"),
		Email: reqparams.FieldValue(c, "email"), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminAdminsPage.writeFail(c, err)
		return
	}
	adminAdminsPage.done(c)
}

func (h *AdminPagesHandle) AdministratorsDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminAdminsPage.paramFail(c)
		return
	}
	if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{
		Id: []uint64{id}, OperatorID: builtin.CurrentUserID(c),
	}); err != nil {
		adminAdminsPage.writeFail(c, err)
		return
	}
	adminAdminsPage.done(c)
}

func (h *AdminPagesHandle) AdministratorsBulkDelete(c *gin.Context) {
	operatorID := builtin.CurrentUserID(c)

	ids, berr := shell.BulkIDs(c)
	if berr != nil {

		adminAdminsPage.fail(c, adminBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			continue
		}
		if _, err := h.admins.AdminDelete(c.Request.Context(), &admindto.AdminDeleteReq{
			Id: []uint64{id}, OperatorID: operatorID,
		}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := adminBulkOutcome(c, adminBulkNounAdmin, deleted, skipped)
	adminAdminsPage.jump(c, ok, msg)
}

func (h *AdminPagesHandle) DatarulesPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)
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
	view := &admindto.RuleListPageResp{
		Title:        pagesMsgDatarulesTitle,
		Menu:         "datarules",
		Rows:         res.List,
		Total:        res.Total,
		Domains:      domains,
		FilterDomain: domain,
		Err:          adminListLoadErr(c, err),
		ListQuery:    adminDatarulesPage.listQuery(c),
	}
	if !adminui.AttachPaginationView(view, "/admin/datarules",
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
		adminDatarulesPage.paramFail(c)
		return
	}
	if err := h.rules.RuleCreate(c.Request.Context(), &admindto.RuleCreateReq{
		RuleName: ruleName, Domain: domain, Config: admindto.RuleConfigDTO{},
		Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminDatarulesPage.writeFail(c, err)
		return
	}
	adminDatarulesPage.done(c)
}

func (h *AdminPagesHandle) DatarulesEditPage(c *gin.Context) {
	id := reqparams.ParseUint(c.Query("id"))
	if id == 0 {

		adminDatarulesPage.paramFail(c)
		return
	}
	detail, err := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
	if err != nil || detail == nil {

		adminDatarulesPage.writeFail(c, adminenums.Err(adminenums.ErrRuleNotFound))
		return
	}
	domains, _ := h.rules.RuleSchemaList(c.Request.Context())
	if domains == nil {
		domains = []admindto.RuleDomainItem{}
	}

	editor := h.dataruleEditorContext(c, detail.Domain, detail.Config)

	pageview.Render(c, &admindto.DataruleEditPageResp{
		Title:   pagesMsgDatarulesTitle,
		Menu:    "datarules",
		Detail:  detail,
		Domains: domains,
		Editor:  editor,
	})
}

func adminDataruleBack(c *gin.Context, id uint64) (back string, backText adminui.Msg) {
	if id == 0 || c.PostForm(dataruleEditorMarker) == "" {
		return adminDatarulesPage.backPath(c), adminDatarulesBackMsg
	}
	back = backpath.WithParams("/admin/datarules/edit", map[string]string{"id": strconv.FormatUint(id, 10)})

	return back, adminui.Msg{Key: adminenums.DatarulesBackToEdit, Fallback: "返回编辑页"}
}

func (h *AdminPagesHandle) DatarulesUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	ruleName := reqparams.FieldValue(c, "rule_name")
	domain := reqparams.FieldValue(c, "domain")

	back, backText := adminDataruleBack(c, id)
	if id == 0 || ruleName == "" || domain == "" {
		adminJump(c, false, adminBadRequestMsg, back, backText)
		return
	}
	config := admindto.RuleConfigDTO{}
	if c.PostForm(dataruleEditorMarker) != "" {
		config = dataruleDropEmptyGroups(dataruleConfigFromForm(c))
	} else {
		detail, detailErr := h.rules.RuleDetail(c.Request.Context(), &admindto.RuleDetailReq{ID: id})
		if detailErr != nil {
			adminJump(c, false, adminErrMsg(c, detailErr), back, backText)
			return
		}
		if detail == nil {

			adminJump(c, false, adminErrMsg(c, adminenums.Err(adminenums.ErrRuleNotFound)), back, backText)
			return
		}
		config = detail.Config
	}
	if err := h.rules.RuleUpdate(c.Request.Context(), &admindto.RuleUpdateReq{
		ID: id, RuleName: ruleName, Domain: domain, Config: config,
		Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminJump(c, false, adminErrMsg(c, err), back, backText)
		return
	}
	adminDatarulesPage.done(c)
}

func (h *AdminPagesHandle) DatarulesDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminDatarulesPage.paramFail(c)
		return
	}
	if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
		adminDatarulesPage.writeFail(c, err)
		return
	}
	adminDatarulesPage.done(c)
}

func (h *AdminPagesHandle) DatarulesBulkDelete(c *gin.Context) {

	ids, berr := shell.BulkIDs(c)
	if berr != nil {

		adminDatarulesPage.fail(c, adminBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.rules.RuleDelete(c.Request.Context(), &admindto.RuleDeleteReq{IDs: []uint64{id}}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := adminBulkOutcome(c, adminBulkNounDatarule, deleted, skipped)
	adminDatarulesPage.jump(c, ok, msg)
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
		res = &admindto.DeptPageResp{}
	}
	view := &admindto.DepartmentListPageResp{
		Title:         pagesMsgDepartmentsTitle,
		Menu:          "depts",
		Rows:          res.Rows,
		Parents:       res.Parents,
		Total:         res.Total,
		FilterKeyword: keyword,
		Err:           adminListLoadErr(c, err),
		ListQuery:     adminDeptsPage.listQuery(c),
	}
	if !adminui.AttachPaginationView(view, "/admin/departments",
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
		adminDeptsPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.depts.DeptCreate(c.Request.Context(), &admindto.DeptCreateReq{
		ParentID: reqparams.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminDeptsPage.writeFail(c, err)
		return
	}
	adminDeptsPage.done(c)
}

func (h *AdminPagesHandle) DepartmentsUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	deptName := reqparams.FieldValue(c, "dept_name")
	deptCode := reqparams.FieldValue(c, "dept_code")
	if id == 0 || deptName == "" || deptCode == "" {
		adminDeptsPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.depts.DeptUpdate(c.Request.Context(), &admindto.DeptUpdateReq{
		ID: id, ParentID: reqparams.ParseUint(c.PostForm("parent_id")), DeptName: deptName, DeptCode: deptCode,
		SortOrder: sortOrder, Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminDeptsPage.writeFail(c, err)
		return
	}
	adminDeptsPage.done(c)
}

func (h *AdminPagesHandle) DepartmentsDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminDeptsPage.paramFail(c)
		return
	}
	if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
		adminDeptsPage.writeFail(c, err)
		return
	}
	adminDeptsPage.done(c)
}

func (h *AdminPagesHandle) DepartmentsBulkDelete(c *gin.Context) {

	ids, berr := shell.BulkIDs(c)
	if berr != nil {

		adminDeptsPage.fail(c, adminBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.depts.DeptDelete(c.Request.Context(), &admindto.DeptDeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := adminBulkOutcome(c, adminBulkNounDept, deleted, skipped)
	adminDeptsPage.jump(c, ok, msg)
}

const (
	pagesMsgAdministratorsTitle = "MsgAdministratorsTitle"
	pagesMsgRolesTitle          = "MsgRolesTitle"
	pagesMsgMenusTitle          = "MsgMenusTitle"
	pagesMsgPermissionsTitle    = "MsgPermissionsTitle"
	pagesMsgDepartmentsTitle    = "MsgDepartmentsTitle"
	pagesMsgDatarulesTitle      = "MsgDatarulesTitle"
)

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

type adminLabelTranslate = func(key, fallback string) string

func adminLabelTranslator(c *gin.Context) adminLabelTranslate {
	return func(key, fallback string) string { return adminui.TranslateArgs(c, key, fallback, nil) }
}

func adminMenuTypeLabel(tr adminLabelTranslate, t int) string {
	key, fallback := adminenums.MenuTypeLabel(t)
	if tr == nil || strings.TrimSpace(key) == "" {
		return fallback
	}
	return tr(key, fallback)
}

func adminDrawerHX(c *gin.Context) bool {
	return reqparams.IsHXRequest(c)
}

const adminI18nEntryPageSize = 50

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

		adminui.PageErrorInternal(c, "i18n", err)
		return
	}
	categories, _ := i18n.Categories(c.Request.Context())
	page := filter.Offset/adminI18nEntryPageSize + 1
	if page < 1 {
		page = 1
	}
	editURLs := make([]string, len(items))
	for idx, entry := range items {

		query := url.Values{"key": {entry.Key}, "lang": {entry.Lang}}
		editURLs[idx] = "/admin/i18n/edit?" + query.Encode()
	}
	data := &admindto.I18nListPageResp{

		Title:        adminenums.I18nTitle,
		Entries:      items,
		I18nEditURLs: editURLs,
		Keyword:      filter.Keyword,
		LangFilter:   filter.Lang,
		CatFilter:    filter.Category,
		Categories:   categories,
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

	if !adminui.AttachPaginationView(data, "", nil, pagination.Page{
		Total: total, Page: page, Limit: adminI18nEntryPageSize, Base: base,
	}) {
		logger.Scene("admin").Error(errors.New("文案词条视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, data)
}

func adminI18nSaveMissingMsg(key, lang, value string) string {
	switch {
	case key == "":
		return adminenums.ErrI18nKeyEmpty
	case lang == "":
		return adminenums.ErrI18nLangEmpty
	case strings.TrimSpace(value) == "":
		return adminenums.ErrI18nValueEmpty
	}
	return ""
}

func adminI18nDeleteMissingMsg(key, lang string) string {
	switch {
	case key == "":
		return adminenums.ErrI18nKeyEmpty
	case lang == "":
		return adminenums.ErrI18nLangEmpty
	}
	return ""
}

func (h *adminI18nEntryHandle) I18nEntryEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	key, lang := strings.TrimSpace(c.Query("key")), strings.TrimSpace(c.Query("lang"))
	if key == "" || lang == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	entry, err := i18n.GetEntry(c.Request.Context(), key, lang)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(http.StatusNotFound)
		} else {
			adminErrMsg(c, err)
			c.Status(http.StatusInternalServerError)
		}
		return
	}
	pageview.Render(c, &admindto.I18nEditFormResp{
		I18nEdit: entry,
	})
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
		Title:        adminui.MsgText(c, adminui.Msg{Key: adminenums.ShellLoginTitle, Fallback: "登录"}),
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
		res = &admindto.MenuPageResp{}
	}
	view := &admindto.MenuListPageResp{
		Title:         pagesMsgMenusTitle,
		Menu:          "menus",
		Rows:          res.Rows,
		Parents:       res.Parents,
		Total:         res.Total,
		FilterKeyword: keyword,
		Err:           adminListLoadErr(c, err),
		ListQuery:     adminMenusPage.listQuery(c),

		SearchMode: keyword != "",

		PermChoices: h.buildPermChoices(c, nil),
	}
	if !adminui.AttachPaginationView(view, "/admin/menus",
		map[string]string{"keyword": keyword},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("菜单视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

func (h *AdminPagesHandle) MenusEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := reqparams.ParseUint(c.Query("id"))
	if id == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: id})
	if err != nil {
		adminErrMsg(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	if row == nil || row.ID != id {
		c.Status(http.StatusNotFound)
		return
	}
	pageview.Render(c, menuEditData(c, h, row, nil))
}

func menuEditData(c *gin.Context, h *AdminPagesHandle, row *admindto.MenuDetailResp,
	echo *admindto.MenuEditFormResp) *admindto.MenuEditFormResp {
	data := &admindto.MenuEditFormResp{
		MenuEdit: row, PermChoices: h.buildPermChoices(c, row.PermissionCodes),
	}

	if echo != nil {
		data.MenuEditErr = echo.MenuEditErr
		data.MenuEditEcho = echo.MenuEditEcho
		data.MenuEditEchoCodes = echo.MenuEditEchoCodes
		data.MenuEditParent = echo.MenuEditParent
		data.PermChoices = echo.PermChoices
	}
	if parents, err := h.menus.MenuParentOptions(c.Request.Context(), row.ID); err == nil {
		data.Parents = parents
	}
	return data
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

func (h *AdminPagesHandle) buildPermChoices(c *gin.Context, selected []string) []permChoiceGroup {
	res, err := h.perms.PermOptions(c.Request.Context(), &admindto.PermOptionsReq{})
	if err != nil || res == nil {

		if err != nil {
			adminErrMsg(c, err)
		}
		return nil
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
	return groups
}

func (h *AdminPagesHandle) menuEditFail(c *gin.Context, msg adminui.Msg) {
	if !adminDrawerHX(c) {

		adminMenusPage.fail(c, msg)
		return
	}
	id := reqparams.ParseUint(c.PostForm("id"))
	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: id})
	if id == 0 || err != nil || row == nil || row.ID != id {

		adminMenusPage.fail(c, msg)
		return
	}

	echo := &admindto.MenuEditFormResp{
		MenuEditErr: adminui.MsgText(c, msg),
		MenuEditEcho: map[string]string{
			"title": c.PostForm("title"), "parent_id": c.PostForm("parent_id"),
			"type": c.PostForm("type"), "status": c.PostForm("status"),
			"path": c.PostForm("path"), "icon": c.PostForm("icon"),
			"sort_order": c.PostForm("sort_order"), "remark": c.PostForm("remark"),
		},
		MenuEditEchoCodes: menuPermissionCodes(c),

		PermChoices: h.buildPermChoices(c, menuPermissionCodes(c)),

		MenuEditParent: reqparams.ParseUint(c.PostForm("parent_id")),
	}

	c.HTML(http.StatusOK, MenuEditFormTemplate, adminui.PrepareBareView(c, menuEditData(c, h, row, echo)))
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
		adminMenusPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.menus.MenuCreate(c.Request.Context(), &admindto.MenuCreateReq{
		Title: title, ParentID: reqparams.ParseUint(c.PostForm("parent_id")),
		Type: reqparams.ParseStatus(c.PostForm("type")), Path: reqparams.FieldValue(c, "path"),
		Status: reqparams.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: reqparams.FieldValue(c, "remark"),
		PermissionCodes: menuPermissionCodes(c),
	}); err != nil {
		adminMenusPage.writeFail(c, err)
		return
	}
	adminMenusPage.done(c)
}

func (h *AdminPagesHandle) MenusUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	title := reqparams.FieldValue(c, "title")
	if id == 0 || title == "" {
		if adminDrawerHX(c) {
			h.menuEditFail(c, adminBadRequestMsg)
			return
		}
		adminMenusPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))

	row, err := h.menus.MenuDetail(c.Request.Context(), &admindto.MenuDetailReq{ID: id})
	if err != nil || row == nil || row.ID != id {
		if adminDrawerHX(c) {
			h.menuEditFail(c, adminErrMsg(c, adminenums.Err(adminenums.ErrMenuNotFound)))
			return
		}
		adminMenusPage.writeFail(c, adminenums.Err(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuUpdate(c.Request.Context(), &admindto.MenuUpdateReq{
		ID: id, Title: title, ParentID: reqparams.ParseUint(c.PostForm("parent_id")),
		Type: reqparams.ParseStatus(c.PostForm("type")), Path: reqparams.FieldValue(c, "path"),
		Status: reqparams.ParseStatus(c.PostForm("status")), SortOrder: sortOrder, Remark: reqparams.FieldValue(c, "remark"),
		PermissionCodes: menuPermissionCodes(c),

		Icon: reqparams.FieldValue(c, "icon"),

		TitleKey: row.TitleKey, ExternalURL: row.ExternalURL,
		IsHidden: row.IsHidden, IsPublic: row.IsPublic,
	}); err != nil {
		if adminDrawerHX(c) {
			h.menuEditFail(c, adminErrMsg(c, err))
			return
		}
		adminMenusPage.writeFail(c, err)
		return
	}
	adminMenusPage.done(c)
}

func (h *AdminPagesHandle) MenusDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminMenusPage.paramFail(c)
		return
	}
	nodes, err := h.menus.MenuTree(c.Request.Context(), &admindto.MenuTreeReq{})
	if err != nil {
		adminMenusPage.writeFail(c, err)
		return
	}
	if !adminMenuTreeHas(nodes, id) {
		adminMenusPage.writeFail(c, adminenums.Err(adminenums.ErrMenuNotFound))
		return
	}
	if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
		adminMenusPage.writeFail(c, err)
		return
	}
	adminMenusPage.done(c)
}

func (h *AdminPagesHandle) MenusBulkDelete(c *gin.Context) {

	ids, berr := shell.BulkIDs(c)
	if berr != nil {

		adminMenusPage.fail(c, adminBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.menus.MenuDelete(c.Request.Context(), &admindto.MenuDeleteReq{IDs: []uint64{id}}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := adminBulkOutcome(c, adminBulkNounMenu, deleted, skipped)
	adminMenusPage.jump(c, ok, msg)
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
		res = &admindto.PermListResp{}
	}
	view := &admindto.PermListPageResp{
		Title:        pagesMsgPermissionsTitle,
		Menu:         "permissions",
		Rows:         res.List,
		Total:        res.Total,
		FilterCode:   code,
		FilterModule: module,
		Err:          adminListLoadErr(c, err),
		ListQuery:    adminPermsPage.listQuery(c),
	}
	if !adminui.AttachPaginationView(view, "/admin/permissions",
		map[string]string{"code": code, "module": module},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("权限点列表视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

func (h *AdminPagesHandle) PermissionsEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := reqparams.ParseUint(c.Query("id"))
	if id == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	row, err := h.perms.PermDetail(c.Request.Context(), &admindto.PermDetailReq{ID: id})
	if err != nil {
		adminErrMsg(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	if row == nil || row.ID != id {
		c.Status(http.StatusNotFound)
		return
	}
	pageview.Render(c, &admindto.PermissionEditFormResp{PermissionEdit: row})
}

func (h *AdminPagesHandle) permissionEditFail(c *gin.Context, msg adminui.Msg) {
	if !adminDrawerHX(c) {

		adminPermsPage.fail(c, msg)
		return
	}
	id := reqparams.ParseUint(c.PostForm("id"))
	row, err := h.perms.PermDetail(c.Request.Context(), &admindto.PermDetailReq{ID: id})
	if id == 0 || err != nil || row == nil || row.ID != id {

		adminPermsPage.fail(c, msg)
		return
	}
	data := &admindto.PermissionEditFailResp{PermissionEdit: row, PermissionEditErr: adminui.MsgText(c, msg), PermissionEditEcho: map[string]string{
		"permission_name": c.PostForm("permission_name"), "module": c.PostForm("module"),
		"api_path": c.PostForm("api_path"), "api_method": c.PostForm("api_method"),
		"status": c.PostForm("status"), "remark": c.PostForm("remark"),
	}}

	c.HTML(http.StatusOK, PermissionEditFormTemplate, adminui.PrepareBareView(c, data))
}

func (h *AdminPagesHandle) PermissionsCreate(c *gin.Context) {
	code := reqparams.FieldValue(c, "permission_code")
	name := reqparams.FieldValue(c, "permission_name")
	module := reqparams.FieldValue(c, "module")
	apiPath := reqparams.FieldValue(c, "api_path")
	apiMethod := strings.ToUpper(reqparams.FieldValue(c, "api_method"))
	if code == "" || name == "" || module == "" || apiPath == "" {
		adminPermsPage.paramFail(c)
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
		adminPermsPage.writeFail(c, err)
		return
	}
	adminPermsPage.done(c)
}

func (h *AdminPagesHandle) PermissionsUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	name := reqparams.FieldValue(c, "permission_name")
	module := reqparams.FieldValue(c, "module")
	apiPath := reqparams.FieldValue(c, "api_path")
	apiMethod := strings.ToUpper(reqparams.FieldValue(c, "api_method"))
	if id == 0 || name == "" || module == "" || apiPath == "" {
		if adminDrawerHX(c) {
			h.permissionEditFail(c, adminBadRequestMsg)
			return
		}
		adminPermsPage.paramFail(c)
		return
	}
	if apiMethod != "GET" && apiMethod != "POST" {
		apiMethod = "POST"
	}
	if _, err := h.perms.PermUpdate(c.Request.Context(), &admindto.PermUpdateReq{
		ID: id, PermissionCode: reqparams.FieldValue(c, "permission_code"),
		PermissionName: name, Module: module, APIPath: apiPath, APIMethod: apiMethod,
		Status: reqparams.ParseStatus(c.PostForm("status")), Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		if adminDrawerHX(c) {
			h.permissionEditFail(c, adminErrMsg(c, err))
			return
		}
		adminPermsPage.writeFail(c, err)
		return
	}
	adminPermsPage.done(c)
}

func (h *AdminPagesHandle) PermissionsDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminPermsPage.paramFail(c)
		return
	}
	if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
		adminPermsPage.writeFail(c, err)
		return
	}
	adminPermsPage.done(c)
}

func (h *AdminPagesHandle) PermissionsBulkDelete(c *gin.Context) {

	ids, berr := shell.BulkIDs(c)
	if berr != nil {

		adminPermsPage.fail(c, adminBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			continue
		}
		if _, err := h.perms.PermDelete(c.Request.Context(), &admindto.PermDeleteReq{IDs: []uint64{id}}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := adminBulkOutcome(c, adminBulkNounPermission, deleted, skipped)
	adminPermsPage.jump(c, ok, msg)
}

func (h *AdminPagesHandle) RolesPage(c *gin.Context) {
	page, limit := reqparams.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	res, err := h.roles.RoleList(c.Request.Context(), &admindto.RoleListReq{
		RolePageReq: admindto.RolePageReq{Page: page, Limit: limit},
		Keyword:     keyword,
	})
	if err != nil {
		res = &admindto.RoleListResp{}
	}
	view := &admindto.RoleListPageResp{
		Title:         pagesMsgRolesTitle,
		Menu:          "roles",
		Rows:          res.List,
		Total:         res.Total,
		FilterKeyword: keyword,
		Err:           adminListLoadErr(c, err),
		ListQuery:     adminRolesPage.listQuery(c),
	}
	if !adminui.AttachPaginationView(view, "/admin/roles",
		map[string]string{"keyword": keyword},
		pagination.Page{Total: res.Total, Page: page, Limit: limit}) {
		logger.Scene("admin").Error(errors.New("角色视图缺少 Pagination 字段"), "分页事实挂载失败")
	}
	pageview.Render(c, view)
}

type permTreeRow struct {
	ID        uint64
	Title     string
	Type      int
	TypeLabel string
	Depth     int

	PadLeft  int
	ParentID uint64

	PermissionCodes []string
	Status          int
	Checked         bool
	HasChildren     bool
}

const permRowIndentStep = 20

func flattenPermissionTree(tr adminLabelTranslate, nodes []admindto.MenuTreeNode, depth int, checked map[uint64]struct{}, out *[]permTreeRow) {
	for _, n := range nodes {
		_, ok := checked[n.ID]
		*out = append(*out, permTreeRow{
			ID: n.ID, Title: n.Title, Type: n.Type, TypeLabel: adminMenuTypeLabel(tr, n.Type),
			Depth: depth, PadLeft: (depth-1)*permRowIndentStep + 10,
			ParentID: n.ParentID, PermissionCodes: n.PermissionCodes,
			Status: n.Status, Checked: ok, HasChildren: len(n.Children) > 0,
		})
		if len(n.Children) > 0 {
			flattenPermissionTree(tr, n.Children, depth+1, checked, out)
		}
	}
}

func (h *AdminPagesHandle) rolePermissionsDrawerData(c *gin.Context, roleID uint64) (*admindto.RolePermissionsResp, error) {
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
	flattenPermissionTree(adminLabelTranslator(c), tree.Tree, 1, checked, &rows)
	return &admindto.RolePermissionsResp{Role: tree, Rows: rows}, nil
}

func (h *AdminPagesHandle) RolePermissionsDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	roleID := reqparams.ParseUint(c.Query("role_id"))
	if roleID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	data, err := h.rolePermissionsDrawerData(c, roleID)
	if err != nil {

		adminErrMsg(c, err)
		c.Status(http.StatusNotFound)
		return
	}
	pageview.Render(c, data)
}

func (h *AdminPagesHandle) RolesCreate(c *gin.Context) {
	roleCode := reqparams.FieldValue(c, "role_code")
	roleName := reqparams.FieldValue(c, "role_name")
	if roleCode == "" || roleName == "" {
		adminRolesPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.roles.RoleCreate(c.Request.Context(), &admindto.RoleCreateReq{
		RoleCode: roleCode, RoleName: roleName, Status: reqparams.ParseStatusPtr(c.PostForm("status")),
		SortOrder: sortOrder, Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminRolesPage.writeFail(c, err)
		return
	}
	adminRolesPage.done(c)
}

func (h *AdminPagesHandle) RolesUpdate(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	roleName := reqparams.FieldValue(c, "role_name")
	if id == 0 || roleName == "" {
		adminRolesPage.paramFail(c)
		return
	}
	sortOrder, _ := strconv.Atoi(reqparams.FieldValue(c, "sort_order"))
	if err := h.roles.RoleUpdate(c.Request.Context(), &admindto.RoleUpdateReq{
		ID: id, RoleName: roleName, Status: reqparams.ParseStatus(c.PostForm("status")),
		SortOrder: sortOrder, Remark: reqparams.FieldValue(c, "remark"),
	}); err != nil {
		adminRolesPage.writeFail(c, err)
		return
	}
	adminRolesPage.done(c)
}

func (h *AdminPagesHandle) RolesDelete(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	if id == 0 {
		adminRolesPage.paramFail(c)
		return
	}
	if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
		adminRolesPage.writeFail(c, err)
		return
	}
	adminRolesPage.done(c)
}

func (h *AdminPagesHandle) RolesBulkDelete(c *gin.Context) {

	ids, berr := shell.BulkIDs(c)
	if berr != nil {

		adminRolesPage.fail(c, adminBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
		if id == 0 {
			continue
		}
		if err := h.roles.RoleDelete(c.Request.Context(), &admindto.RoleDeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := adminBulkOutcome(c, adminBulkNounRole, deleted, skipped)
	adminRolesPage.jump(c, ok, msg)
}

const (
	dataruleEditorMarker = "config_editor"

	dataruleMaxGroups       = 8
	dataruleMaxRowsPerGroup = 12
)

type dataruleOption struct {
	Value    string
	Label    string
	Selected bool
}

type dataruleFieldView struct {
	Field   string
	Label   string
	Omitted bool
}

type dataruleRowView struct {
	FieldOptions []dataruleOption
	OpOptions    []dataruleOption
	Value        string
}

type dataruleGroupView struct {
	Index     int
	Logic     string
	Rows      []dataruleRowView
	CanAddRow bool
}

type dataruleEditorCtx struct {
	Domain      string
	FieldCount  int
	Fields      []dataruleFieldView
	Groups      []dataruleGroupView
	CanAddGroup bool
}

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

func (h *AdminPagesHandle) dataruleEditorContext(c *gin.Context, domain string, cfg admindto.RuleConfigDTO) dataruleEditorCtx {

	ctx := dataruleEditorCtx{
		Domain:     domain,
		Fields:     []dataruleFieldView{},
		Groups:     []dataruleGroupView{},
		FieldCount: 0,
	}

	detail, _ := h.rules.RuleSchemaDetail(c.Request.Context(), &admindto.RuleSchemaDetailReq{Domain: domain})
	labels := map[string]string{}
	opsByField := map[string][]string{}
	order := make([]string, 0, 8)
	if detail != nil {
		for _, f := range detail.Fields {
			order = append(order, f.Field)
			labels[f.Field] = f.Label
			opsByField[f.Field] = f.Operators
		}
	}
	ctx.FieldCount = len(order)

	omitted := map[string]bool{}
	for _, f := range cfg.OmitFields {
		omitted[f] = true
	}
	for _, field := range order {
		ctx.Fields = append(ctx.Fields, dataruleFieldView{Field: field, Label: labels[field], Omitted: omitted[field]})
	}

	fieldOptions := func(selected string) []dataruleOption {
		options := make([]dataruleOption, 0, len(order)+1)
		options = append(options, dataruleOption{Value: "", Label: adminui.MsgText(c, adminui.Msg{Key: adminenums.DatarulesEditorFieldPlaceholder, Fallback: "请选择字段"}), Selected: selected == ""})
		for _, field := range order {
			options = append(options, dataruleOption{
				Value:    field,
				Label:    labels[field] + "（" + field + "）",
				Selected: field == selected,
			})
		}
		return options
	}
	opOptions := func(field, selected string) []dataruleOption {
		ops := opsByField[field]
		if len(ops) == 0 {
			return []dataruleOption{{Value: "", Label: adminui.MsgText(c, adminui.Msg{Key: adminenums.DatarulesEditorFieldRequired, Fallback: "请先选择字段"}), Selected: true}}
		}
		options := make([]dataruleOption, 0, len(ops))
		for _, op := range ops {
			options = append(options, dataruleOption{Value: op, Label: op, Selected: op == selected})
		}
		return options
	}

	groups := cfg.ConditionGroups
	if len(groups) > dataruleMaxGroups {
		groups = groups[:dataruleMaxGroups]
	}
	for gi, group := range groups {
		logic := datarule.LogicAnd
		if strings.EqualFold(strings.TrimSpace(group.Logic), datarule.LogicOr) {
			logic = datarule.LogicOr
		}
		view := dataruleGroupView{Index: gi, Logic: logic, CanAddRow: len(group.Conditions) < dataruleMaxRowsPerGroup}
		for _, row := range group.Conditions {
			view.Rows = append(view.Rows, dataruleRowView{
				FieldOptions: fieldOptions(row.Field),
				OpOptions:    opOptions(row.Field, row.Op),
				Value:        row.Value,
			})
		}
		ctx.Groups = append(ctx.Groups, view)
	}
	if len(ctx.Groups) == 0 {

		ctx.Groups = append(ctx.Groups, dataruleGroupView{
			Index:     0,
			Logic:     datarule.LogicAnd,
			Rows:      []dataruleRowView{{FieldOptions: fieldOptions(""), OpOptions: opOptions("", "")}},
			CanAddRow: true,
		})
	}
	ctx.CanAddGroup = len(ctx.Groups) < dataruleMaxGroups
	return ctx
}

func (h *AdminPagesHandle) DataruleConfigEditor(c *gin.Context) {
	domain := reqparams.FieldValue(c, "domain")
	cfg := dataruleConfigFromForm(c)

	switch reqparams.FieldValue(c, "action") {
	case "add_group":
		if len(cfg.ConditionGroups) < dataruleMaxGroups {
			cfg.ConditionGroups = append(cfg.ConditionGroups, admindto.RuleConditionGroupDTO{Logic: datarule.LogicAnd})
		}
	case "del_group":
		gi := int(reqparams.ParseUint(c.PostForm("group")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) {
			cfg.ConditionGroups = append(cfg.ConditionGroups[:gi], cfg.ConditionGroups[gi+1:]...)
		}
	case "add_row":
		gi := int(reqparams.ParseUint(c.PostForm("group")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) && len(cfg.ConditionGroups[gi].Conditions) < dataruleMaxRowsPerGroup {
			cfg.ConditionGroups[gi].Conditions = append(cfg.ConditionGroups[gi].Conditions, admindto.RuleConditionDTO{})
		}
	case "del_row":
		gi := int(reqparams.ParseUint(c.PostForm("group")))
		ri := int(reqparams.ParseUint(c.PostForm("row")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) && ri >= 0 && ri < len(cfg.ConditionGroups[gi].Conditions) {
			rows := cfg.ConditionGroups[gi].Conditions
			cfg.ConditionGroups[gi].Conditions = append(rows[:ri], rows[ri+1:]...)
		}
	}

	pageview.Render(c, &admindto.DataruleEditorResp{Editor: h.dataruleEditorContext(c, domain, cfg)})
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

		c.String(http.StatusForbidden, adminui.MsgText(c, adminui.Msg{Key: adminenums.DevLoginLoopbackOnly, Fallback: "开发登录仅限本机访问"}))
		return
	}

	res, err := h.admin.DevLogin(c.Request.Context(), strings.TrimSpace(c.Query("user")))
	if err != nil {
		logger.Scene("admin").Error(err, "开发登录失败")
		c.String(http.StatusInternalServerError, adminui.MsgText(c, adminui.Msg{Key: adminenums.DevLoginFailed, Fallback: "开发登录失败，详情见服务端日志"}))
		return
	}

	if err := auth.SaveCookieSession(c, &auth.CookieSession{
		SessionID: res.SessionID,
	}, false); err != nil {
		logger.Scene("admin").Error(err, "开发登录写会话失败")
		c.String(http.StatusInternalServerError, adminui.MsgText(c, adminui.Msg{Key: adminenums.DevLoginSessionFailed, Fallback: "写会话失败，详情见服务端日志"}))
		return
	}
	if _, err := builtin.RotateCSRFToken(c); err != nil {
		logger.Scene("admin").Error(err, "开发登录轮换 CSRF token 失败")
		c.String(http.StatusInternalServerError, adminui.MsgText(c, adminui.Msg{Key: adminenums.DevLoginCSRFFailed, Fallback: "轮换 CSRF token 失败，详情见服务端日志"}))
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
