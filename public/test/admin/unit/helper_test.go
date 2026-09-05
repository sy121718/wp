// Package unit admin 模块（管理员/角色/权限/菜单/部门/数据权限）service 层单元测试。
//
// 每个测试通过 support.NewPGTestDB 获得独立的 PG schema（search_path 隔离），
// 并装配进程级全局组件：casbin（绑定当前测试 DB）、cache+auth（miniredis）。
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

// createTestSchema 按 admin model 字段手工建 PG 表。
// 说明：model 的 gorm tag 是 MySQL 方言（bigint unsigned/tinyint/datetime(3)），
// Postgres AutoMigrate 会生成非法 DDL（syntax error at or near "unsigned"），
// 因此按 public/migrations/init_schema.sql 的 PG 约定建表；
// 差异点：sys_menus 补 title_key 列（model 存在），sys_rule.config 用 TEXT
// （生产为 JSONB，测试用 TEXT 以覆盖非法 JSON 解析错误分支）。
func createTestSchema(db *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sys_admin (
			id BIGSERIAL PRIMARY KEY,
			dept_id BIGINT NOT NULL DEFAULT 0,
			username VARCHAR(50) NOT NULL,
			password VARCHAR(100) NOT NULL,
			name VARCHAR(50),
			avatar VARCHAR(255),
			email VARCHAR(100),
			phone VARCHAR(20),
			status SMALLINT NOT NULL DEFAULT 1,
			is_admin SMALLINT NOT NULL DEFAULT 0,
			login_failure_count INTEGER NOT NULL DEFAULT 0,
			locked_until_time TIMESTAMP(3),
			metadata JSONB,
			last_failure_time TIMESTAMP(3),
			register_ip VARCHAR(50),
			register_location VARCHAR(100),
			last_login_ip VARCHAR(50),
			last_login_location VARCHAR(100),
			last_login_isp VARCHAR(50),
			last_login_time TIMESTAMP(3),
			create_by BIGINT NOT NULL DEFAULT 0,
			create_time TIMESTAMP(3),
			update_by BIGINT NOT NULL DEFAULT 0,
			update_time TIMESTAMP(3),
			remark VARCHAR(255),
			CONSTRAINT uk_sys_admin_username UNIQUE (username)
		)`,
		`CREATE TABLE IF NOT EXISTS sys_role (
			id BIGSERIAL PRIMARY KEY,
			role_code VARCHAR(50) NOT NULL,
			role_name VARCHAR(100) NOT NULL,
			status SMALLINT NOT NULL DEFAULT 1,
			is_system SMALLINT NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			remark VARCHAR(200),
			create_by BIGINT NOT NULL DEFAULT 0,
			create_time TIMESTAMP(3),
			update_by BIGINT NOT NULL DEFAULT 0,
			update_time TIMESTAMP(3),
			CONSTRAINT uk_sys_role_code UNIQUE (role_code)
		)`,
		`CREATE TABLE IF NOT EXISTS sys_permission (
			id BIGSERIAL PRIMARY KEY,
			permission_code VARCHAR(100) NOT NULL,
			permission_name VARCHAR(100) NOT NULL,
			module VARCHAR(50) NOT NULL,
			api_path VARCHAR(200) NOT NULL,
			api_method VARCHAR(10) NOT NULL DEFAULT 'GET',
			status SMALLINT NOT NULL DEFAULT 1,
			remark VARCHAR(200),
			create_by BIGINT NOT NULL DEFAULT 0,
			create_time TIMESTAMP(3),
			update_by BIGINT NOT NULL DEFAULT 0,
			update_time TIMESTAMP(3),
			CONSTRAINT uk_sys_permission_code UNIQUE (permission_code)
		)`,
		`CREATE TABLE IF NOT EXISTS sys_menus (
			id BIGSERIAL PRIMARY KEY,
			permission_code VARCHAR(100),
			title VARCHAR(50) NOT NULL,
			title_key VARCHAR(100),
			parent_id BIGINT NOT NULL DEFAULT 0,
			type SMALLINT NOT NULL DEFAULT 2,
			path VARCHAR(100) NOT NULL DEFAULT '',
			component VARCHAR(255) NOT NULL DEFAULT '',
			external_url VARCHAR(300) NOT NULL DEFAULT '',
			icon VARCHAR(50) NOT NULL DEFAULT '',
			status SMALLINT NOT NULL DEFAULT 1,
			is_hidden SMALLINT NOT NULL DEFAULT 0,
			is_public SMALLINT NOT NULL DEFAULT 0,
			is_system SMALLINT NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			remark VARCHAR(200),
			create_by BIGINT NOT NULL DEFAULT 0,
			create_time TIMESTAMP(3),
			update_by BIGINT NOT NULL DEFAULT 0,
			update_time TIMESTAMP(3),
			deleted_time TIMESTAMP(3)
		)`,
		`CREATE TABLE IF NOT EXISTS sys_dept (
			id BIGSERIAL PRIMARY KEY,
			parent_id BIGINT NOT NULL DEFAULT 0,
			ancestors VARCHAR(500) NOT NULL DEFAULT '',
			dept_name VARCHAR(100) NOT NULL,
			dept_code VARCHAR(50) NOT NULL,
			leader_id BIGINT,
			sort_order INTEGER NOT NULL DEFAULT 0,
			status SMALLINT NOT NULL DEFAULT 1,
			remark VARCHAR(200),
			create_by BIGINT NOT NULL DEFAULT 0,
			create_time TIMESTAMP(3),
			update_by BIGINT NOT NULL DEFAULT 0,
			update_time TIMESTAMP(3),
			CONSTRAINT uk_sys_dept_code UNIQUE (dept_code)
		)`,
		`CREATE TABLE IF NOT EXISTS sys_rule (
			id BIGSERIAL PRIMARY KEY,
			rule_name VARCHAR(100) NOT NULL,
			domain VARCHAR(50) NOT NULL,
			config TEXT NOT NULL DEFAULT '{}',
			status SMALLINT NOT NULL DEFAULT 1,
			remark VARCHAR(200),
			create_by BIGINT NOT NULL DEFAULT 0,
			create_time TIMESTAMP(3),
			update_by BIGINT NOT NULL DEFAULT 0,
			update_time TIMESTAMP(3)
		)`,
		`CREATE TABLE IF NOT EXISTS sys_rule_assignment (
			id BIGSERIAL PRIMARY KEY,
			rule_id BIGINT NOT NULL,
			target_type SMALLINT NOT NULL,
			target_id BIGINT NOT NULL,
			target_scope SMALLINT NOT NULL DEFAULT 0,
			create_by BIGINT NOT NULL DEFAULT 0,
			create_time TIMESTAMP(3)
		)`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// setupEnv 装配独立测试环境：
//  1. 独立 PG schema + 手工建 admin 表（PG 方言 DDL）；
//  2. casbin 全局单例 Close 后绑定当前测试 DB（sys_casbin_rule 落在本 schema）；
//  3. miniredis 初始化 cache + auth；
//  4. 构造合并后的 admin Service。
//
// PG 不可用时 t.Skip（任务约定）。
func setupEnv(t *testing.T) *env {
	t.Helper()

	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
	}

	if err := createTestSchema(db); err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	// 注册测试数据域（生产由 admin_router.registerDomains 装配时注册；此处幂等注册）。
	// ADMIN 与生产一致；ORDER 为测试模拟的第二个业务域（datarule 测试大量使用）。
	datarule.RegisterDomain(datarule.DomainConfig{
		Domain:      "ADMIN",
		DomainLabel: "管理员",
		TableName:   "sys_admin",
		WhiteList: []datarule.FieldDef{
			{Field: "username", Label: "用户名", DataType: "varchar", Operators: []string{"EQ", "NEQ", "LIKE", "NOT_LIKE"}},
			{Field: "email", Label: "邮箱", DataType: "varchar", Operators: []string{"EQ", "NEQ", "LIKE"}},
			{Field: "phone", Label: "手机号", DataType: "varchar", Operators: []string{"EQ", "NEQ"}},
			{Field: "status", Label: "状态", DataType: "tinyint", Operators: []string{"EQ", "NEQ", "IN", "NOT_IN"}},
			{Field: "dept_id", Label: "所属部门", DataType: "bigint", Operators: []string{"EQ", "NEQ", "IN", "NOT_IN"}},
		},
	})
	datarule.RegisterDomain(datarule.DomainConfig{
		Domain:      "ORDER",
		DomainLabel: "订单",
		TableName:   "sys_order",
		WhiteList: []datarule.FieldDef{
			{Field: "order_no", Label: "订单号", DataType: "varchar", Operators: []string{"EQ", "NEQ", "LIKE"}},
			{Field: "price", Label: "金额", DataType: "decimal", Operators: []string{"EQ", "NEQ", "GT", "GTE", "LT", "LTE"}},
		},
	})

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
