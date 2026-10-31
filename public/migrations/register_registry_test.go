package migrations

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

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

var migrationSlotWhitelist = map[string]string{}

var seedSlotWhitelist = map[string]string{}

func TestMigrationVersionSlotsUnique(t *testing.T) {
	versions := make([]string, 0, len(allMigrations))
	for _, m := range allMigrations {
		versions = append(versions, m.Version)
	}
	checkVersionSlots(t, "迁移", versions, migrationSlotWhitelist)
}

func TestSeedVersionSlotsUnique(t *testing.T) {
	versions := make([]string, 0, len(allSeeds))
	for _, s := range allSeeds {
		versions = append(versions, s.Version)
	}
	checkVersionSlots(t, "种子", versions, seedSlotWhitelist)
}

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
