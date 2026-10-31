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

	session, err := auth.GetUserSession(ctx, id)
	wantErr(t, err, "")
	if session == nil || session.Username != username {
		t.Fatalf("登录后会话应写入 Redis")
	}
}

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

	session, err := auth.GetUserSession(ctx, res.UserID)
	wantErr(t, err, "")
	if session == nil || session.Username == "" {
		t.Fatalf("登录后会话应写入 Redis")
	}
}

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

func TestAdminLoginUserNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	cid, code := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: "no_such_user_xyz", Password: "test-pass-123", CaptchaID: cid, Captcha: code,
	}, "")
	wantErr(t, err, adminenums.ErrBadCredentials)
}

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

func TestAdminLoginCaptchaWrong(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	cid, _ := newCaptcha(t)
	_, err := e.svc.AdminLogin(ctx, &admindto.AdminLoginReq{
		Username: uniq("cap"), Password: "test-pass-123", CaptchaID: cid, Captcha: "000000",
	}, "")
	wantErr(t, err, adminenums.ErrCaptchaExpired)
}

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

func TestAdminDetailNotExistReturnsError(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	res, err := e.svc.AdminDetail(ctx, &admindto.AdminDetailReq{Id: 999999})
	if err == nil {
		t.Fatalf("已知缺陷：查询不存在的管理员应返回 ErrAdminNotFound，实际返回 nil error，resp=%+v", res)
	}
	wantErr(t, err, adminenums.ErrAdminNotFound)
}

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

func TestAdminEditNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.AdminEdit(ctx, &admindto.AdminEditReq{
		Id: 888888, Username: uniq("n"), Email: uniq("n") + "@example.com",
	})
	wantErr(t, err, adminenums.ErrAdminNotFound)
}

func TestAdminEditInvalidatesOldSession(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("editse"), uniq("editse")+"@example.com")

	err := auth.SaveUserSession(ctx, &auth.UserSession{ID: id, SessionID: "sess-1", Username: uniq("editse")}, 0)
	wantErr(t, err, "")
	session, err := auth.GetUserSession(ctx, id)
	wantErr(t, err, "")
	if session == nil {
		t.Fatalf("前置条件：会话应已写入")
	}

	newName := uniq("edited2")
	_, err = e.svc.AdminEdit(ctx, &admindto.AdminEditReq{
		Id: id, Username: newName, Email: newName + "@example.com",
	})
	wantErr(t, err, "")

	after, err := auth.GetUserSession(ctx, id)
	wantErr(t, err, "")
	if after != nil {
		t.Fatalf("编辑成功后旧会话应被删除")
	}
}

func TestAdminEditSessionRevokeFailureKeepsSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("editfail"), uniq("editfail")+"@example.com")

	e.mini.Close()

	newName := uniq("editedfail")
	_, err := e.svc.AdminEdit(ctx, &admindto.AdminEditReq{
		Id: id, Username: newName, Email: newName + "@example.com",
	})
	wantErr(t, err, "")

	var entity adminmodel.AdminEntity
	if err := e.db.First(&entity, id).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if entity.Username != newName {
		t.Fatalf("DB 更新应已生效: got=%s want=%s", entity.Username, newName)
	}
}

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

func TestAdminDeleteSuperAdmin(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("super"), uniq("super")+"@example.com")

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

func TestAdminDeleteNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.AdminDelete(ctx, &admindto.AdminDeleteReq{
		Id:         []uint64{777777},
		OperatorID: 1,
	})
	wantErr(t, err, adminenums.ErrAdminNotFound)
}

func TestAdminDeleteSuccess(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("delok"), uniq("delok")+"@example.com")

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

func TestAdminListExcludesSuperAdmin(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	u1 := uniq("list1")
	u2 := uniq("list2")
	createAdminDB(t, e.db, u1, u1+"@example.com")
	createAdminDB(t, e.db, u2, u2+"@example.com")

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

	res, err = e.svc.AdminList(ctx, &admindto.AdminListReq{Email: u1})
	wantErr(t, err, "")
	if res.Total != 1 || res.List[0].Username != u1 {
		t.Fatalf("邮箱筛选结果不符: total=%d", res.Total)
	}
}

func TestAdminProfileFallbackToDB(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	id := createAdminDB(t, e.db, uniq("profile"), uniq("profile")+"@example.com")

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

func TestAdminProfileNotFound(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	_, err := e.svc.AdminProfile(ctx, 999999)
	wantErr(t, err, adminenums.ErrUserNotFound)
}

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
