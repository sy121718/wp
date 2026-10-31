package pageservice

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
