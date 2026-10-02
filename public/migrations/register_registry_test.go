package migrations

import (
	"io/fs"
	"strconv"
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

// migrationSlotWhitelist / seedSlotWhitelist 是「同主编号 + 同补丁位」的例外清单，
// 逐条写理由（清单条目不再命中即失败，禁止只增不减）。
//
// 合法形态是**同号 + 不同补丁位**（086 / 086a / 086b）—— 那正是补丁位存在的用途；
// 本测试拦的是「两个都是同号且都没补丁位」。
//
// **当前两项都是空的**：本仓库历史上攒下的三对同槽（155 的 presentation 双注册、
// 199 的 rls/webhook、228 的两个 i18n seed）已分别补成 155b / 199b / 228b，
// 排序逐项不变（见各注册处的注释）。从此新增同槽必须当场补位，而不是先挂进白名单。
var migrationSlotWhitelist = map[string]string{}

var seedSlotWhitelist = map[string]string{}

// TestMigrationVersionSlotsUnique 保证注册迁移的「(数字主编号, 字母补丁位)」唯一。
//
// 为什么必须有这一条：migrator.ValidateRegistry 只拒**重复的版本字符串**，
// 主编号撞车它一律放行。而 compareVersion 在同主编号、同后缀时会退到**整串字典序** ——
// 顺序仍然确定，但「按编号定位迁移」会命中错文件，读代码的人也无法判断谁先谁后
// （本仓库真的攒了这样的对：199 与 155）。
func TestMigrationVersionSlotsUnique(t *testing.T) {
	versions := make([]string, 0, len(allMigrations))
	for _, m := range allMigrations {
		versions = append(versions, m.Version)
	}
	checkVersionSlots(t, "迁移", versions, migrationSlotWhitelist)
}

// TestSeedVersionSlotsUnique 同一条判据管种子那一半（AllSeeds 同样按版本号排序）。
func TestSeedVersionSlotsUnique(t *testing.T) {
	versions := make([]string, 0, len(allSeeds))
	for _, s := range allSeeds {
		versions = append(versions, s.Version)
	}
	checkVersionSlots(t, "种子", versions, seedSlotWhitelist)
}

// checkVersionSlots 按 parseVersionPrefix 的真实语义分组，而不是按字符串前缀切分
// —— 判据必须与排序实现同源，否则测试绿而顺序照样退化。
func checkVersionSlots(t *testing.T, kind string, versions []string, whitelist map[string]string) {
	t.Helper()
	type slot struct {
		major  int
		suffix string
		items  []string
	}
	slots := make(map[string]*slot, len(versions))
	for _, raw := range versions {
		v := strings.TrimSpace(raw)
		major, suffix, err := parseVersionPrefix(v)
		if err != nil {
			t.Errorf("%s版本 %q 解析失败: %v", kind, v, err)
			continue
		}
		key := strconv.Itoa(major) + "|" + suffix
		if slots[key] == nil {
			slots[key] = &slot{major: major, suffix: suffix}
		}
		slots[key].items = append(slots[key].items, v)
	}
	hit := make(map[string]bool, len(whitelist))
	for _, s := range slots {
		if len(s.items) < 2 {
			continue
		}
		majorKey := strconv.Itoa(s.major)
		if reason, ok := whitelist[majorKey]; ok {
			hit[majorKey] = true
			t.Logf("%s主编号 %s 的同号例外（%s）：%v", kind, majorKey, reason, s.items)
			continue
		}
		t.Errorf("%s主编号 %s 被 %d 个版本共用（补丁位都是 %q）：%v —— 同号会让排序退化成整串字典序、"+
			"按编号定位会命中错文件；请改用 086a/086b 这样的补丁位，或在白名单里登记理由",
			kind, majorKey, len(s.items), s.suffix, s.items)
	}
	for majorKey, reason := range whitelist {
		if !hit[majorKey] {
			t.Errorf("白名单里的%s主编号 %s 已经不再冲突（理由：%s）—— 请删掉这条豁免", kind, majorKey, reason)
		}
	}
}
