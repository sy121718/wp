package pageservice

// rebuild_batch_capacity_test.go — 批容量常量的对账（契约公开值 vs service 实现上限）。
//
// 为什么必须有这条：装配层（menu_stale 执行器）按 pagecontract.RebuildBatchCapacity 分块，
// 一旦它与 service 内部的 maxAutoRebuildPages 分叉，两块之间就出现一个静默的坏区 ——
// 容量公开值比实现大 → 一块超过实现上限 → 触发 overflow 入队 + ErrRebuildOverloaded，
// 任务记 Failed 且没有下一轮触发源；比实现小 → 白白多分块（不致命，但也说明有人改了数）。
// 两个数必须相等，且这条用例不依赖数据库。

import (
	"testing"

	pagecontract "go_wp/internal/module/page/contract"
)

func TestRebuildBatchCapacityMatchesImplementation(t *testing.T) {
	if got, want := pagecontract.RebuildBatchCapacity, maxAutoRebuildPages; got != want {
		t.Fatalf("契约公开的批容量 %d 与实现上限 %d 不一致：menu_stale 执行器按契约分块，"+
			"契约偏大就会每块都触发 overflow 入队与 ErrRebuildOverloaded（任务 Failed 且无下轮触发源）", got, want)
	}
	if pagecontract.RebuildBatchCapacity <= 0 {
		t.Fatalf("批容量必须是正数，实际 %d", pagecontract.RebuildBatchCapacity)
	}
}
