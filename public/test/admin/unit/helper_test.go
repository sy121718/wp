// Package unit admin 模块（管理员/角色/权限/菜单/部门/数据权限）service 层单元测试。
//
// 每个测试通过 support.NewMigratedPGTestDB 获得独立的 PG schema（search_path 隔离），
// 表结构由**生产迁移**（migrations.Run）建立，不再手抄 DDL；并装配进程级全局组件：
// casbin（绑定当前测试 DB）、cache+auth（miniredis）。
// 本地 PostgreSQL 不可用时整体 t.Skip。
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

// orderProbeEntity 测试用订单域实体：不下任何真实表（sys_order 在迁移里不存在），
// 只用于声明白名单，走与生产相同的 tag 语法与 TableName() 约定。
type orderProbeEntity struct {
	OrderNo string  `gorm:"column:order_no" datarule:"label=订单号;ops=EQ,NEQ,LIKE"`
	DeptID  uint64  `gorm:"column:dept_id" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
	Price   float64 `gorm:"column:price" datarule:"label=金额;ops=EQ,NEQ,GT,GTE,LT,LTE"`
}

// TableName 返回测试域的模拟表名（引擎按表名匹配域，与真实表无关）。
func (orderProbeEntity) TableName() string { return "sys_order" }

// env 一次测试的独立环境。
type env struct {
	svc  *adminservice.Service
	db   *gorm.DB
	mini *miniredis.Miniredis
}

var uniqSeq atomic.Int64

// uniq 生成测试内唯一字符串（用户名/邮箱/编码等），避免同测试内多次创建冲突。
func uniq(prefix string) string {
	n := uniqSeq.Add(1)
	return fmt.Sprintf("%s%d%d", prefix, n, time.Now().UnixNano()%1000000)
}

// setupEnv 装配独立测试环境：
//  1. 独立 PG schema + **跑生产迁移**建表（真实 DDL，不再手抄）；
//  2. casbin 全局单例 Close 后绑定当前测试 DB（sys_casbin_rule 落在本 schema）；
//  3. miniredis 初始化 cache + auth；
//  4. 构造合并后的 admin Service。
//
// PG 不可用时 t.Skip（任务约定）。
func setupEnv(t *testing.T) *env {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)

	// 注册测试数据域：ADMIN 直接用生产声明（adminmodel.AdminDataRuleDomain，白名单由
	// AdminEntity 的 datarule tag 派生）—— 测试与运行时同一来源，不再手抄一份白名单；
	// ORDER 是测试模拟的第二个业务域，用本地 probe 实体声明，tag 语法与生产完全一致。
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

	// casbin 单例：重置后绑定当前测试 schema（进程级全局，测试包内串行执行）。
	if err := pkgcasbin.Close(); err != nil {
		t.Fatalf("重置 Casbin 失败: %v", err)
	}
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() { _ = pkgcasbin.Close() })

	// miniredis + cache + auth
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

// wantErr 断言错误：
//   - want == ""：期望 err 为 nil；
//   - 否则期望 err 非 nil 且错误文本包含 want（enums key 子串匹配）。
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

// newCaptcha 生成一组可用的验证码 id/code（走真实 captcha 单例）。
func newCaptcha(t *testing.T) (id, code string) {
	t.Helper()
	id, code = captcha.Get().Generate()
	if id == "" || code == "" {
		t.Fatalf("生成验证码失败")
	}
	return id, code
}

// createAdminDB 绕过 service 直接落库一个启用状态的用户（bcrypt 固定密码 test-pass-123）。
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

// createPerm 快速创建启用权限点（service PermCreate）。
func createPerm(t *testing.T, e *env, code, path string) uint64 {
	t.Helper()
	res, err := e.svc.PermCreate(context.Background(), &admindto.PermCreateReq{
		PermissionCode: code,
		PermissionName: "测试权限 " + code,
		Module:         "admin",
		APIPath:        path,
		APIMethod:      "GET",
		Status:         1,
	})
	wantErr(t, err, "")
	return res.ID
}

// createRole 快速创建启用角色（service RoleCreate），返回角色 ID。
func createRole(t *testing.T, e *env, code, name string) uint64 {
	t.Helper()
	enabled := adminmodel.RoleStatusEnabled
	err := e.svc.RoleCreate(context.Background(), &admindto.RoleCreateReq{
		RoleCode: code,
		RoleName: name,
		Status:   &enabled,
	})
	wantErr(t, err, "")
	// 直接查 DB 拿 ID
	var r adminmodel.RoleEntity
	if err := e.db.Where("role_code = ?", code).First(&r).Error; err != nil {
		t.Fatalf("查询角色失败: %v", err)
	}
	return r.ID
}
