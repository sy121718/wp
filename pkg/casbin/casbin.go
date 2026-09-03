package casbin

import (
	"fmt"
	"strings"
	"sync"

	"go_wp/pkg/database"
	"go_wp/pkg/logger"

	casbinlib "github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// Casbin 权限管理组件
// 使用 RBAC 模型，策略通过 GORM Adapter 持久化到数据库。
//
// 策略类型：
//   - p, sub, obj, act, code  — 权限策略（sub 可以是 role_code 或 user_id）
//   - g, user_id, role_code   — 用户角色关系
//   - g2, role_code, active   — 角色启用状态（active=1 表示启用，缺失表示禁用）
//
// matcher 语义：
//   - r.sub == p.sub          — 用户直接额外权限（p.sub 为 user_id 时直接命中）
//   - g(r.sub, p.sub)         — 角色继承（p.sub 为 role_code 时通过 g 关系命中）
//   - g2(p.sub, "active")     — 角色必须处于启用状态
//
// role_code 约定：
//   - 禁止纯数字，避免与 user_id subject 冲突。
//   - sys_role.role_code 建议使用字母前缀 + 可选数字，如 editor、viewer、role_01。

var (
	enforcer *casbinlib.Enforcer
	mu       sync.RWMutex
	policyMu sync.Mutex

	// urlCodeMap 内存映射：URL+Method → code
	// 用于例外查询时快速找到请求 URL 对应的 code，避免每次 deny 都查数据库
	urlCodeMap sync.Map
)

// urlCodeKey 生成 URL+Method 的映射 key
func urlCodeKey(url, method string) string {
	return url + "||" + strings.ToUpper(method)
}

// rbacModel RBAC 权限模型定义。
//
// g  = 用户↔角色关系（user_id, role_code）
// g2 = 角色启用状态（role_code, "active"），用于启停角色而不删除关联
const rbacModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act, code

[role_definition]
g = _, _
g2 = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = (r.sub == p.sub || (g(r.sub, p.sub) && g2(p.sub, "active"))) && r.obj == p.obj && r.act == p.act
`

// GetEnforcer 获取 Casbin Enforcer 实例
func GetEnforcer() *casbinlib.Enforcer {
	mu.RLock()
	defer mu.RUnlock()
	return enforcer
}

// GetCodeByURL 根据 URL+Method 查询对应的权限 code。
// 从内存映射中查找，不涉及数据库查询。
// 返回 code 和是否存在。用于例外查询场景：Casbin deny 后查此映射拿到 code，再查例外表。
func GetCodeByURL(url, method string) (string, bool) {
	val, ok := urlCodeMap.Load(urlCodeKey(url, method))
	if !ok {
		return "", false
	}
	return val.(string), true
}

// Init 初始化 Casbin 组件。
func Init(_ *viper.Viper) error {
	db, err := database.GetDB()
	if err != nil {
		return fmt.Errorf("获取数据库实例失败: %w", err)
	}
	return InitCasbin(db)
}

// InitCasbin 初始化 Casbin（传入已有的 GORM DB 实例）
func InitCasbin(db *gorm.DB) error {
	mu.Lock()
	defer mu.Unlock()

	if enforcer != nil {
		return nil
	}

	instance, err := initCasbin(db)
	if err != nil {
		return err
	}

	enforcer = instance
	logger.Scene("casbin").Info("Casbin 初始化成功")
	return nil
}

// Close 关闭 Casbin 组件并清理运行时状态。
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	enforcer = nil
	return nil
}

// Ready 检查 Casbin 组件是否已完成初始化。
func Ready() error {
	if GetEnforcer() == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	return nil
}

func initCasbin(db *gorm.DB) (*casbinlib.Enforcer, error) {
	a, err := NewAdapter(db)
	if err != nil {
		return nil, fmt.Errorf("创建 Casbin 适配器失败: %w", err)
	}

	m, err := model.NewModelFromString(rbacModel)
	if err != nil {
		return nil, fmt.Errorf("创建 Casbin 模型失败: %w", err)
	}

	instance, err := casbinlib.NewEnforcer(m, a)
	if err != nil {
		return nil, fmt.Errorf("创建 Casbin Enforcer 失败: %w", err)
	}

	if err := instance.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("加载 Casbin 策略失败: %w", err)
	}

	rebuildURLCodeMap(instance)
	return instance, nil
}

// rebuildURLCodeMap 从 Casbin p 策略中重建 URL+Method → code 内存映射。
// 遍历所有 p 规则，提取 v1（URL）、v2（Method）、v3（code）建立映射。
func rebuildURLCodeMap(e *casbinlib.Enforcer) {
	urlCodeMap = sync.Map{}

	policies, err := e.GetPolicy()
	if err != nil {
		logger.Scene("casbin").Error(err, "Casbin 获取策略失败")
		return
	}

	for _, rule := range policies {
		// p 规则格式：[sub, obj, act, code]
		if len(rule) < 4 {
			continue
		}
		url := rule[1]    // obj = URL
		method := rule[2] // act = Method
		code := rule[3]   // code
		if code == "" {
			continue
		}
		urlCodeMap.Store(urlCodeKey(url, method), code)
	}
	count := 0
	urlCodeMap.Range(func(_, _ interface{}) bool { count++; return true })
	logger.Scene("casbin").With("count", count).Info("Casbin URL→code 映射已加载")
}

// ============================
// 集中式策略变更 facade
// ============================

// ReloadPolicy 重新加载内存策略并重建 URL→code 映射。
// 任何 p/g/g2 写操作后必须调用，否则变更不生效。
func ReloadPolicy() error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if err := e.LoadPolicy(); err != nil {
		return fmt.Errorf("加载 Casbin 策略失败: %w", err)
	}
	rebuildURLCodeMap(e)
	return nil
}

// HasPermissionPolicies 检查权限编码是否已分配给角色或用户。
func HasPermissionPolicies(code string) (bool, error) {
	e := GetEnforcer()
	if e == nil {
		return false, fmt.Errorf("casbin 未初始化")
	}
	if code == "" {
		return false, fmt.Errorf("权限编码不能为空")
	}

	rules, err := e.GetFilteredPolicy(3, code)
	if err != nil {
		return false, fmt.Errorf("查询权限策略失败: %w", err)
	}
	return len(rules) > 0, nil
}

// ReplacePermissionDefinition 保留授权主体并替换权限的请求路径和方法。
func ReplacePermissionDefinition(code, path, method string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if code == "" || path == "" || method == "" {
		return fmt.Errorf("权限编码、请求路径和方法不能为空")
	}

	policyMu.Lock()
	defer policyMu.Unlock()

	rules, err := e.GetFilteredPolicy(3, code)
	if err != nil {
		return fmt.Errorf("查询权限策略失败: %w", err)
	}
	if len(rules) == 0 {
		return nil
	}

	oldRules := make([][]string, len(rules))
	for i, rule := range rules {
		oldRules[i] = append([]string(nil), rule...)
	}

	if _, err = e.RemoveFilteredPolicy(3, code); err != nil {
		return fmt.Errorf("删除权限旧策略失败: %w", err)
	}
	for _, rule := range oldRules {
		if len(rule) < 4 {
			continue
		}
		if _, err = e.AddPolicy(rule[0], path, strings.ToUpper(method), code); err != nil {
			return restorePermissionPolicies(e, code, oldRules, err)
		}
	}

	return ReloadPolicy()
}

func restorePermissionPolicies(e *casbinlib.Enforcer, code string, rules [][]string, cause error) error {
	if _, err := e.RemoveFilteredPolicy(3, code); err != nil {
		logger.Scene("casbin").Error(err, "权限策略替换后恢复失败")
	}
	for _, rule := range rules {
		if len(rule) < 4 {
			continue
		}
		if _, err := e.AddPolicy(rule[0], rule[1], rule[2], rule[3]); err != nil {
			logger.Scene("casbin").Error(err, "权限策略替换后恢复失败")
		}
	}
	if err := ReloadPolicy(); err != nil {
		return fmt.Errorf("替换权限策略失败: %v；恢复策略失败: %w", cause, err)
	}
	return fmt.Errorf("替换权限策略失败: %w", cause)
}

// ReplaceRolePermissions 全量替换角色拥有的权限策略。
// 删除该 role_code 在 p 中所有记录，然后按 codes 反查到的 path/method 重新写入。
//
// 参数：
//   - roleCode：角色编码
//   - policies：要写入的 [path, method, code] 三元组列表
//
// 调用方负责在事务中收集 codes 并转换为 policies。
// 本函数内部不做事务编排，只负责 Casbin 层面的增删 + 刷新。
func ReplaceRolePermissions(roleCode string, policies [][3]string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	// 删除该角色所有旧 p 策略（按 sub=roleCode 过滤）
	if _, err := e.RemoveFilteredPolicy(0, roleCode); err != nil {
		return fmt.Errorf("删除角色旧权限失败: %w", err)
	}

	// 写入新权限
	for _, p := range policies {
		if _, err := e.AddPolicy(roleCode, p[0], p[1], p[2]); err != nil {
			return fmt.Errorf("添加角色权限失败: %w", err)
		}
	}

	return ReloadPolicy()
}

// ReplaceUserRoleBindings 全量替换某用户绑定的角色列表。
// 删除该用户所有 g 记录，然后重新写入。
//
// 参数：
//   - userID：用户 ID（字符串形式）
//   - roleCodes：角色编码列表
func ReplaceUserRoleBindings(userID string, roleCodes []string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if userID == "" {
		return fmt.Errorf("用户 ID 不能为空")
	}

	// 删除该用户所有旧 g 策略（按 sub=userID 过滤）
	if _, err := e.RemoveFilteredGroupingPolicy(0, userID); err != nil {
		return fmt.Errorf("删除用户旧角色绑定失败: %w", err)
	}

	// 写入新角色绑定
	for _, code := range roleCodes {
		if code == "" {
			continue
		}
		if _, err := e.AddGroupingPolicy(userID, code); err != nil {
			return fmt.Errorf("添加用户角色绑定失败: %w", err)
		}
	}

	return ReloadPolicy()
}

// ReplaceRoleUsers 全量替换某角色下的用户列表。
// 删除该角色所有 g 记录（按 obj=roleCode 过滤），然后重新写入。
//
// 参数：
//   - roleCode：角色编码
//   - userIDs：用户 ID 列表（字符串形式）
func ReplaceRoleUsers(roleCode string, userIDs []string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	// 删除该角色所有旧 g 策略（按 obj=roleCode 过滤，即第 1 个字段）
	if _, err := e.RemoveFilteredGroupingPolicy(1, roleCode); err != nil {
		return fmt.Errorf("删除角色旧用户绑定失败: %w", err)
	}

	// 写入新用户绑定
	for _, uid := range userIDs {
		if uid == "" {
			continue
		}
		if _, err := e.AddGroupingPolicy(uid, roleCode); err != nil {
			return fmt.Errorf("添加角色用户绑定失败: %w", err)
		}
	}

	return ReloadPolicy()
}

// ReplaceUserPermissions 全量替换用户的直接额外权限。
// 只操作 p, user_id, ...（不碰角色 p），删除后重新写入。
//
// 参数：
//   - userID：用户 ID（字符串形式）
//   - policies：要写入的 [path, method, code] 三元组列表
func ReplaceUserPermissions(userID string, policies [][3]string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if userID == "" {
		return fmt.Errorf("用户 ID 不能为空")
	}

	// 删除该用户所有直接 p 策略（按 sub=userID 过滤）
	if _, err := e.RemoveFilteredPolicy(0, userID); err != nil {
		return fmt.Errorf("删除用户直接权限失败: %w", err)
	}

	// 写入新权限
	for _, p := range policies {
		if _, err := e.AddPolicy(userID, p[0], p[1], p[2]); err != nil {
			return fmt.Errorf("添加用户直接权限失败: %w", err)
		}
	}

	return ReloadPolicy()
}

// ActivateRole 启用角色：写入 g2, roleCode, active。
// 如果已存在则幂等返回。
func ActivateRole(roleCode string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	exists, err := e.HasNamedGroupingPolicy("g2", roleCode, "active")
	if err != nil {
		return fmt.Errorf("检查角色启用状态失败: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := e.AddNamedGroupingPolicy("g2", roleCode, "active"); err != nil {
		return fmt.Errorf("启用角色失败: %w", err)
	}
	return ReloadPolicy()
}

// DeactivateRole 禁用角色：删除 g2, roleCode, active。
// 不删除 p/g 关联，重新启用时恢复 ActivateRole 即可。
func DeactivateRole(roleCode string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	if _, err := e.RemoveNamedGroupingPolicy("g2", roleCode, "active"); err != nil {
		return fmt.Errorf("禁用角色失败: %w", err)
	}
	return ReloadPolicy()
}

// DeleteRoleAllPolicies 删除角色关联的全部策略。
// 在删除角色时调用，事务内清理：
//   - 该角色的全部 p 策略（角色拥有的权限）
//   - 所有用户与该角色的 g 关联（用户角色绑定）
//   - 该角色的 g2 启用状态
//
// 调用方负责删除 sys_role 记录，本函数只处理 Casbin 层面。
func DeleteRoleAllPolicies(roleCode string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	// 删除该角色所有 p 策略
	if _, err := e.RemoveFilteredPolicy(0, roleCode); err != nil {
		return fmt.Errorf("删除角色权限策略失败: %w", err)
	}

	// 删除所有用户与该角色的 g 关联（按 obj=roleCode 过滤）
	if _, err := e.RemoveFilteredGroupingPolicy(1, roleCode); err != nil {
		return fmt.Errorf("删除角色用户关联失败: %w", err)
	}

	// 删除该角色的 g2 启用状态
	if _, err := e.RemoveNamedGroupingPolicy("g2", roleCode, "active"); err != nil {
		return fmt.Errorf("删除角色启用状态失败: %w", err)
	}

	return ReloadPolicy()
}

// DeleteUserAllPolicies 删除用户的直接权限和角色绑定。
func DeleteUserAllPolicies(userID string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if userID == "" {
		return fmt.Errorf("用户 ID 不能为空")
	}

	policyMu.Lock()
	defer policyMu.Unlock()

	if _, err := e.RemoveFilteredPolicy(0, userID); err != nil {
		return fmt.Errorf("删除用户直接权限失败: %w", err)
	}
	if _, err := e.RemoveFilteredGroupingPolicy(0, userID); err != nil {
		return fmt.Errorf("删除用户角色绑定失败: %w", err)
	}
	return ReloadPolicy()
}

// GetRoleCodesByUserID 查询用户绑定的角色编码列表。
func GetRoleCodesByUserID(userID string) ([]string, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	return e.GetRolesForUser(userID)
}

// GetUserIDsByRoleCode 查询角色下的用户 ID 列表。
func GetUserIDsByRoleCode(roleCode string) ([]string, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	return e.GetUsersForRole(roleCode)
}

// GetRolePermissions 获取角色的权限列表。
// 返回 [path, method, code] 三元组。
func GetRolePermissions(roleCode string) ([][3]string, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	rules, err := e.GetFilteredPolicy(0, roleCode)
	if err != nil {
		return nil, fmt.Errorf("查询角色权限失败: %w", err)
	}

	result := make([][3]string, 0, len(rules))
	for _, rule := range rules {
		if len(rule) < 4 {
			continue
		}
		result = append(result, [3]string{rule[1], rule[2], rule[3]})
	}
	return result, nil
}

// GetUserDirectPermissions 获取用户直接额外权限。
// 返回 [path, method, code] 三元组。
func GetUserDirectPermissions(userID string) ([][3]string, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	rules, err := e.GetFilteredPolicy(0, userID)
	if err != nil {
		return nil, fmt.Errorf("查询用户直接权限失败: %w", err)
	}

	result := make([][3]string, 0, len(rules))
	for _, rule := range rules {
		if len(rule) < 4 {
			continue
		}
		result = append(result, [3]string{rule[1], rule[2], rule[3]})
	}
	return result, nil
}
