package builtin

// datarule_test.go —— datarule 中间件的部门子树解析器注入点。
//
// 改造前中间件每次请求都要为「我能看哪些部门（本部门及子部门）」再查一次库；改造后由 admin 在
// 装配期注入一个解析器（读**同一份**数据权限部门快照），中间件只调它。
//
// **方向**：这里解析的是**向下**的子树，不是向上的祖先链 —— 祖先是「我隶属于哪些上级部门」，
// 用于匹配分配给上级部门的规则（那段匹配在 admin 的规则快照内部完成）。
// 两个方向搞反的后果是把可见范围放大到上级部门（越权），所以字段名与注释都写死方向。
//
// 覆盖两条边界语义：
//   - 未注入 → 返回 nil：消费方（引擎）据此回退到既有子查询，行为与改造前一致；
//   - 已注入 → 返回解析结果；部门 id 为 0 时不调用解析器。
//
// 中间件主流程（会话 + Casbin + UserContext 填充）依赖 Redis 与 Casbin，不在本包单测覆盖范围，
// 由 public/test 的链路用例与 admin 侧的快照用例共同保证。

import (
	"reflect"
	"testing"
)

// TestResolveDeptIDsWithoutInjection 未注入解析器时返回 nil（回退路径）。
// 本用例必须在任何注入用例之前执行 —— 同文件内 Go 按声明顺序运行测试。
func TestResolveDeptIDsWithoutInjection(t *testing.T) {
	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 10); got != nil {
		t.Fatalf("未注入子树解析器时应返回 nil，实际 %v", got)
	}
}

// TestSetDataRuleDeptResolver 注入后按部门 id 解析子树；部门 id 为 0 或传 nil 不改变行为。
func TestSetDataRuleDeptResolver(t *testing.T) {
	SetDataRuleDeptResolver(func(deptID uint64) []uint64 { return []uint64{deptID, deptID + 10} })
	t.Cleanup(func() {
		// 还原为「未注入」，避免影响同包其它用例（包级 atomic 变量是共享状态）。
		dataRuleSubtreeResolver.Store(nil)
	})

	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 5); !reflect.DeepEqual(got, []uint64{5, 15}) {
		t.Fatalf("子树解析结果不符: %v", got)
	}
	// 部门 id 为 0（未分配部门）不调用解析器。
	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 0); got != nil {
		t.Fatalf("部门 id 为 0 时不应解析，实际 %v", got)
	}
	// 传 nil 解析器不覆盖已有注入（装配期只注入一次，重复注入不该把能力清掉）。
	SetDataRuleDeptResolver(nil)
	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 5); !reflect.DeepEqual(got, []uint64{5, 15}) {
		t.Fatalf("nil 解析器不应覆盖已有注入，实际 %v", got)
	}
}
