package unit

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/auth"
	pkgcasbin "go_wp/pkg/casbin"

	"golang.org/x/crypto/bcrypt"
)

// --- 创建 ---

// TestAdminCreateSuccess 正常创建：密码 bcrypt 哈希落库、默认启用、Name 取用户名。
func TestAdminCreateSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("creator")
	email := username + "@example.com"
	res, err := e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: username,
		Email:    email,
		Password: "pass-123456",
		Phone:    "13800000000",
	})
	wantErr(t, err, "")
	if res.ID == 0 {
		t.Fatalf("创建返回 ID 不应为 0")
	}
	if res.Username != username {
		t.Fatalf("返回用户名不符: got=%s want=%s", res.Username, username)
	}

	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, res.ID).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if entity.Password == "pass-123456" {
		t.Fatalf("密码不应明文落库")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(entity.Password), []byte("pass-123456")); err != nil {
		t.Fatalf("bcrypt 哈希无法验证: %v", err)
	}
	if entity.Status != adminmodel.AdminStatusActive {
		t.Fatalf("新用户应默认启用: got=%d", entity.Status)
	}
	if entity.Name == nil || *entity.Name != username {
		t.Fatalf("Name 应取用户名: got=%v", entity.Name)
	}
	if entity.Email == nil || *entity.Email != email {
		t.Fatalf("Email 未正确保存: got=%v", entity.Email)
	}
}

// TestAdminCreateDuplicateUsername 重复用户名被拒绝。
func TestAdminCreateDuplicateUsername(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("dupuser")
	email := username + "@example.com"
	_, err := e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: username, Email: email, Password: "pass-123456",
	})
	wantErr(t, err, "")
	_, err = e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: username, Email: uniq("e") + "@example.com", Password: "pass-123456",
	})
	wantErr(t, err, adminenums.ErrUsernameExists)
}

// TestAdminCreateDuplicateEmail 重复邮箱被拒绝。
func TestAdminCreateDuplicateEmail(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	email := uniq("dupemail") + "@example.com"
	_, err := e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: uniq("u1"), Email: email, Password: "pass-123456",
	})
	wantErr(t, err, "")
	_, err = e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: uniq("u2"), Email: email, Password: "pass-123456",
	})
	wantErr(t, err, adminenums.ErrEmailExists)
}

// TestAdminCreateDuplicatePhone 重复手机号被拒绝。
func TestAdminCreateDuplicatePhone(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	phone := "13900001111"
	_, err := e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: uniq("p1"), Email: uniq("e1") + "@example.com",
		Password: "pass-123456", Phone: phone,
	})
	wantErr(t, err, "")
	_, err = e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: uniq("p2"), Email: uniq("e2") + "@example.com",
		Password: "pass-123456", Phone: phone,
	})
	wantErr(t, err, adminenums.ErrPhoneExists)
}

// TestAdminCreateEmptyPasswordServiceLacksValidation 观察项：service 层不校验空密码（依赖 handle 层 binding）。
func TestAdminCreateEmptyPasswordServiceLacksValidation(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	res, err := e.svc.AdminCreate(ctx, &admindto.AdminCreateReq{
		Username: uniq("nopass"),
		Email:    uniq("nopass") + "@example.com",
		Password: "",
	})
	wantErr(t, err, "")
	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, res.ID).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(entity.Password), []byte("")); err != nil {
		t.Fatalf("空密码应被哈希并可通过空串验证（说明 service 层未拒绝空密码）: %v", err)
	}
}

// --- 登录 ---

// TestAdminLoginSuccess 正确账号密码 + 验证码登录成功，记录登录信息并写入会话。
func TestAdminLoginSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("login")
	id := createAdminDB(t, e.db, username, uniq("loginmail")+"@example.com")
	cid, code := newCaptcha(t)
	res, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username:  username,
		Password:  "test-pass-123",
		CaptchaID: cid,
		Captcha:   code,
	}, "127.0.0.1")
	wantErr(t, err, "")
	if res.UserID != id || res.SessionID == "" {
		t.Fatalf("登录响应应包含正确 UserID 与 SessionID: %+v", res)
	}
	// 登录信息已写入
	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, id).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if entity.LastLoginTime == nil {
		t.Fatalf("登录成功后 last_login_time 应被写入")
	}
	if entity.LastLoginIP == nil || *entity.LastLoginIP != "127.0.0.1" {
		t.Fatalf("登录成功后 last_login_ip 应被写入: %v", entity.LastLoginIP)
	}
	if entity.LoginFailureCount != 0 {
		t.Fatalf("登录成功后失败计数应清零: %d", entity.LoginFailureCount)
	}
	// 会话已写入 Redis
	session, err := auth.GetUserSession(ctx, id)
	wantErr(t, err, "")
	if session == nil || session.Username != username {
		t.Fatalf("登录后会话应写入 Redis")
	}
}

// TestAdminLoginByEmail 使用邮箱作为账号登录成功。
func TestAdminLoginByEmail(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	email := uniq("loginmail") + "@example.com"
	createAdminDB(t, e.db, uniq("u"), email)
	cid, code := newCaptcha(t)
	res, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username:  email,
		Password:  "test-pass-123",
		CaptchaID: cid,
		Captcha:   code,
	}, "127.0.0.1")
	wantErr(t, err, "")
	if res.UserID == 0 || res.SessionID == "" {
		t.Fatalf("登录响应应包含 UserID 与 SessionID: %+v", res)
	}
	// 登录信息已写入
	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, res.UserID).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if entity.LastLoginTime == nil {
		t.Fatalf("登录成功后 last_login_time 应被写入")
	}
	if entity.LastLoginIP == nil || *entity.LastLoginIP != "127.0.0.1" {
		t.Fatalf("登录成功后 last_login_ip 应被写入: %v", entity.LastLoginIP)
	}
	if entity.LoginFailureCount != 0 {
		t.Fatalf("登录成功后失败计数应清零: %d", entity.LoginFailureCount)
	}
	// 会话已写入 Redis
	session, err := auth.GetUserSession(ctx, res.UserID)
	wantErr(t, err, "")
	if session == nil || session.Username == "" {
		t.Fatalf("登录后会话应写入 Redis")
	}
}

// TestAdminLoginWrongPassword 密码错误返回统一错误并累加失败计数。
func TestAdminLoginWrongPassword(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("wrongpass")
	id := createAdminDB(t, e.db, username, uniq("wrongpassmail")+"@example.com")
	cid, code := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: username, Password: "wrong-password", CaptchaID: cid, Captcha: code,
	}, "")
	wantErr(t, err, adminenums.ErrBadCredentials)

	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, id).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if entity.LoginFailureCount != 1 {
		t.Fatalf("失败计数应累加为 1: got=%d", entity.LoginFailureCount)
	}
}

// TestAdminLoginUserNotFound 用户不存在返回模糊错误（不泄露用户存在性）。
func TestAdminLoginUserNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	cid, code := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: "no_such_user_xyz", Password: "test-pass-123", CaptchaID: cid, Captcha: code,
	}, "")
	wantErr(t, err, adminenums.ErrBadCredentials)
}

// TestAdminLoginDisabled 禁用用户登录被拒绝。
func TestAdminLoginDisabled(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("disabled")
	email := username + "@example.com"
	id := createAdminDB(t, e.db, username, email)
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", id).Update("status", adminmodel.AdminStatusInactive).Error; err != nil {
		t.Fatalf("禁用用户失败: %v", err)
	}
	cid, code := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: username, Password: "test-pass-123", CaptchaID: cid, Captcha: code,
	}, "")
	wantErr(t, err, adminenums.ErrAccountDisabled)
}

// TestAdminLoginCaptchaWrong 验证码错误被拒绝（不触达账号查询）。
func TestAdminLoginCaptchaWrong(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	cid, _ := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: uniq("cap"), Password: "test-pass-123", CaptchaID: cid, Captcha: "000000",
	}, "")
	wantErr(t, err, adminenums.ErrCaptchaExpired)
}

// TestAdminLoginLockedAfterFiveFailures 连续 5 次失败后账号锁定 30 分钟。
func TestAdminLoginLockedAfterFiveFailures(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("lockme")
	createAdminDB(t, e.db, username, username+"@example.com")

	for i := 0; i < 5; i++ {
		cid, code := newCaptcha(t)
		_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
			Username: username, Password: "wrong-password", CaptchaID: cid, Captcha: code,
		}, "")
		wantErr(t, err, adminenums.ErrBadCredentials)
	}

	// 第 6 次即使密码正确也因锁定被拒
	cid, code := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: username, Password: "test-pass-123", CaptchaID: cid, Captcha: code,
	}, "")
	wantErr(t, err, adminenums.ErrAccountLocked)

	var entity adminmodel.AdminEntity
	if err := e.db.Where("username = ?", username).First(&entity).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if entity.LockedUntilTime == nil {
		t.Fatalf("锁定后 locked_until_time 应非空")
	}
	if entity.Status != adminmodel.AdminStatusActive {
		t.Fatalf("锁定不应修改 status（避免永久禁用）: got=%d", entity.Status)
	}
}

// TestAdminLoginUnlockAfterLockExpiry 锁定到期后自动解锁（locked_until_time 过期间隔，不改 status 也能登录）。
func TestAdminLoginUnlockAfterLockExpiry(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("unlock")
	id := createAdminDB(t, e.db, username, username+"@example.com")
	past := time.Now().Add(-time.Minute)
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", id).Update("locked_until_time", &past).Error; err != nil {
		t.Fatalf("设置过期锁定时失败: %v", err)
	}

	cid, code := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: username, Password: "test-pass-123", CaptchaID: cid, Captcha: code,
	}, "")
	wantErr(t, err, "")
}

// --- 详情 / 编辑 / 删除 ---

// TestAdminDetailSuccess 正常详情查询。
func TestAdminDetailSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	username := uniq("detail")
	email := username + "@example.com"
	id := createAdminDB(t, e.db, username, email)
	res, err := e.svc.AdminDetail(ctx, &admindto.AdminDetailReq{Id: id})
	wantErr(t, err, "")
	if res.ID != id || res.Username != username {
		t.Fatalf("详情字段不符: %+v", res)
	}
	if res.Email != email {
		t.Fatalf("Email 不符: got=%q", res.Email)
	}
}

// TestAdminDetailNotExistReturnsError 已知缺陷复现：不存在的管理员应返回 ErrAdminNotFound，
// 当前实现 Scan 不命中时静默返回空结构体（与 PermDetail 的 ID==0 检查对比）。
func TestAdminDetailNotExistReturnsError(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	res, err := e.svc.AdminDetail(ctx, &admindto.AdminDetailReq{Id: 999999})
	if err == nil {
		t.Fatalf("已知缺陷：查询不存在的管理员应返回 ErrAdminNotFound，实际返回 nil error，resp=%+v", res)
	}
	wantErr(t, err, adminenums.ErrAdminNotFound)
}

// TestAdminEditSuccess 正常修改用户名/邮箱/手机号。
func TestAdminEditSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("edit"), uniq("edit")+"@example.com")
	newName := uniq("edited")
	newEmail := newName + "@example.com"
	_, err := e.svc.AdminEdit(ctx, &admindto.AdminEditReq{
		Id:       id,
		Username: newName,
		Email:    newEmail,
		Phone:    "13700001111",
	})
	wantErr(t, err, "")

	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, id).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if entity.Username != newName || entity.Email == nil || *entity.Email != newEmail {
		t.Fatalf("编辑后字段不符: %+v", entity)
	}
	if entity.Phone == nil || *entity.Phone != "13700001111" {
		t.Fatalf("Phone 未更新: %v", entity.Phone)
	}
}

// TestAdminEditNotFound 编辑不存在的管理员报错。
func TestAdminEditNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.AdminEdit(ctx, &admindto.AdminEditReq{
		Id: 888888, Username: uniq("n"), Email: uniq("n") + "@example.com",
	})
	wantErr(t, err, adminenums.ErrAdminNotFound)
}

// TestAdminEditUsernameExists 编辑为他人已占用用户名被拒绝。
func TestAdminEditUsernameExists(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	u1 := uniq("eu1")
	u2 := uniq("eu2")
	id1 := createAdminDB(t, e.db, u1, u1+"@example.com")
	createAdminDB(t, e.db, u2, u2+"@example.com")

	_, err := e.svc.AdminEdit(ctx, &admindto.AdminEditReq{
		Id: id1, Username: u2, Email: u1 + "@example.com",
	})
	wantErr(t, err, adminenums.ErrUsernameExists)
}

// TestAdminEditEmailExists 编辑为他人已占用邮箱被拒绝。
func TestAdminEditEmailExists(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	u1 := uniq("ee1")
	u2 := uniq("ee2")
	id1 := createAdminDB(t, e.db, u1, u1+"@example.com")
	createAdminDB(t, e.db, u2, u2+"@example.com")

	_, err := e.svc.AdminEdit(ctx, &admindto.AdminEditReq{
		Id: id1, Username: u1, Email: u2 + "@example.com",
	})
	wantErr(t, err, adminenums.ErrEmailExists)
}

// TestAdminDeleteSelf 删除自己被拒绝。
func TestAdminDeleteSelf(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("self"), uniq("self")+"@example.com")
	_, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id:         []uint64{id},
		OperatorID: id,
	})
	wantErr(t, err, adminenums.ErrDeleteSelf)
}

// TestAdminDeleteSuperAdmin 删除超级管理员被拒绝。
func TestAdminDeleteSuperAdmin(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("super"), uniq("super")+"@example.com")
	// 提升为超管需要内部授权上下文（allow_modify_is_admin），模拟受信任内部调用
	adminCtx := context.WithValue(ctx, "allow_modify_is_admin", true)
	if err := e.db.WithContext(adminCtx).Model(&adminmodel.AdminEntity{}).
		Where("id = ?", id).Update("is_admin", 1).Error; err != nil {
		t.Fatalf("设置超管失败: %v", err)
	}
	_, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id:         []uint64{id},
		OperatorID: id + 1,
	})
	wantErr(t, err, adminenums.ErrDeleteSuperAdmin)
}

// TestAdminDeleteNotFound 删除不存在的管理员报错。
func TestAdminDeleteNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id:         []uint64{777777},
		OperatorID: 1,
	})
	wantErr(t, err, adminenums.ErrAdminNotFound)
}

// TestAdminDeleteSuccess 删除成功：记录消失、会话被撤销、Casbin 授权被清理。
func TestAdminDeleteSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("delok"), uniq("delok")+"@example.com")
	// 给用户绑定角色与直接权限，验证删除后清理
	roleCode := "role_del_" + uniq("")
	if err := pkgcasbin.ReplaceUserRoleBindings(idStr(id), []string{roleCode}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}
	if err := pkgcasbin.ReplaceUserPermissions(idStr(id), [][3]string{{"/api/x", "GET", "x:list"}}); err != nil {
		t.Fatalf("写入权限失败: %v", err)
	}

	res, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id:         []uint64{id},
		OperatorID: id + 1,
	})
	wantErr(t, err, "")
	if res.DeletedCount != 1 {
		t.Fatalf("删除数量不符: got=%d", res.DeletedCount)
	}

	var count int64
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("删除后记录应不存在")
	}
	codes, err := pkgcasbin.GetRoleCodesByUserID(idStr(id))
	wantErr(t, err, "")
	if len(codes) != 0 {
		t.Fatalf("删除后 Casbin 角色绑定应被清理: %v", codes)
	}
	perms, err := pkgcasbin.GetUserDirectPermissions(idStr(id))
	wantErr(t, err, "")
	if len(perms) != 0 {
		t.Fatalf("删除后 Casbin 直接权限应被清理: %v", perms)
	}
}

// --- 列表 / 资料 ---

// TestAdminListExcludesSuperAdmin 列表默认排除超管，支持邮箱筛选与分页。
func TestAdminListExcludesSuperAdmin(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	u1 := uniq("list1")
	u2 := uniq("list2")
	createAdminDB(t, e.db, u1, u1+"@example.com")
	createAdminDB(t, e.db, u2, u2+"@example.com")
	// 直接落库一个超管
	hashed, _ := bcrypt.GenerateFromPassword([]byte("x12345678"), bcrypt.DefaultCost)
	superEmail := uniq("super") + "@example.com"
	if err := e.db.Create(&adminmodel.AdminEntity{
		Username: uniq("superu"), Password: string(hashed), Email: &superEmail,
		Status: adminmodel.AdminStatusActive, IsAdmin: 1,
	}).Error; err != nil {
		t.Fatalf("创建超管失败: %v", err)
	}

	res, err := e.svc.AdminList(ctx, &admindto.AdminListReq{})
	wantErr(t, err, "")
	if res.Total != 2 {
		t.Fatalf("列表应排除超管: total=%d", res.Total)
	}
	for _, item := range res.List {
		if item.Email != nil && strings.Contains(*item.Email, "super") {
			t.Fatalf("超管不应出现在列表中")
		}
	}

	// 邮箱筛选
	res, err = e.svc.AdminList(ctx, &admindto.AdminListReq{Email: u1})
	wantErr(t, err, "")
	if res.Total != 1 || res.List[0].Username != u1 {
		t.Fatalf("邮箱筛选结果不符: total=%d", res.Total)
	}
}

// TestAdminProfileFallbackToDB Redis 未命中时回源数据库并回填。
func TestAdminProfileFallbackToDB(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("profile"), uniq("profile")+"@example.com")
	// 确保 Redis 无会话
	_ = auth.DeleteUserSession(ctx, id)

	res, err := e.svc.AdminProfile(ctx, id)
	wantErr(t, err, "")
	if res.ID != id || res.Username == "" {
		t.Fatalf("Profile 回源数据不符: %+v", res)
	}
	session, err := auth.GetUserSession(ctx, id)
	wantErr(t, err, "")
	if session == nil {
		t.Fatalf("回源后应回填 Redis 会话")
	}
}

// TestAdminProfileNotFound 不存在的用户 Profile 报错。
func TestAdminProfileNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.AdminProfile(ctx, 999999)
	wantErr(t, err, adminenums.ErrUserNotFound)
}

// --- model 层约束 ---

// TestAdminModelSuperAdminUnique 超管唯一性：第二个超管被 BeforeCreate 拒绝。
func TestAdminModelSuperAdminUnique(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	hashed, _ := bcrypt.GenerateFromPassword([]byte("x12345678"), bcrypt.DefaultCost)
	mk := func(name, email string) *adminmodel.AdminEntity {
		return &adminmodel.AdminEntity{
			Username: name, Password: string(hashed), Email: &email,
			Status: adminmodel.AdminStatusActive, IsAdmin: 1,
		}
	}
	if err := e.db.WithContext(ctx).Create(mk(uniq("sa1"), uniq("sa1")+"@example.com")).Error; err != nil {
		t.Fatalf("创建第一个超管失败: %v", err)
	}
	err := e.db.WithContext(ctx).Create(mk(uniq("sa2"), uniq("sa2")+"@example.com")).Error
	wantErr(t, err, adminenums.ErrSuperAdminExists)
}

// TestAdminModelIsAdminFieldProtected 外部直接更新 IsAdmin 被 BeforeUpdate 拒绝。
func TestAdminModelIsAdminFieldProtected(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("protect"), uniq("protect")+"@example.com")
	err := e.db.WithContext(ctx).Model(&adminmodel.AdminEntity{}).
		Where("id = ?", id).Update("is_admin", 1).Error
	wantErr(t, err, adminenums.ErrFieldProtected)

	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, id).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if entity.IsAdmin != 0 {
		t.Fatalf("IsAdmin 不应被外部修改: got=%d", entity.IsAdmin)
	}
}

// TestAdminDeleteZeroID 全 0 / 空 ID 列表经唯一化后为空，返回管理员不存在。
func TestAdminDeleteZeroID(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id:         []uint64{0, 0},
		OperatorID: 1,
	})
	wantErr(t, err, adminenums.ErrAdminNotFound)

	_, err = e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id:         nil,
		OperatorID: 1,
	})
	wantErr(t, err, adminenums.ErrAdminNotFound)
}

func idStr(id uint64) string {
	return strconv.FormatUint(id, 10)
}
