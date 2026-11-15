package unit

import (
	"context"
	"reflect"
	"testing"
	"time"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	"go_wp/pkg/utils"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

func newCustomerFixture(t *testing.T) (*usermodel.UserModel, *userservice.Service) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, nil
	}
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	m := usermodel.NewUserModel(db)

	svc := userservice.NewService(m, nil, nil, nil, "测试站点")
	return m, svc
}

func mkCustomer(t *testing.T, m *usermodel.UserModel, username, email, display string,
	status int, registeredAt time.Time) *usermodel.UserEntity {
	t.Helper()
	ctx := context.Background()
	e := &usermodel.UserEntity{Username: username, Email: email, Status: status, RegisteredAt: &registeredAt}
	if display != "" {
		e.DisplayName = &display
	}
	if err := m.Create(ctx, e); err != nil {
		t.Fatalf("建客户 %s 失败: %v", username, err)
	}
	return e
}

func day(y int, mo time.Month, d int) time.Time {
	return time.Date(y, mo, d, 10, 0, 0, 0, time.Local)
}

func TestCustomerListMatchesDisplayName(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	mkCustomer(t, m, "alice", "alice@example.com", "艾丽丝的店", usermodel.UserStatusActive, day(2026, 9, 1))
	mkCustomer(t, m, "bob", "bob@example.com", "鲍勃", usermodel.UserStatusActive, day(2026, 9, 2))

	res, err := svc.ListCustomers(ctx, &userdto.CustomerListReq{Keyword: "艾丽丝", Status: userdto.CustomerStatusAll})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if res.Total != 1 || len(res.List) != 1 || res.List[0].Username != "alice" {
		t.Fatalf("按展示名搜索应命中 1 个客户，实得 total=%d list=%d", res.Total, len(res.List))
	}

	if res.List[0].DisplayName != "艾丽丝的店" {
		t.Errorf("展示名没有返回：%q", res.List[0].DisplayName)
	}
}

func TestCustomerListFiltersEmailVerifiedAndRegisterRange(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	verified := mkCustomer(t, m, "v1", "v1@example.com", "", usermodel.UserStatusActive, day(2026, 9, 10))
	mkCustomer(t, m, "u1", "u1@example.com", "", usermodel.UserStatusPending, day(2026, 10, 5))
	if err := m.UpdateFields(ctx, verified.ID, map[string]any{"email_verified_at": day(2026, 9, 11)}); err != nil {
		t.Fatalf("标记邮箱已验证失败: %v", err)
	}

	only, err := svc.ListCustomers(ctx, &userdto.CustomerListReq{
		Status: userdto.CustomerStatusAll, EmailVerified: userdto.EmailVerifiedYes})
	if err != nil {
		t.Fatalf("已验证筛选失败: %v", err)
	}
	if only.Total != 1 || only.List[0].Username != "v1" {
		t.Fatalf("「已验证」应只命中 v1，实得 total=%d", only.Total)
	}
	if !only.List[0].EmailVerified {
		t.Errorf("已验证标记没有反映到响应上")
	}

	none, err := svc.ListCustomers(ctx, &userdto.CustomerListReq{
		Status: userdto.CustomerStatusAll, EmailVerified: userdto.EmailVerifiedNo})
	if err != nil {
		t.Fatalf("未验证筛选失败: %v", err)
	}
	if none.Total != 1 || none.List[0].Username != "u1" {
		t.Fatalf("「未验证」应只命中 u1，实得 total=%d", none.Total)
	}

	from, to := day(2026, 9, 1), day(2026, 9, 30)
	ranged, err := svc.ListCustomers(ctx, &userdto.CustomerListReq{
		Status: userdto.CustomerStatusAll, RegisteredFrom: utils.NewJSONTimePtr(&from), RegisteredTo: utils.NewJSONTimePtr(&to)})
	if err != nil {
		t.Fatalf("注册时间范围筛选失败: %v", err)
	}
	if ranged.Total != 1 || ranged.List[0].Username != "v1" {
		t.Fatalf("9 月注册的应只有 v1，实得 total=%d", ranged.Total)
	}
}

func TestCustomerListRejectsInvertedRegisterRange(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	from, to := day(2026, 10, 1), day(2026, 9, 1)
	_, err := svc.ListCustomers(context.Background(), &userdto.CustomerListReq{
		Status: userdto.CustomerStatusAll, RegisteredFrom: utils.NewJSONTimePtr(&from), RegisteredTo: utils.NewJSONTimePtr(&to)})
	if err == nil || err.Error() != userenums.ErrInvalidParam {
		t.Fatalf("起止颠倒应报 %q，实得 %v", userenums.ErrInvalidParam, err)
	}
}

func TestCustomerCountersJudgeLockByTime(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	future := mkCustomer(t, m, "locked", "locked@example.com", "", usermodel.UserStatusActive, day(2026, 9, 1))
	expired := mkCustomer(t, m, "expired", "expired@example.com", "", usermodel.UserStatusActive, day(2026, 9, 2))
	now := time.Now()
	if err := m.UpdateFields(ctx, future.ID, map[string]any{
		"locked_until_time": now.Add(30 * time.Minute), "login_failure_count": 5}); err != nil {
		t.Fatalf("设置锁定失败: %v", err)
	}
	if err := m.UpdateFields(ctx, expired.ID, map[string]any{
		"locked_until_time": now.Add(-30 * time.Minute), "login_failure_count": 5}); err != nil {
		t.Fatalf("设置过期锁定失败: %v", err)
	}

	res, err := svc.ListCustomers(ctx, &userdto.CustomerListReq{Status: userdto.CustomerStatusAll})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if res.Counters.Locked != 1 {
		t.Errorf("只有锁未过期的算「已锁定」，实得 %d", res.Counters.Locked)
	}
	if res.Counters.Total != 2 || res.Counters.Active != 2 {
		t.Errorf("计数不正确：total=%d active=%d", res.Counters.Total, res.Counters.Active)
	}

	for _, item := range res.List {
		if item.Username == "expired" && item.Locked {
			t.Errorf("锁定时间已过期的账号不应显示为已锁定")
		}
		if item.Username == "locked" && !item.Locked {
			t.Errorf("锁定时间未过期的账号应显示为已锁定")
		}
	}
}

func TestSetCustomerStatusOnlyAcceptsActiveAndDisabled(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	u := mkCustomer(t, m, "carol", "carol@example.com", "", usermodel.UserStatusActive, day(2026, 9, 3))

	for _, target := range []int{usermodel.UserStatusPending, 7, -1} {
		if _, err := svc.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{CustomerID: u.ID, Status: target}); err == nil {
			t.Errorf("状态 %d 应被拒绝", target)
		} else if err.Error() != userenums.ErrCustomerStatusInvalid {
			t.Errorf("状态 %d 的错误文案应为 %q，实得 %q", target, userenums.ErrCustomerStatusInvalid, err.Error())
		}
	}

	after, err := m.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if after.Status != usermodel.UserStatusActive {
		t.Errorf("被拒绝的写入不应改库，实际状态 %d", after.Status)
	}
}

func TestSetCustomerStatusEnablesAndDisables(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	u := mkCustomer(t, m, "dave", "dave@example.com", "", usermodel.UserStatusActive, day(2026, 9, 4))

	res, err := svc.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{CustomerID: u.ID, Status: usermodel.UserStatusDisabled})
	if err != nil {
		t.Fatalf("停用失败: %v", err)
	}

	if res.Status != usermodel.UserStatusDisabled {
		t.Errorf("停用回执不正确：%+v", res)
	}
	if res.StatusLabel != "" {
		t.Errorf("service 不应产出展示文案（留给出口取词），实际 StatusLabel=%q", res.StatusLabel)
	}
	stored, err := m.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if stored.Status != usermodel.UserStatusDisabled {
		t.Fatalf("停用没有落库，实际状态 %d", stored.Status)
	}

	if _, err := svc.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{CustomerID: u.ID, Status: usermodel.UserStatusDisabled}); err != nil {
		t.Fatalf("重复停用应当幂等: %v", err)
	}
	if _, err := svc.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{CustomerID: u.ID, Status: usermodel.UserStatusActive}); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	back, err := m.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if back.Status != usermodel.UserStatusActive {
		t.Errorf("启用没有落库，实际状态 %d", back.Status)
	}
}

func TestUnlockCustomerReportsOutcomes(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	ctx := context.Background()
	u := mkCustomer(t, m, "erin", "erin@example.com", "", usermodel.UserStatusActive, day(2026, 9, 5))
	now := time.Now()

	res, err := svc.UnlockCustomer(ctx, &userdto.CustomerUnlockReq{CustomerID: u.ID})
	if err != nil {
		t.Fatalf("解锁失败: %v", err)
	}
	if res.Unlocked || res.Cleared {
		t.Errorf("本来就没事时两个标志都应为 false：%+v", res)
	}

	if err := m.UpdateFields(ctx, u.ID, map[string]any{"login_failure_count": 3}); err != nil {
		t.Fatalf("设置失败计数失败: %v", err)
	}
	res, err = svc.UnlockCustomer(ctx, &userdto.CustomerUnlockReq{CustomerID: u.ID})
	if err != nil {
		t.Fatalf("解锁失败: %v", err)
	}
	if res.Unlocked || !res.Cleared {
		t.Errorf("未锁定但有残留计数时应只报 Cleared：%+v", res)
	}
	after, err := m.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if after.LoginFailureCount != 0 {
		t.Errorf("失败计数没有清零：%d", after.LoginFailureCount)
	}

	if err := m.UpdateFields(ctx, u.ID, map[string]any{
		"login_failure_count": 5, "locked_until_time": now.Add(30 * time.Minute)}); err != nil {
		t.Fatalf("设置锁定失败: %v", err)
	}
	res, err = svc.UnlockCustomer(ctx, &userdto.CustomerUnlockReq{CustomerID: u.ID})
	if err != nil {
		t.Fatalf("解锁失败: %v", err)
	}
	if !res.Unlocked {
		t.Errorf("锁定中应报 Unlocked=true：%+v", res)
	}
	after, err = m.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if after.LockedUntilTime != nil || after.LoginFailureCount != 0 {
		t.Errorf("解锁后锁定字段应清空：lockedUntil=%v count=%d", after.LockedUntilTime, after.LoginFailureCount)
	}
}

func TestGetCustomerNotFound(t *testing.T) {
	m, svc := newCustomerFixture(t)
	if m == nil {
		return
	}
	if _, err := svc.GetCustomer(context.Background(), 999999); err == nil {
		t.Fatal("不存在的客户应报错")
	} else if err.Error() != userenums.ErrUserNotFound {
		t.Errorf("错误文案应为 %q，实得 %q", userenums.ErrUserNotFound, err.Error())
	}
}

func TestCustomerDTOExposesNoCredentialFields(t *testing.T) {

	support.AssertNoCredentialFields(t,
		reflect.TypeOf(userdto.CustomerResp{}),
		reflect.TypeOf(userdto.CustomerListResp{}),
		reflect.TypeOf(userdto.CustomerCounters{}),
		reflect.TypeOf(userdto.CustomerStatusResp{}),
		reflect.TypeOf(userdto.CustomerUnlockResp{}),
	)
}

func TestCustomerAdminPermissionSeed(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return
	}
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}

	if err := support.SeedTestAdmin(t, db, "customer-seed-admin", "Test@123456"); err != nil {
		t.Fatalf("准备测试超管失败: %v", err)
	}
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}

	want := map[string][2]string{
		"user:customer_list":   {"/api/customer/list", "GET"},
		"user:customer_detail": {"/api/customer/get", "GET"},
		"user:customer_status": {"/api/customer/status", "POST"},
		"user:customer_unlock": {"/api/customer/unlock", "POST"},
	}
	ctx := context.Background()

	for code, route := range want {
		var point struct {
			APIPath   string `gorm:"column:api_path"`
			APIMethod string `gorm:"column:api_method"`
			Status    int    `gorm:"column:status"`
		}
		if err := db.WithContext(ctx).Table("sys_permission").
			Select("api_path, api_method, status").
			Where("permission_code = ?", code).Take(&point).Error; err != nil {
			t.Errorf("权限点 %s 没有 seed（%v）—— 该接口会对含超管在内的所有人 403", code, err)
			continue
		}
		if point.APIPath != route[0] || point.APIMethod != route[1] {
			t.Errorf("权限点 %s 绑的路径不对：实得 %s %s，应为 %s %s",
				code, point.APIMethod, point.APIPath, route[1], route[0])
		}
		if point.Status != 1 {
			t.Errorf("权限点 %s 未启用（status=%d）", code, point.Status)
		}
	}

	var missing []string
	if err := db.WithContext(ctx).Table("sys_permission AS p").
		Select("p.permission_code").
		Where("p.permission_code IN ?", []string{"user:customer_list", "user:customer_detail", "user:customer_status", "user:customer_unlock"}).
		Where("NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype = 'p' AND r.v3 = p.permission_code)").
		Pluck("p.permission_code", &missing).Error; err != nil {
		t.Fatalf("查询超管策略失败: %v", err)
	}
	if len(missing) > 0 {
		t.Errorf("以下权限点没有超管策略（超管也会 403）：%v", missing)
	}

	var menus int64
	if err := db.WithContext(ctx).Table("sys_menus").
		Where("path = ? AND type = 2 AND deleted_at IS NULL", "/admin/customers").
		Count(&menus).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if menus != 1 {
		t.Errorf("后台菜单 /admin/customers 应 seed 恰好 1 条，实得 %d", menus)
	}
}
