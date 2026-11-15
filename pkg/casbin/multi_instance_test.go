package casbin

import (
	"testing"

	casbinlib "github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
)

// memoryAdapter 模拟 sys_casbin_rule：策略源（数据库表）是共享的，
// 每个 Enforcer 实例只持有自己加载时的内存副本。
type memoryAdapter struct {
	rules [][]string
}

func (a *memoryAdapter) LoadPolicy(m model.Model) error {
	for _, rule := range a.rules {
		line := "p"
		for _, field := range rule {
			line += ", " + field
		}
		if err := persist.LoadPolicyLine(line, m); err != nil {
			return err
		}
	}
	return nil
}

func (a *memoryAdapter) SavePolicy(m model.Model) error {
	a.rules = nil
	for ptype, ast := range m["p"] {
		for _, rule := range ast.Policy {
			line := make([]string, 0, len(rule)+1)
			line = append(line, ptype)
			line = append(line, rule...)
			a.rules = append(a.rules, line)
		}
	}
	return nil
}

func (a *memoryAdapter) AddPolicy(_ string, _ string, rule []string) error {
	a.rules = append(a.rules, append([]string(nil), rule...))
	return nil
}

func (a *memoryAdapter) RemovePolicy(_ string, _ string, rule []string) error {
	for i, existing := range a.rules {
		if equalRule(existing, rule) {
			a.rules = append(a.rules[:i], a.rules[i+1:]...)
			return nil
		}
	}
	return nil
}

func (a *memoryAdapter) RemoveFilteredPolicy(_ string, _ string, fieldIndex int, fieldValues ...string) error {
	kept := a.rules[:0]
	for _, existing := range a.rules {
		match := true
		for i, value := range fieldValues {
			if value == "" {
				continue
			}
			if fieldIndex+i >= len(existing) || existing[fieldIndex+i] != value {
				match = false
				break
			}
		}
		if !match {
			kept = append(kept, existing)
		}
	}
	a.rules = kept
	return nil
}

func equalRule(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPolicyMutationDoesNotPropagateToOtherInstance 固化多实例部署下的策略可见性：
// 写路径（ReplaceRolePermissions 等）只重建**本进程**的内存 enforcer 与 URL→code 映射，
// 另一实例在自己的 ReloadPolicy 之前看不到新策略。这不是待修缺陷，而是当前设计的事实；
// 若将来接入 Watcher/TTL 自动刷新，这条用例会变红，提醒同步更新部署说明
// （多实例要么滚动重启，要么显式调用 ReloadPolicy）。
func TestPolicyMutationDoesNotPropagateToOtherInstance(t *testing.T) {
	source := &memoryAdapter{rules: [][]string{
		{"role_base", "/admin/known", "GET", "perm.known"},
	}}

	newEnforcer := func(t *testing.T) *casbinlib.SyncedEnforcer {
		t.Helper()
		m, err := model.NewModelFromString(rbacModel)
		if err != nil {
			t.Fatalf("构建模型失败: %v", err)
		}
		instance, err := casbinlib.NewSyncedEnforcer(m, source)
		if err != nil {
			t.Fatalf("构建 Enforcer 失败: %v", err)
		}
		if err := instance.LoadPolicy(); err != nil {
			t.Fatalf("加载策略失败: %v", err)
		}
		return instance
	}

	writer := newEnforcer(t) // 进程 A
	reader := newEnforcer(t) // 进程 B

	if ok, err := writer.Enforce("role_base", "/admin/known", "GET"); err != nil || !ok {
		t.Fatalf("前置条件不成立：A 应能看到既有策略 err=%v ok=%v", err, ok)
	}

	if _, err := writer.AddPolicy("role_new", "/admin/probe", "GET", "perm.probe"); err != nil {
		t.Fatalf("写入策略失败: %v", err)
	}
	if ok, err := writer.Enforce("role_new", "/admin/probe", "GET"); err != nil || !ok {
		t.Fatalf("A 写后本进程应立即生效 err=%v ok=%v", err, ok)
	}

	// 关键断言：B 只依赖自己的内存副本，不做重载就看不到。
	ok, err := reader.Enforce("role_new", "/admin/probe", "GET")
	if err != nil {
		t.Fatalf("B 判定失败: %v", err)
	}
	if ok {
		t.Fatal("B 未重载却看到新策略：说明存在跨实例传播机制，需同步部署文档与缓存失效设计")
	}

	// 恢复路径：显式重载（滚动重启等价）。
	if err := reader.LoadPolicy(); err != nil {
		t.Fatalf("B 重载失败: %v", err)
	}
	if ok, err := reader.Enforce("role_new", "/admin/probe", "GET"); err != nil || !ok {
		t.Fatalf("B 重载后应看到新策略 err=%v ok=%v", err, ok)
	}
}
