package unit

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/datarule"
	support "go_wp/public/test/support"

	"github.com/alicebob/miniredis/v2"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type orderProbeEntity struct {
	OrderNo string  `gorm:"column:order_no" datarule:"label=订单号;ops=EQ,NEQ,LIKE"`
	DeptID  uint64  `gorm:"column:dept_id" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
	Price   float64 `gorm:"column:price" datarule:"label=金额;ops=EQ,NEQ,GT,GTE,LT,LTE"`
}

func (orderProbeEntity) TableName() string { return "sys_order" }

type env struct {
	svc  *adminservice.Service
	db   *gorm.DB
	mini *miniredis.Miniredis
}

var uniqSeq atomic.Int64

func uniq(prefix string) string {
	n := uniqSeq.Add(1)
	return fmt.Sprintf("%s%d%d", prefix, n, time.Now().UnixNano()%1000000)
}

func setupEnv(t *testing.T) *env {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)

	adminDomain, err := adminmodel.AdminDataRuleDomain()
	if err != nil {
		t.Fatalf("生成 ADMIN 数据域失败: %v", err)
	}
	if err := datarule.RegisterDomain(adminDomain); err != nil {
		t.Fatalf("注册 ADMIN 数据域失败: %v", err)
	}
	orderDomain, err := datarule.DomainFromEntity("ORDER", "订单", orderProbeEntity{})
	if err != nil {
		t.Fatalf("生成 ORDER 数据域失败: %v", err)
	}
	if err := datarule.RegisterDomain(orderDomain); err != nil {
		t.Fatalf("注册 ORDER 数据域失败: %v", err)
	}

	if err := pkgcasbin.Close(); err != nil {
		t.Fatalf("重置 Casbin 失败: %v", err)
	}
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() { _ = pkgcasbin.Close() })

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	t.Cleanup(mini.Close)

	cfg := viper.New()
	cfg.Set("redis.enabled", true)
	cfg.Set("redis.addrs", []string{mini.Addr()})
	cfg.Set("auth.session_secret", "admin-unit-test-secret")
	if err := cache.Init(cfg); err != nil {
		t.Fatalf("初始化 cache 失败: %v", err)
	}
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化 auth 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = auth.Close()
		_ = cache.Close()
	})

	return &env{svc: adminservice.NewService(db), db: db, mini: mini}
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("期望成功，实际得到错误: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("期望错误 %q，实际得到 nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("期望错误包含 %q，实际得到: %v", want, err)
	}
}

func newCaptcha(t *testing.T) (id, code string) {
	t.Helper()
	id, code = captcha.Get().Generate()
	if id == "" || code == "" {
		t.Fatalf("生成验证码失败")
	}
	return id, code
}

func createAdminDB(t *testing.T, db *gorm.DB, username, email string) uint64 {
	t.Helper()
	hashed, err := bcrypt.GenerateFromPassword([]byte("test-pass-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt 失败: %v", err)
	}
	e := &adminmodel.AdminEntity{
		Username: username,
		Password: string(hashed),
		Email:    &email,
		Status:   adminmodel.AdminStatusActive,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return e.ID
}

func createPerm(t *testing.T, e *env, code, path string) uint64 {
	t.Helper()
	return createPermInModule(t, e, code, path, "admin")
}

func createPermInModule(t *testing.T, e *env, code, path, module string) uint64 {
	t.Helper()
	res, err := e.svc.PermCreate(context.Background(), &admindto.PermCreateReq{
		PermissionCode: code,
		PermissionName: "测试权限 " + code,
		Module:         module,
		APIPath:        path,
		APIMethod:      "GET",
		Status:         1,
	})
	wantErr(t, err, "")
	return res.ID
}

func createRole(t *testing.T, e *env, code, name string) uint64 {
	t.Helper()
	enabled := adminmodel.RoleStatusEnabled
	err := e.svc.RoleCreate(context.Background(), &admindto.RoleCreateReq{
		RoleCode: code,
		RoleName: name,
		Status:   &enabled,
	})
	wantErr(t, err, "")

	var r adminmodel.RoleEntity
	if err := e.db.Where("role_code = ?", code).First(&r).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	return r.ID
}
