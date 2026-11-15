// Package schema 是数据库结构的唯一来源：一表一文件，外加 base.sql 的全局对象。
//
// 表文件用 -- @phase pre|ddl|constr|idx|fk|data 分段。载入顺序固定为：
// base.sql → 全部 pre（序列）→ 全部 ddl（建表/触发器/策略/挂分区）→ 全部 constr（主键与唯一约束）
// → 全部 idx（索引与索引挂载）→ 全部 fk（外键）→ 全部 comment（对象注释）
// → 按 order.txt 装载系统数据。
// 这个段序复现 pg_dump 的依赖顺序，不能合并：分区子表先建主键再挂载会撞 multiple primary keys。
//
// 载入是原子的：空库一次事务内建完并写入 sys_baseline 水位；已有水位且摘要一致时跳过；
// 摘要不一致直接报错，不在已有结构上重放 DDL（历史结构无法可靠回退）。
package schema

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/sqlsplit"
)

//go:embed base.sql defaults.sql order.txt tables/*.sql
var assets embed.FS

const watermarkTable = "sys_baseline"

// Digest 返回 schema 产物的内容摘要（文件路径 + 内容，按表名排序）。
func Digest() string {
	h := sha256.New()
	files := append([]string{"base.sql", "defaults.sql"}, tableFiles()...)
	files = append(files, "order.txt")
	for _, name := range files {
		data, err := assets.ReadFile(name)
		if err != nil {
			panic("schema 产物缺失：" + name + ": " + err.Error())
		}
		fmt.Fprintf(h, "%d:%s\n", len(name), name)
		h.Write(data)
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Version 是写进 sys_baseline.version 的版本标签（不含日期，跨日稳定）。
func Version() string { return "schema-" + Digest()[:8] }

func tableFiles() []string {
	entries, err := assets.ReadDir("tables")
	if err != nil {
		panic("schema/tables 缺失：" + err.Error())
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, "tables/"+e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TableNames 返回表文件名（不含目录与 .sql 后缀），按字母序。
func TableNames() []string {
	files := tableFiles()
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(f, "tables/"), ".sql"))
	}
	return out
}

// Apply 在空库上建结构并装载系统数据；已有同摘要水位时跳过。
func Apply(db *gorm.DB) error {
	if db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return fmt.Errorf("schema.Apply 只支持 PostgreSQL")
	}
	return db.Connection(func(conn *gorm.DB) error {
		if err := acquireLock(conn); err != nil {
			return fmt.Errorf("获取 schema 载入锁失败: %w", err)
		}
		defer releaseLock(conn)

		if err := conn.Exec("SELECT set_config('search_path', current_setting('search_path') || ',ext_shared', false)").Error; err != nil {
			return fmt.Errorf("设置 schema 载入 search_path 失败: %w", err)
		}

		skip, err := watermarkMatches(conn)
		if err != nil {
			return err
		}
		if skip {
			return applyDefaults(conn)
		}

		return conn.Transaction(func(tx *gorm.DB) error {
			if err := execSection(tx, mustRead("base.sql")); err != nil {
				return fmt.Errorf("schema base.sql 失败: %w", err)
			}
			for _, phase := range []string{"pre", "ddl", "constr", "idx", "fk", "comment"} {
				for _, name := range tableFiles() {
					section := sectionOf(mustRead(name), phase)
					if section == "" {
						continue
					}
					if err := execSection(tx, section); err != nil {
						return fmt.Errorf("schema %s [%s] 失败: %w", name, phase, err)
					}
				}
			}
			// 系统数据是终态快照：装载期关掉用户触发器，避免插入再触发一遍业务写入
			// （例如 sys_menus 会写 sys_menu_permission，既抢主键又空推序列）。
			// 触发器在数据装载完成后立即恢复（SET LOCAL 随事务结束）。
			if err := tx.Exec("SET LOCAL session_replication_role = replica").Error; err != nil {
				return fmt.Errorf("关闭触发器装载系统数据失败: %w", err)
			}
			for _, table := range order() {
				section := sectionOf(mustRead("tables/"+table+".sql"), "data")
				if section == "" {
					continue
				}
				if err := execSection(tx, section); err != nil {
					return fmt.Errorf("schema 系统数据 %s 失败: %w", table, err)
				}
			}
			if err := tx.Exec("INSERT INTO sys_baseline (version, manifest_sha256, note) VALUES (?, ?, ?)",
				Version(), Digest(), "由 public/schema 载入").Error; err != nil {
				return err
			}
			return execSection(tx, mustRead("defaults.sql"))
		})
	})
}

// applyDefaults 为已有工程补齐默认内容模板；同摘要跳过结构时也要执行。
func applyDefaults(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		return execSection(tx, mustRead("defaults.sql"))
	})
}

func mustRead(name string) string {
	data, err := assets.ReadFile(name)
	if err != nil {
		panic("schema 产物缺失：" + name + ": " + err.Error())
	}
	return string(data)
}

// sectionOf 取出某个 -- @phase 段的正文；行首的标记行本身不计入。
func sectionOf(file, phase string) string {
	marker := "-- @phase " + phase
	var b strings.Builder
	active := false
	for _, line := range strings.Split(file, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "-- @phase ") {
			active = t == marker
			continue
		}
		if active {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func order() []string {
	var out []string
	for _, line := range strings.Split(mustRead("order.txt"), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func execSection(tx *gorm.DB, text string) error {
	for _, stmt := range sqlsplit.Split(text) {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// watermarkMatches 判定是否已有同摘要水位；已有对象但没有同摘要水位时拒绝继续。
func watermarkMatches(db *gorm.DB) (bool, error) {
	var exists bool
	if err := db.Raw("SELECT to_regclass('public.sys_baseline') IS NOT NULL").Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("无法确认 sys_baseline 是否存在: %w", err)
	}
	if !exists {
		var others int64
		if err := db.Raw("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace " +
			"WHERE n.nspname = 'public' AND c.relkind IN ('r','p')").Scan(&others).Error; err != nil {
			return false, fmt.Errorf("统计既有表失败: %w", err)
		}
		if others > 0 {
			return false, fmt.Errorf("库中已有 %d 张表但没有 sys_baseline 水位：请先用 scripts/schema-adopt.sh 核验并写入水位，不能直接重放结构", others)
		}
		return false, nil
	}

	var rows []struct {
		Version        string
		ManifestSHA256 string
	}
	if err := db.Raw("SELECT version, manifest_sha256 FROM sys_baseline ORDER BY id DESC LIMIT 1").Scan(&rows).Error; err != nil {
		return false, fmt.Errorf("读取 sys_baseline 水位失败: %w", err)
	}
	if len(rows) == 0 {
		var others int64
		if err := db.Raw("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace " +
			"WHERE n.nspname = 'public' AND c.relkind IN ('r','p') AND c.relname <> 'sys_baseline'").Scan(&others).Error; err != nil {
			return false, fmt.Errorf("统计既有表失败: %w", err)
		}
		if others > 0 {
			return false, fmt.Errorf("库中已有 %d 张表但没有 schema 水位：请先用 scripts/schema-adopt.sh 核验并写入水位，不能直接重放结构", others)
		}
		return false, nil
	}
	if rows[0].ManifestSHA256 == Digest() {
		return true, nil
	}
	return false, fmt.Errorf("schema 产物已变更：库登记 %s，仓库产物 %s。历史结构不能在已有库上重放；请核对差异后执行 scripts/schema-adopt.sh --db <库> --refresh --apply", rows[0].Version, Version())
}

func acquireLock(db *gorm.DB) error {
	return db.Exec("SELECT pg_advisory_lock(hashtextextended(current_database() || ':go_wp_schema', 0))").Error
}

func releaseLock(db *gorm.DB) {
	_ = db.Exec("SELECT pg_advisory_unlock(hashtextextended(current_database() || ':go_wp_schema', 0))").Error
}
