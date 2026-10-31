package casbin

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"go_wp/pkg/database"
	"go_wp/pkg/logger"

	casbinlib "github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

var (
	enforcer *casbinlib.SyncedEnforcer
	mu       sync.RWMutex
	policyMu sync.Mutex

	urlCodeMap atomic.Pointer[sync.Map]
)

func urlCodeKey(url, method string) string {
	return url + "||" + strings.ToUpper(method)
}

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

func GetEnforcer() *casbinlib.SyncedEnforcer {
	mu.RLock()
	defer mu.RUnlock()
	return enforcer
}

func GetCodeByURL(url, method string) (string, bool) {
	m := urlCodeMap.Load()
	if m == nil {
		return "", false
	}
	val, ok := m.Load(urlCodeKey(url, method))
	if !ok {
		return "", false
	}
	return val.(string), true
}

func Init(_ *viper.Viper) error {
	db, err := database.GetDB()
	if err != nil {
		return fmt.Errorf("获取数据库实例失败: %w", err)
	}
	return InitCasbin(db)
}

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

func Close() error {
	mu.Lock()
	defer mu.Unlock()

	enforcer = nil
	return nil
}

func Ready() error {
	if GetEnforcer() == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	return nil
}

func initCasbin(db *gorm.DB) (*casbinlib.SyncedEnforcer, error) {
	a, err := NewAdapter(db)
	if err != nil {
		return nil, fmt.Errorf("创建 Casbin 适配器失败: %w", err)
	}

	m, err := model.NewModelFromString(rbacModel)
	if err != nil {
		return nil, fmt.Errorf("创建 Casbin 模型失败: %w", err)
	}

	instance, err := casbinlib.NewSyncedEnforcer(m, a)
	if err != nil {
		return nil, fmt.Errorf("创建 Casbin Enforcer 失败: %w", err)
	}

	if err := instance.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("加载 Casbin 策略失败: %w", err)
	}

	rebuildURLCodeMap(instance)
	return instance, nil
}

func rebuildURLCodeMap(e *casbinlib.SyncedEnforcer) {
	nm := &sync.Map{}

	policies, err := e.GetPolicy()
	if err != nil {
		logger.Scene("casbin").Error(err, "Casbin 获取策略失败")
		return
	}

	count := 0
	for _, rule := range policies {

		if len(rule) < 4 {
			continue
		}
		url := rule[1]
		method := rule[2]
		code := rule[3]
		if code == "" {
			continue
		}
		nm.Store(urlCodeKey(url, method), code)
		count++
	}
	urlCodeMap.Store(nm)
	logger.Scene("casbin").With("count", count).Info("Casbin URL→code 映射已加载")
}

func ReloadPolicy() error {
	policyMu.Lock()
	defer policyMu.Unlock()
	return reloadPolicyLocked()
}

func reloadPolicyLocked() error {
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

func ReplaceRolePermissions(roleCode string, policies [][3]string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	policyMu.Lock()
	defer policyMu.Unlock()

	want := make(map[string]struct{}, len(policies))
	for _, p := range policies {
		want[p[0]+"\x00"+p[1]+"\x00"+p[2]] = struct{}{}
	}
	for _, p := range policies {
		if _, err := e.AddPolicy(roleCode, p[0], p[1], p[2]); err != nil {
			return fmt.Errorf("添加角色权限失败: %w", err)
		}
	}
	existing, err := e.GetFilteredPolicy(0, roleCode)
	if err != nil {
		return fmt.Errorf("查询角色现有权限失败: %w", err)
	}
	for _, rule := range existing {
		if len(rule) < 4 {
			continue
		}
		if _, keep := want[rule[1]+"\x00"+rule[2]+"\x00"+rule[3]]; keep {
			continue
		}
		if _, err := e.RemovePolicy(rule[0], rule[1], rule[2], rule[3]); err != nil {
			return fmt.Errorf("删除角色旧权限失败: %w", err)
		}
	}

	return reloadPolicyLocked()
}

func ReplaceUserRoleBindings(userID string, roleCodes []string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if userID == "" {
		return fmt.Errorf("用户 ID 不能为空")
	}

	policyMu.Lock()
	defer policyMu.Unlock()

	want := make(map[string]struct{}, len(roleCodes))
	for _, code := range roleCodes {
		if code == "" {
			continue
		}
		want[code] = struct{}{}
	}
	for code := range want {
		if _, err := e.AddGroupingPolicy(userID, code); err != nil {
			return fmt.Errorf("添加用户角色绑定失败: %w", err)
		}
	}
	existing, err := e.GetFilteredGroupingPolicy(0, userID)
	if err != nil {
		return fmt.Errorf("查询用户现有角色绑定失败: %w", err)
	}
	for _, rule := range existing {
		if len(rule) < 2 {
			continue
		}
		if _, keep := want[rule[1]]; keep {
			continue
		}
		if _, err := e.RemoveGroupingPolicy(rule[0], rule[1]); err != nil {
			return fmt.Errorf("删除用户旧角色绑定失败: %w", err)
		}
	}

	return reloadPolicyLocked()
}

func ReplaceRoleUsers(roleCode string, userIDs []string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	policyMu.Lock()
	defer policyMu.Unlock()

	want := make(map[string]struct{}, len(userIDs))
	for _, uid := range userIDs {
		if uid == "" {
			continue
		}
		want[uid] = struct{}{}
	}
	for uid := range want {
		if _, err := e.AddGroupingPolicy(uid, roleCode); err != nil {
			return fmt.Errorf("添加角色用户绑定失败: %w", err)
		}
	}
	existing, err := e.GetFilteredGroupingPolicy(1, roleCode)
	if err != nil {
		return fmt.Errorf("查询角色现有用户绑定失败: %w", err)
	}
	for _, rule := range existing {
		if len(rule) < 2 {
			continue
		}
		if _, keep := want[rule[0]]; keep {
			continue
		}
		if _, err := e.RemoveGroupingPolicy(rule[0], rule[1]); err != nil {
			return fmt.Errorf("删除角色旧用户绑定失败: %w", err)
		}
	}

	return reloadPolicyLocked()
}

func ReplaceUserPermissions(userID string, policies [][3]string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if userID == "" {
		return fmt.Errorf("用户 ID 不能为空")
	}

	policyMu.Lock()
	defer policyMu.Unlock()

	want := make(map[string]struct{}, len(policies))
	for _, p := range policies {
		want[p[0]+"\x00"+p[1]+"\x00"+p[2]] = struct{}{}
	}
	for _, p := range policies {
		if _, err := e.AddPolicy(userID, p[0], p[1], p[2]); err != nil {
			return fmt.Errorf("添加用户直接权限失败: %w", err)
		}
	}
	existing, err := e.GetFilteredPolicy(0, userID)
	if err != nil {
		return fmt.Errorf("查询用户现有直接权限失败: %w", err)
	}
	for _, rule := range existing {
		if len(rule) < 4 {
			continue
		}
		if _, keep := want[rule[1]+"\x00"+rule[2]+"\x00"+rule[3]]; keep {
			continue
		}
		if _, err := e.RemovePolicy(rule[0], rule[1], rule[2], rule[3]); err != nil {
			return fmt.Errorf("删除用户旧直接权限失败: %w", err)
		}
	}

	return reloadPolicyLocked()
}

func DeleteRoleAllPolicies(roleCode string) error {
	e := GetEnforcer()
	if e == nil {
		return fmt.Errorf("casbin 未初始化")
	}
	if roleCode == "" {
		return fmt.Errorf("角色编码不能为空")
	}

	policyMu.Lock()
	defer policyMu.Unlock()

	if _, err := e.RemoveFilteredPolicy(0, roleCode); err != nil {
		return fmt.Errorf("删除角色权限策略失败: %w", err)
	}

	if _, err := e.RemoveFilteredGroupingPolicy(1, roleCode); err != nil {
		return fmt.Errorf("删除角色用户关联失败: %w", err)
	}

	if _, err := e.RemoveNamedGroupingPolicy("g2", roleCode, "active"); err != nil {
		return fmt.Errorf("删除角色启用状态失败: %w", err)
	}

	return reloadPolicyLocked()
}

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
	return reloadPolicyLocked()
}

func GetRoleCodesByUserID(userID string) ([]string, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	return e.GetRolesForUser(userID)
}

func GetUserIDsByRoleCode(roleCode string) ([]string, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	return e.GetUsersForRole(roleCode)
}

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
