package builtin

import (
	"reflect"
	"testing"
)

func TestResolveDeptIDsWithoutInjection(t *testing.T) {
	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 10); got != nil {
		t.Fatalf("未注入子树解析器时应返回 nil，实际 %v", got)
	}
}

func TestSetDataRuleDeptResolver(t *testing.T) {
	SetDataRuleDeptResolver(func(deptID uint64) []uint64 { return []uint64{deptID, deptID + 10} })
	t.Cleanup(func() {

		dataRuleSubtreeResolver.Store(nil)
	})

	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 5); !reflect.DeepEqual(got, []uint64{5, 15}) {
		t.Fatalf("子树解析结果不符: %v", got)
	}

	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 0); got != nil {
		t.Fatalf("部门 id 为 0 时不应解析，实际 %v", got)
	}

	SetDataRuleDeptResolver(nil)
	if got := resolveDeptIDs(&dataRuleSubtreeResolver, 5); !reflect.DeepEqual(got, []uint64{5, 15}) {
		t.Fatalf("nil 解析器不应覆盖已有注入，实际 %v", got)
	}
}
