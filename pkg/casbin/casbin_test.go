package casbin

// 纯函数单元测试：策略行 ↔ 策略数组转换 + URL/Method 映射 key。
// 覆盖自研 Adapter 的核心转换逻辑（不依赖数据库/enforcer），
// 锁死「尾部空字段裁剪」与「缺失字段留空」的往返语义，防止策略持久化漂移。

import "testing"

func TestURLCoderKeyNormalizesMethod(t *testing.T) {
	if got := urlCodeKey("/api/page/list", "get"); got != "/api/page/list||GET" {
		t.Fatalf("urlCodeKey 大小写归一失败: %q", got)
	}
	if got := urlCodeKey("/api/page/create", "POST"); got != "/api/page/create||POST" {
		t.Fatalf("urlCodeKey 拼接失败: %q", got)
	}
}

func TestRuleToPolicyArrayTrimsTrailingEmpty(t *testing.T) {
	a := &Adapter{}
	cases := []struct {
		line CasbinRule
		want []string
	}{
		{CasbinRule{Ptype: "p", V0: "u1", V1: "/api/x", V2: "GET", V3: "code"}, []string{"p", "u1", "/api/x", "GET", "code"}},
		{CasbinRule{Ptype: "g", V0: "u1", V1: "role"}, []string{"g", "u1", "role"}},
		{CasbinRule{Ptype: "p", V0: "u1"}, []string{"p", "u1"}},
	}
	for _, c := range cases {
		got := a.ruleToPolicyArray(c.line)
		if len(got) != len(c.want) {
			t.Fatalf("ruleToPolicyArray 长度不符: got %v want %v", got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("ruleToPolicyArray[%d] 不符: got %v want %v", i, got, c.want)
			}
		}
	}
}

func TestSavePolicyLineFillsMissingEmpty(t *testing.T) {
	a := &Adapter{}
	got := a.savePolicyLine("p", []string{"u1", "/api/x", "GET", "code"})
	if got.Ptype != "p" || got.V0 != "u1" || got.V1 != "/api/x" || got.V2 != "GET" || got.V3 != "code" {
		t.Fatalf("savePolicyLine 组装失败: %+v", got)
	}
	if got.V4 != "" || got.V5 != "" {
		t.Fatalf("savePolicyLine 缺失字段应留空: %+v", got)
	}
	// 往返：savePolicyLine → ruleToPolicyArray 应还原。
	round := a.ruleToPolicyArray(got)
	want := []string{"p", "u1", "/api/x", "GET", "code"}
	if len(round) != len(want) {
		t.Fatalf("往返长度不符: %v", round)
	}
	for i := range round {
		if round[i] != want[i] {
			t.Fatalf("往返[%d] 不符: %v", i, round)
		}
	}
}
