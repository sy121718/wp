package migrations_test

// seed_lookup_test.go — 「按版本号取种子」的公共小工具。
//
// 为什么需要它：437 / 441 两个「种子不覆盖运营改动」的回归测试，断言的是**全量重放种子后**
// 的终值。那两条键的终值由比它们更晚的收口迁移决定（522 把联系人空态改指页头
// 「导入联系人」抽屉、526 重写无筛选空态描述）。如果只是把期望值改成现实值，测试就退化成
// 一面跟着现实走的镜子 —— 谁把 522 / 526 删掉或改坏，它照样绿。
//
// 所以那些用例在断言终值之前，先用这里取到「终值出处」的种子，显式确认它的判据
// 确实是「已完成」。判据与终值两头都钉住，才叫护栏。

import (
	"testing"

	"go_wp/public/migrations"
)

// findSeedByVersion 返回指定版本号的种子；未注册时直接判该用例失败。
func findSeedByVersion(t *testing.T, version string) migrations.Seed {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == version {
			return seed
		}
	}
	t.Fatalf("种子 %s 未注册；本用例的终值由它收口", version)
	return migrations.Seed{}
}
