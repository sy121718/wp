package facing_test

// fact_test.go — 业务错误形态判据（FactOf）。
//
// 这个判据决定了一条错误**要不要对外展示原文**：命中业务判据 → 出口按 key 取词给用户；
// 未命中 → 归口成「系统内部错误」+ 只进日志。判错的代价是双向的：
//   - 漏判（业务错误当内部错误）→ 用户看到 500，而真因是可行动的一句说明；
//   - 误判（内部错误当业务错误）→ 把 SQL / 路径原样发给客户端。

import (
	"fmt"
	"testing"

	"go_wp/pkg/facing"
)

// TestFactOfAcceptsFullWidthColon 全角冒号也是合法的「key：明细」分隔符。
//
// 本仓 service 侧写复合错误的既有形态就是全角冒号
//（`fmt.Errorf("%s：%s", enums.ErrXxx, detail)`，见 product_err_detail.go 与
// order 的退货数量超限）。只认 ASCII `:` 时这类错误会被当成内部错误 ——
// 用户看到 500「系统内部错误」，而真因是"退货数量超过可退数量"。
func TestFactOfAcceptsFullWidthColon(t *testing.T) {
	const key = "order.err.returnQuantityExceeded"
	err := fmt.Errorf("%s：%s", key, "退货重复行商品")

	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("全角冒号的复合错误应按业务错误处理，实际 ok=%v business=%v（err=%v）",
			ok, fact.Business, err)
	}
	if fact.Key != key {
		t.Errorf("key 应为 %q，实际 %q", key, fact.Key)
	}
	if fact.Tail != "退货重复行商品" {
		t.Errorf("明细应为「退货重复行商品」，实际 %q（全角冒号是 3 字节，切错会带残留字节）",
			fact.Tail)
	}
}

// TestFactOfHalfWidthColonStillWorks 半角冒号的既有形态不受影响。
func TestFactOfHalfWidthColonStillWorks(t *testing.T) {
	const key = "order.err.returnQuantityExceeded"
	fact, ok := facing.FactOf(fmt.Errorf("%s: %s", key, "detail"))
	if !ok || fact.Key != key || fact.Tail != "detail" {
		t.Fatalf("半角冒号形态应仍然成立，实际 ok=%v fact=%+v", ok, fact)
	}
}

// TestFactOfSeparatorIsTheFirstOne 分隔符取**最先出现**的那个。
//
// 写侧形态是 `key` 紧跟分隔符、明细在后，所以第一个分隔符才是边界。
// 明细内部可能还有冒号（如 URL），取错会把 key 切成一段无意义的前缀。
func TestFactOfSeparatorIsTheFirstOne(t *testing.T) {
	const key = "order.err.returnQuantityExceeded"
	fact, ok := facing.FactOf(fmt.Errorf("%s：见 https://example.test/a:b", key))
	if !ok || fact.Key != key {
		t.Fatalf("应认出 key，实际 ok=%v key=%q", ok, fact.Key)
	}
	if fact.Tail != "见 https://example.test/a:b" {
		t.Errorf("明细应完整保留（含内部冒号），实际 %q", fact.Tail)
	}
}

// TestFactOfSingleKeyIsBusiness 单段 key 本身就是业务错误（不带明细）。
func TestFactOfSingleKeyIsBusiness(t *testing.T) {
	fact, ok := facing.FactOf(facing.New("ErrSessionNotFound", "会话不存在"))
	if !ok || !fact.Business || fact.Key != "ErrSessionNotFound" {
		t.Fatalf("单段 key 应命中业务判据，实际 ok=%v fact=%+v", ok, fact)
	}
}

// TestFactOfPlainErrorIsNotBusiness 没有 key 形态的错误不是业务错误。
//
// 这条守的是泄漏方向：SQL 原文、文件路径都不该被当成业务文案发给客户端。
//
// 注意判据是**普通 error**（fmt.Errorf / 驱动错误），不是 facing.New：
// facing.New 声明的是「这是我自己写的对外文案」（它实现 Source，① 优先命中），
// 那是代码在声明处的显式选择，不是形态推断。真正的泄漏面是有人把
// `fmt.Errorf("pq: …")` 直接返给出口 —— 那正是本用例覆盖的形态。
func TestFactOfPlainErrorIsNotBusiness(t *testing.T) {
	for _, raw := range []string{
		`pq: relation "sys_config" does not exist`,
		`open /var/lib/data/x.db: permission denied`,
	} {
		if fact, ok := facing.FactOf(fmt.Errorf("%s", raw)); ok && fact.Business {
			t.Errorf("%q 不该被判成业务错误（会原样上屏）", raw)
		}
	}
}
