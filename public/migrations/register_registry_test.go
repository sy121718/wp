package migrations

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// retainedSQLArchives记录两分支实际运行过的同义历史文件，不作为第二次执行登记。
// 固定双方原文hash，任何改写或正式登记丢失都失败，不能放过任意未注册SQL。
var retainedSQLArchives = map[string]struct {
	canonical       string
	archiveSHA256   string
	canonicalSHA256 string
}{
	"679_blocks_version_preset.sql": {
		"676_blocks_version_preset.sql",
		"884ea197e39ccfe724da045879e758a84a02340ba02ed1ba659fcd8e2ed66cd0",
		"6f1f1718de491e575cfc5ffd5da8dcced9fec1dc2c93d6a4e3fac174b006d66f",
	},
	"680_i18n_block_version_conflict.sql": {
		"677_i18n_block_version_conflict.sql",
		"a6a625ab05ca0c4c5958a855d15cc72696dacf2adec61de010df3d8a42795de5",
		"f1db1f4cb33dd64d08a1805099fbcd94eb41cf745e7c730fa06763248cafb788",
	},
}

// TestEmbeddedSQLFilesAllRegistered保证每个SQL有唯一执行登记或精确历史归档。
// 正式登记与历史归档分开计数，不伪造执行过679/680。
func TestEmbeddedSQLFilesAllRegistered(t *testing.T) {
	entries, err := fs.ReadDir(migrationSQLFS, ".")
	if err != nil {
		t.Fatalf("读取嵌入的 SQL 目录失败: %v", err)
	}
	onDisk := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			onDisk[e.Name()] = struct{}{}
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("嵌入的 SQL 列表为空，//go:embed *.sql 可能已失效")
	}
	for name := range onDisk {
		if _, archived := retainedSQLArchives[name]; archived {
			continue
		}
		if _, ok := registeredSQLFiles[name]; !ok {
			t.Errorf("迁移 SQL %s 没有正式登记或精确历史归档", name)
		}
	}
	for name := range registeredSQLFiles {
		if _, ok := onDisk[name]; !ok {
			t.Errorf("注册台账引用了 %s，但嵌入文件系统里没有这个文件", name)
		}
	}
	for name, archive := range retainedSQLArchives {
		if _, executed := registeredSQLFiles[name]; executed {
			t.Errorf("历史归档%s不得再次登记执行，唯一正式文件为%s", name, archive.canonical)
		}
		if _, registered := registeredSQLFiles[archive.canonical]; !registered {
			t.Errorf("历史归档%s对应的正式文件%s没有登记", name, archive.canonical)
		}
		for file, want := range map[string]string{name: archive.archiveSHA256, archive.canonical: archive.canonicalSHA256} {
			raw, readErr := migrationSQLFS.ReadFile(file)
			if readErr != nil {
				t.Errorf("迁移原文%s不可读: %v", file, readErr)
				continue
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
				t.Errorf("迁移历史原文%s被改写: hash=%s want=%s", file, got, want)
			}
		}
	}
	if len(onDisk) != len(registeredSQLFiles)+len(retainedSQLArchives) {
		t.Errorf("磁盘 SQL 数 %d != 正式登记%d + 精确历史归档%d", len(onDisk), len(registeredSQLFiles), len(retainedSQLArchives))
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
