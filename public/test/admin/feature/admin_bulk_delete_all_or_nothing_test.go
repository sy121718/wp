package feature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	json "go_wp/pkg/jsonx"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/casbin"
	"go_wp/pkg/facing"
	"go_wp/public/test/support"
)

// bulkProbe 是批量删除用例的探针：直接调 service，用真实 PG 读回行数。
type bulkProbe struct {
	t   *testing.T
	svc *adminservice.Service
	db  *gorm.DB
	ctx context.Context
}

func newBulkProbe(t *testing.T) *bulkProbe {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	return &bulkProbe{t: t, svc: adminservice.NewService(db), db: db, ctx: context.Background()}
}

func (p *bulkProbe) create(v any) {
	p.t.Helper()
	if err := p.db.WithContext(p.ctx).Create(v).Error; err != nil {
		p.t.Fatalf("写入测试数据失败: %v", err)
	}
}

func (p *bulkProbe) count(table, where string, args ...any) int64 {
	p.t.Helper()
	var n int64
	q := p.db.WithContext(p.ctx).Table(table)
	if where != "" {
		q = q.Where(where, args...)
	}
	if err := q.Count(&n).Error; err != nil {
		p.t.Fatalf("数 %s 行数失败: %v", table, err)
	}
	return n
}

// requireBlockedOne 断言单一原因的拒绝：状态码精确、明细精确、明细带可定位 id。
func requireBlockedOne(t *testing.T, err error, wantReason string, wantStatus int) {
	t.Helper()
	if err == nil {
		t.Fatal("应被拒绝，实际成功")
	}
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("拒绝必须带明细（出口转 data.items），实际错误不含 BlockedError：%v", err)
	}
	items := blocked.BlockedItems()
	if len(items) != 1 {
		t.Fatalf("明细条数 = %d，want 1（全部 %+v）", len(items), items)
	}
	if items[0].ID == "" {
		t.Fatalf("明细项缺少可定位 id：%+v", items[0])
	}
	if items[0].ReasonKey != wantReason {
		t.Fatalf("明细原因 = %q，want %q", items[0].ReasonKey, wantReason)
	}
	if got, ok := facing.StatusOf(err); !ok || got != wantStatus {
		t.Fatalf("状态码 = %d（声明 %v），want %v", got, ok, wantStatus)
	}
}

func TestAdminBulkDeleteAllOrNothingAcrossEntities(t *testing.T) {
	p := newBulkProbe(t)
	const missing = uint64(99999999)

	t.Run("角色：全成 / 混不存在整批回滚 / 内置打回", func(t *testing.T) {
		roleOK := &adminmodel.RoleEntity{RoleCode: "bk_role_ok", RoleName: "可删角色", IsSystem: 0}
		roleKeep := &adminmodel.RoleEntity{RoleCode: "bk_role_keep", RoleName: "保留角色", IsSystem: 0}
		roleSys := &adminmodel.RoleEntity{RoleCode: "bk_role_sys", RoleName: "内置角色", IsSystem: 1}
		p.create(roleOK)
		p.create(roleKeep)
		p.create(roleSys)

		if err := p.svc.RoleDelete(p.ctx, &admindto.RoleDeleteReq{IDs: []uint64{roleOK.ID}}); err != nil {
			t.Fatalf("全成路径删除失败: %v", err)
		}
		if left := p.count("sys_role", "id = ?", roleOK.ID); left != 0 {
			t.Fatalf("删除后仍有 %d 行", left)
		}

		err := p.svc.RoleDelete(p.ctx, &admindto.RoleDeleteReq{IDs: []uint64{roleKeep.ID, missing}})
		requireBlockedOne(t, err, adminenums.ErrRoleNotFound, http.StatusNotFound)
		if left := p.count("sys_role", "id = ?", roleKeep.ID); left != 1 {
			t.Fatalf("整批回滚后合法角色应仍在，实际 %d 行", left)
		}

		err = p.svc.RoleDelete(p.ctx, &admindto.RoleDeleteReq{IDs: []uint64{roleSys.ID}})
		requireBlockedOne(t, err, adminenums.ErrRoleIsSystem, http.StatusConflict)
		if left := p.count("sys_role", "id = ?", roleSys.ID); left != 1 {
			t.Fatalf("内置角色应保留，实际 %d 行", left)
		}
	})

	t.Run("部门：全成 / 混不存在整批回滚 / 有子部门打回", func(t *testing.T) {
		deptOK := &adminmodel.DeptEntity{DeptName: "可删部门", DeptCode: "bk_dept_ok", Status: 1}
		deptKeep := &adminmodel.DeptEntity{DeptName: "保留部门", DeptCode: "bk_dept_keep", Status: 1}
		p.create(deptOK)
		p.create(deptKeep)
		child := &adminmodel.DeptEntity{DeptName: "子部门", DeptCode: "bk_dept_child", ParentID: deptKeep.ID, Status: 1}
		p.create(child)

		if err := p.svc.DeptDelete(p.ctx, &admindto.DeptDeleteReq{IDs: []uint64{deptOK.ID}}); err != nil {
			t.Fatalf("全成路径删除失败: %v", err)
		}
		if left := p.count("sys_dept", "id = ?", deptOK.ID); left != 0 {
			t.Fatalf("删除后仍有 %d 行", left)
		}

		err := p.svc.DeptDelete(p.ctx, &admindto.DeptDeleteReq{IDs: []uint64{child.ID, missing}})
		requireBlockedOne(t, err, adminenums.ErrDeptNotFound, http.StatusNotFound)
		if left := p.count("sys_dept", "id = ?", child.ID); left != 1 {
			t.Fatalf("整批回滚后合法部门应仍在，实际 %d 行", left)
		}

		err = p.svc.DeptDelete(p.ctx, &admindto.DeptDeleteReq{IDs: []uint64{deptKeep.ID}})
		requireBlockedOne(t, err, adminenums.ErrDeptHasChildren, http.StatusConflict)
		if left := p.count("sys_dept", "id = ?", deptKeep.ID); left != 1 {
			t.Fatalf("有子部门的部门应保留，实际 %d 行", left)
		}
	})

	t.Run("菜单：全成 / 混不存在整批回滚 / 系统菜单打回", func(t *testing.T) {
		menuOK := &adminmodel.MenuEntity{Title: "可删菜单", Type: 2, Status: 1}
		menuKeep := &adminmodel.MenuEntity{Title: "保留菜单", Type: 2, Status: 1}
		menuSys := &adminmodel.MenuEntity{Title: "系统菜单", Type: 2, Status: 1, IsSystem: 1}
		p.create(menuOK)
		p.create(menuKeep)
		p.create(menuSys)

		if err := p.svc.MenuDelete(p.ctx, &admindto.MenuDeleteReq{IDs: []uint64{menuOK.ID}}); err != nil {
			t.Fatalf("全成路径删除失败: %v", err)
		}
		if left := p.count("sys_menus", "id = ? AND deleted_at IS NULL", menuOK.ID); left != 0 {
			t.Fatalf("删除后仍有 %d 行（软删未生效）", left)
		}

		err := p.svc.MenuDelete(p.ctx, &admindto.MenuDeleteReq{IDs: []uint64{menuKeep.ID, missing}})
		requireBlockedOne(t, err, adminenums.ErrMenuNotFound, http.StatusNotFound)
		if left := p.count("sys_menus", "id = ? AND deleted_at IS NULL", menuKeep.ID); left != 1 {
			t.Fatalf("整批回滚后合法菜单应仍在，实际 %d 行", left)
		}

		err = p.svc.MenuDelete(p.ctx, &admindto.MenuDeleteReq{IDs: []uint64{menuSys.ID}})
		requireBlockedOne(t, err, adminenums.ErrMenuIsSystem, http.StatusConflict)
		if left := p.count("sys_menus", "id = ? AND deleted_at IS NULL", menuSys.ID); left != 1 {
			t.Fatalf("系统菜单应保留，实际 %d 行", left)
		}
	})

	t.Run("权限点：全成 / 混不存在整批回滚 / 被角色绑定打回 / 被菜单引用打回", func(t *testing.T) {
		permOK := &adminmodel.PermissionEntity{
			PermissionCode: "bk.perm.ok", PermissionName: "可删权限", Module: "admin",
			APIPath: "/bk/ok", APIMethod: "GET", Status: 1,
		}
		permKeep := &adminmodel.PermissionEntity{
			PermissionCode: "bk.perm.keep", PermissionName: "保留权限", Module: "admin",
			APIPath: "/bk/keep", APIMethod: "GET", Status: 1,
		}
		p.create(permOK)
		p.create(permKeep)

		if _, err := p.svc.PermDelete(p.ctx, &admindto.PermDeleteReq{IDs: []uint64{permOK.ID}}); err != nil {
			t.Fatalf("全成路径删除失败: %v", err)
		}
		if left := p.count("sys_permission", "id = ?", permOK.ID); left != 0 {
			t.Fatalf("删除后仍有 %d 行", left)
		}

		_, err := p.svc.PermDelete(p.ctx, &admindto.PermDeleteReq{IDs: []uint64{permKeep.ID, missing}})
		requireBlockedOne(t, err, adminenums.ErrPermissionNotFound, http.StatusNotFound)
		if left := p.count("sys_permission", "id = ?", permKeep.ID); left != 1 {
			t.Fatalf("整批回滚后合法权限应仍在，实际 %d 行", left)
		}

		// 被角色绑定：写一条真实的 p 策略指向这个权限码，删除必须 409。
		permAssigned := &adminmodel.PermissionEntity{
			PermissionCode: "bk.perm.assigned", PermissionName: "被绑定权限", Module: "admin",
			APIPath: "/bk/assigned", APIMethod: "POST", Status: 1,
		}
		p.create(permAssigned)
		p.create(&adminmodel.RoleEntity{RoleCode: "bk_role_holder", RoleName: "持有角色", IsSystem: 0})
		p.create(&casbin.CasbinRule{
			Ptype: "p", V0: "bk_role_holder",
			V1: permAssigned.APIPath, V2: permAssigned.APIMethod, V3: permAssigned.PermissionCode,
		})
		_, err = p.svc.PermDelete(p.ctx, &admindto.PermDeleteReq{IDs: []uint64{permAssigned.ID}})
		requireBlockedOne(t, err, adminenums.ErrPermissionAssigned, http.StatusConflict)
		if left := p.count("sys_permission", "id = ?", permAssigned.ID); left != 1 {
			t.Fatalf("被角色绑定的权限应保留，实际 %d 行", left)
		}

		// 被菜单引用：写一行 sys_menu_permission，删除必须 409。
		permInMenu := &adminmodel.PermissionEntity{
			PermissionCode: "bk.perm.inmenu", PermissionName: "菜单引用权限", Module: "admin",
			APIPath: "/bk/inmenu", APIMethod: "GET", Status: 1,
		}
		p.create(permInMenu)
		menuHolder := &adminmodel.MenuEntity{Title: "引用菜单", Type: 2, Status: 1}
		p.create(menuHolder)
		if err := p.db.WithContext(p.ctx).Table("sys_menu_permission").Create(map[string]any{
			"menu_id": menuHolder.ID, "permission_code": permInMenu.PermissionCode,
		}).Error; err != nil {
			t.Fatalf("写菜单权限绑定失败: %v", err)
		}
		_, err = p.svc.PermDelete(p.ctx, &admindto.PermDeleteReq{IDs: []uint64{permInMenu.ID}})
		requireBlockedOne(t, err, adminenums.ErrMenuReferenced, http.StatusConflict)
		if left := p.count("sys_permission", "id = ?", permInMenu.ID); left != 1 {
			t.Fatalf("被菜单引用的权限应保留，实际 %d 行", left)
		}
	})

	t.Run("数据规则：全成 / 混不存在整批回滚", func(t *testing.T) {
		ruleOK := &adminmodel.SysRuleEntity{RuleName: "可删规则", Domain: "bk", Config: "[]", Status: 1}
		ruleKeep := &adminmodel.SysRuleEntity{RuleName: "保留规则", Domain: "bk", Config: "[]", Status: 1}
		p.create(ruleOK)
		p.create(ruleKeep)

		if err := p.svc.RuleDelete(p.ctx, &admindto.RuleDeleteReq{IDs: []uint64{ruleOK.ID}}); err != nil {
			t.Fatalf("全成路径删除失败: %v", err)
		}
		if left := p.count("sys_rule", "id = ?", ruleOK.ID); left != 0 {
			t.Fatalf("删除后仍有 %d 行", left)
		}

		err := p.svc.RuleDelete(p.ctx, &admindto.RuleDeleteReq{IDs: []uint64{ruleKeep.ID, missing}})
		requireBlockedOne(t, err, adminenums.ErrRuleNotFound, http.StatusNotFound)
		if left := p.count("sys_rule", "id = ?", ruleKeep.ID); left != 1 {
			t.Fatalf("整批回滚后合法规则应仍在，实际 %d 行", left)
		}
	})

	t.Run("管理员：全成 / 混不存在整批回滚 / 删自己打回", func(t *testing.T) {
		op := &adminmodel.AdminEntity{Username: "bk_op", Password: "x", Status: adminmodel.AdminStatusActive}
		target := &adminmodel.AdminEntity{Username: "bk_target", Password: "x", Status: adminmodel.AdminStatusActive}
		keep := &adminmodel.AdminEntity{Username: "bk_keep", Password: "x", Status: adminmodel.AdminStatusActive}
		p.create(op)
		p.create(target)
		p.create(keep)

		if _, err := p.svc.AdminDelete(p.ctx, &admindto.AdminDeleteReq{
			Id: []uint64{target.ID}, OperatorID: op.ID,
		}); err != nil {
			t.Fatalf("全成路径删除失败: %v", err)
		}
		if left := p.count("sys_admin", "id = ?", target.ID); left != 0 {
			t.Fatalf("删除后仍有 %d 行", left)
		}

		_, err := p.svc.AdminDelete(p.ctx, &admindto.AdminDeleteReq{
			Id: []uint64{keep.ID, missing}, OperatorID: op.ID,
		})
		requireBlockedOne(t, err, adminenums.ErrAdminNotFound, http.StatusNotFound)
		if left := p.count("sys_admin", "id = ?", keep.ID); left != 1 {
			t.Fatalf("整批回滚后合法管理员应仍在，实际 %d 行", left)
		}

		_, err = p.svc.AdminDelete(p.ctx, &admindto.AdminDeleteReq{
			Id: []uint64{op.ID}, OperatorID: op.ID,
		})
		requireBlockedOne(t, err, adminenums.ErrDeleteSelf, http.StatusConflict)
		if left := p.count("sys_admin", "id = ?", op.ID); left != 1 {
			t.Fatalf("操作人自己应保留，实际 %d 行", left)
		}
	})
}

func TestAdminBulkDeleteIllegalIDsReachItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	svc := adminservice.NewService(db)
	h := adminhttp.NewAdminPagesHandle(nil, svc, nil, nil, nil, nil)

	engine := gin.New()
	engine.POST("/admin/roles/bulk-delete", h.RolesBulkDelete)

	form := url.Values{}
	form.Add("ids", "abc")
	form.Add("ids", "0")
	form.Add("ids", "12345")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/roles/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 id 应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Items []struct {
				ID        string `json:"id"`
				ReasonKey string `json:"reasonKey"`
				Message   string `json:"message"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 JSON：%v body=%s", err, rec.Body.String())
	}
	if payload.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400", payload.Code)
	}
	if payload.Message == "" || payload.Message == adminenums.ErrBulkDeleteInvalidID {
		t.Fatalf("message 必须取过词，got %q", payload.Message)
	}
	got := map[string]string{}
	for _, item := range payload.Data.Items {
		if item.ReasonKey != adminenums.ErrBulkDeleteInvalidID {
			t.Fatalf("明细原因 = %q，want %q", item.ReasonKey, adminenums.ErrBulkDeleteInvalidID)
		}
		if item.Message == "" || item.Message == item.ReasonKey {
			t.Fatalf("明细 message 必须取过词，got %q", item.Message)
		}
		got[item.ID] = item.ReasonKey
	}
	for _, raw := range []string{"abc", "0"} {
		if _, ok := got[raw]; !ok {
			t.Fatalf("data.items 应逐条带上原始非法值 %q，实际 %+v", raw, payload.Data.Items)
		}
	}
	if _, ok := got["12345"]; ok {
		t.Fatalf("合法但不存在的主键不该出现在非法明细里：%+v", payload.Data.Items)
	}
}
