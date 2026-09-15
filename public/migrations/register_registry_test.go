package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

// TestEmbeddedSQLFilesAllRegistered 保证磁盘上的 .sql 与注册台账**双向**一一对应。
//
// 单向比对只能抓住一半问题：只查「磁盘 → 台账」抓得住「新增 SQL 忘了注册」，
// 只查「台账 → 磁盘」才抓得住「登记了但文件被删」。
//
// 为什么必须有这个测试：迁移未注册不会报错，程序照常启动，只是那段 DDL 永远不执行。
// 本仓库真实发生过（195 未注册，导致一批测试在缺列上失败）。
func TestEmbeddedSQLFilesAllRegistered(t *testing.T) {
	entries, err := fs.ReadDir(migrationSQLFS, ".")
	if err != nil {
		t.Fatalf("读取嵌入的 SQL 目录失败: %v", err)
	}
	onDisk := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		onDisk[e.Name()] = struct{}{}
	}
	if len(onDisk) == 0 {
		t.Fatal("嵌入的 SQL 列表为空，//go:embed *.sql 可能已失效")
	}

	for name := range onDisk {
		if _, ok := registeredSQLFiles[name]; !ok {
			t.Errorf("迁移 SQL %s 未被任何 register/registerSeed 引用（新增了 SQL 但忘了注册，该迁移永远不会执行）", name)
		}
	}
	for name := range registeredSQLFiles {
		if _, ok := onDisk[name]; !ok {
			t.Errorf("注册台账引用了 %s，但嵌入文件系统里没有这个文件", name)
		}
	}
	if len(onDisk) != len(registeredSQLFiles) {
		t.Errorf("磁盘 SQL 数 %d != 注册台账数 %d", len(onDisk), len(registeredSQLFiles))
	}
}

// TestSeedVersionsUnique 保证种子版本号不重复。
//
// AllSeeds 按版本号排序决定执行顺序；版本号重复时顺序会退化成「取决于注册顺序」。
// migrator.ValidateRegistry 只校验 allMigrations，种子的这一半由本测试兜住。
func TestSeedVersionsUnique(t *testing.T) {
	seen := make(map[string]string, len(allSeeds))
	for _, s := range allSeeds {
		v := strings.TrimSpace(s.Version)
		if prev, ok := seen[v]; ok {
			t.Errorf("种子版本重复 %q：%s 与 %s", v, prev, s.TableName)
		}
		seen[v] = s.TableName
	}
}
