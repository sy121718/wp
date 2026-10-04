// Package migrations 提供数据库表结构迁移和种子数据管理。
//
// 迁移按 Version 排序依次执行；每个 Migration 通过 CheckSQL 检查目标是否已存在，
// 存在则跳过，不存在则按 ";" 分割逐条执行 SQL。
package migrations

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// migrationAdvisoryLockExpr 多实例启动时迁移互斥锁的键（PostgreSQL advisory lock）。
//
// 键按 database + current_schema() 派生，而不是写死常量：生产多实例「同库同 schema」
// 仍然互斥（原意不变），而测试为每个包建独立隔离 schema —— 固定键会让上百个测试包的
// 迁移完全串行排队（实测并发两个 schema 跑迁移 2.49s，单个只需 1.08s，等于没并行）。
const migrationAdvisoryLockExpr = `hashtextextended(current_database() || ':' || current_schema() || ':go_wp_migrations', 0)`

// Migration 描述一次表结构迁移。
type Migration struct {
	Version   string // 版本号，按字符串排序决定执行顺序
	TableName string // 目标表名，用于幂等检查
	CheckSQL  string // 自定义存在性检查 SQL；为空时使用默认检查
	SQL       string // 建表/变更语句，按 ";" 分割逐条执行
}

// Seed 描述一批种子数据。
type Seed struct {
	Version      string
	TableName    string
	ConditionSQL string // 存在性检查，返回 > 0 则跳过
	SQL          string
}

var allMigrations []Migration
var allSeeds []Seed

func register(m Migration) {
	allMigrations = append(allMigrations, m)
}

func registerSeed(s Seed) {
	allSeeds = append(allSeeds, s)
}

// compareVersion 按「主编号 → 后缀 → 整串」排序：
//
//  1. 主编号按**数值**比较 —— 字符串比较会让 "1000_bar" 排在 "999_foo" 前面，
//     迁移一旦跨过 99x 就开始乱序执行；
//  2. 同主编号下按后缀比较（空串在前）：086 < 086a < 086b < 087 < 087b < 088；
//  3. 主编号与后缀都相同才退到整串比较（保证顺序稳定、可复现）。
func compareVersion(a, b string) bool {
	na, sa, ea := parseVersionPrefix(a)
	nb, sb, eb := parseVersionPrefix(b)
	if ea != nil {
		panic(ea)
	}
	if eb != nil {
		panic(eb)
	}
	if na != nb {
		return na < nb
	}
	if sa != sb {
		return sa < sb
	}
	return a < b
}

// parseVersionPrefix 解析迁移版本前缀：数字 + 可选小写字母后缀 + 分隔符。
//
// 合法形状：
//
//	001-init                      → 1, ""
//	086a-product-attribute-perms  → 86, "a"
//	087b-product-variant-options  → 87, "b"
//
// 字母后缀是**有语义的补丁位**（「086 之后的第一个补丁」），必须合法：仓库里三个
// 存量版本号就是这么写的（086a / 086b / 087b），改名字会让已执行过的库与新代码
// 记的版本对不上，排查时两边的历史对不起来。
func parseVersionPrefix(version string) (major int, suffix string, err error) {
	v := strings.TrimSpace(version)
	if v == "" {
		return 0, "", fmt.Errorf("迁移版本不能为空")
	}
	i := 0
	for i < len(v) && unicode.IsDigit(rune(v[i])) {
		i++
	}
	if i == 0 {
		return 0, "", fmt.Errorf("迁移版本 %q 格式不合法（应为 001-name）", version)
	}
	// 可选后缀：紧跟数字的连续小写字母。
	j := i
	for j < len(v) && unicode.IsLower(rune(v[j])) {
		j++
	}
	suffix = v[i:j]
	if j >= len(v) || (v[j] != '_' && v[j] != '-') {
		return 0, "", fmt.Errorf("迁移版本 %q 格式不合法（应为 001-name 或 001a-name）", version)
	}
	major, perr := strconv.Atoi(v[:i])
	if perr != nil {
		return 0, "", fmt.Errorf("迁移版本 %q 前缀不是整数: %w", version, perr)
	}
	return major, suffix, nil
}

// All 返回按版本号排序的全部迁移。
//
// 用稳定排序：sort.Slice 在版本号重复时的相对顺序属实现细节，稳定排序固定为
// 注册顺序，使同一组注册在任何构建上都排出同一序列。ValidateRegistry 已拒绝
// 重复版本号，这里是第二道保险 —— 只消除不确定性，不新增启动期失败路径。
func All() []Migration {
	sort.SliceStable(allMigrations, func(i, j int) bool {
		return compareVersion(allMigrations[i].Version, allMigrations[j].Version)
	})
	return allMigrations
}

// AllSeeds 返回按版本号排序的全部种子数据。稳定排序的理由同 All：
// 种子版本号此前没有重复校验（见 TestSeedVersionsUnique），稳定排序保证
// 「万一重复」时顺序仍由注册顺序确定，而不是随构建漂移。
func AllSeeds() []Seed {
	sort.SliceStable(allSeeds, func(i, j int) bool {
		return compareVersion(allSeeds[i].Version, allSeeds[j].Version)
	})
	return allSeeds
}

// Run 依次执行全部迁移。
func Run(db *gorm.DB) error {
	if err := ValidateRegistry(); err != nil {
		return err
	}
	if db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return runAll(db)
	}
	// 迁移必须固定在**同一个连接**上跑：advisory lock 是会话级的，而 db.Raw/db.Exec
	// 每次都从连接池取连接 —— 换连接会让 unlock 落到别的连接上（锁永不释放，几十个
	// 测试进程一起泄漏就把锁表撑爆，报 out of shared memory），也会让锁根本保护不到
	// 真正的迁移语句。db.Connection 提供单连接会话。
	return db.Connection(func(conn *gorm.DB) error {
		if err := acquireMigrationAdvisoryLock(conn); err != nil {
			return fmt.Errorf("获取迁移锁失败: %w", err)
		}
		defer releaseMigrationAdvisoryLock(conn)
		// 共享扩展 schema（ext_shared，pg_trgm 装在这里）未必在调用方的 search_path 里：
		// 测试自己的连接、插件迁移、以及任何非 pgtest 路径都可能直接开连接跑迁移。
		// 167/169/173 要用 gin_trgm_ops 建索引，缺了它整条迁移会报 operator class does not exist。
		// 这里统一补上（schema 尚未创建时该名字在 search_path 里无害，创建后自动生效）。
		if err := conn.Exec("SELECT set_config('search_path', current_setting('search_path') || ',ext_shared', false)").Error; err != nil {
			return fmt.Errorf("设置迁移 search_path 失败: %w", err)
		}
		return runAll(conn)
	})
}

// runAll 按版本顺序执行全部迁移。
func runAll(db *gorm.DB) error {
	for _, m := range All() {
		if err := apply(db, m); err != nil {
			return fmt.Errorf("迁移 %s (%s) 失败: %w", m.Version, m.TableName, err)
		}
	}
	return nil
}

func acquireMigrationAdvisoryLock(db *gorm.DB) error {
	const maxWait = 10 * time.Minute
	deadline := time.Now().Add(maxWait)
	for {
		var locked bool
		if err := db.Raw("SELECT pg_try_advisory_lock(" + migrationAdvisoryLockExpr + ")").Scan(&locked).Error; err != nil {
			return err
		}
		if locked {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待迁移锁超时（%s）", maxWait)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func releaseMigrationAdvisoryLock(db *gorm.DB) {
	var unlocked bool
	if err := db.Raw("SELECT pg_advisory_unlock(" + migrationAdvisoryLockExpr + ")").Scan(&unlocked).Error; err != nil {
		logger.Scene("init").Error(err, "释放迁移锁失败")
	}
}

// ValidateRegistry 在真正连接数据库前检查迁移版本是否重复或为空。
// 版本重复会让排序结果依赖注册顺序，升级时可能出现同一环境执行顺序不一致。
func ValidateRegistry() error {
	seen := make(map[string]string, len(allMigrations))
	for _, m := range allMigrations {
		v := strings.TrimSpace(m.Version)
		if _, _, err := parseVersionPrefix(v); err != nil {
			return fmt.Errorf("迁移 %s（表 %s）: %w", v, m.TableName, err)
		}
		if prev, ok := seen[v]; ok {
			return fmt.Errorf("迁移版本重复 %q：%s 与 %s", v, prev, m.TableName)
		}
		seen[v] = m.TableName
	}
	return nil
}

const defaultMigrationCheckSQL = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?"

func apply(db *gorm.DB, m Migration) error {
	// CheckSQL 空值的兜底放在 ApplyStatements 内（导出函数必须自带兜底，见那里注释）。
	return ApplyStatements(db, m.Version, m.TableName, m.CheckSQL, SplitStatements(m.SQL))
}

// ApplyStatements 在**单个事务**内执行一条迁移：先在事务里做存在性检查，再逐条执行语句。
//
// 为什么必须同一个事务（审计 R-01）：
//
//	① 一条迁移的多条语句要么全生效、要么全不生效。此前是逐条 db.Exec 且没有外层事务：
//	   中途失败会留下半成品对象，而重跑时存在性检查只看**主对象**是否已建 → 打印
//	   「迁移对象已存在，跳过」直接返回 nil，剩下的 DDL 永远不再执行，且没有任何告警。
//	   全仓 381 个迁移里 262 个是多语句，触发面不是边角。
//	② 检查与执行同事务还消掉了 TOCTOU：检查通过之后被并发会话插入同名对象的情形不再可能。
//
// 为什么可以整体事务化：PostgreSQL 的 DDL 是事务性的，而本仓库的迁移**不含**不可事务语句
// （无 CREATE INDEX CONCURRENTLY / DROP INDEX CONCURRENTLY / ALTER TYPE … ADD VALUE /
// REINDEX / CLUSTER / VACUUM —— 由 public/test/architecture 的迁移语句扫描用例守着，
// 命中即红）。因此不需要「逐条退出的开关」：一旦出现不可事务语句，正确做法是改那条迁移，
// 而不是给引擎开后门。
//
// 导出是给外部测试包用的（public/migrations 的用例在 migrations_test 包里，而 apply 依赖
// 注册表）：用例要断言的是「失败不留半成品」这条性质，不需要动注册表 —— 与既有
// SplitStatements / Fingerprint 的导出口径一致。
func ApplyStatements(db *gorm.DB, version, tableName, checkSQL string, parts []string) error {
	// 空 CheckSQL = 用默认的「主对象是否已建」检查。这里必须自带兜底：
	// 空 SQL 交给 GORM 会得到一句 `SELECT * FROM ` + nil 参数，报
	// "unsupported data type: <nil>" —— 那是调用者的入参错误伪装成数据库故障。
	if strings.TrimSpace(checkSQL) == "" {
		checkSQL = defaultMigrationCheckSQL
	}
	executed := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var count int64
		// 自定义 CheckSQL 不一定用 ? 占位符（例如按 pg_constraint / pg_indexes 检查对象是否已建），
		// 而无条件传参会让这类检查直接报 "expected 0 arguments, got 1"，迁移永远跑不起来。
		// 只在 SQL 里真的出现占位符时才传表名。
		check := tx.Raw(checkSQL)
		if strings.Contains(checkSQL, "?") {
			check = tx.Raw(checkSQL, tableName)
		}
		if err := check.Scan(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		for _, part := range parts {
			if part == "" {
				continue
			}
			if err := tx.Exec(part).Error; err != nil {
				return fmt.Errorf("执行 %s (%s) 失败: %w", version, tableName, err)
			}
		}
		executed = true
		return nil
	})
	if err != nil {
		return err
	}

	scene := logger.Scene("init").With("version", version).With("table", tableName)
	if executed {
		scene.Info("迁移完成")
	} else {
		scene.Info("迁移对象已存在，跳过")
	}
	return nil
}

// RunSeeds 依次执行全部种子数据。
func RunSeeds(db *gorm.DB) error {
	for _, s := range AllSeeds() {
		if err := applySeed(db, s); err != nil {
			return fmt.Errorf("种子数据 %s (%s) 失败: %w", s.Version, s.TableName, err)
		}
	}
	return nil
}

func applySeed(db *gorm.DB, s Seed) error {
	var count int64
	if err := db.Raw(s.ConditionSQL).Scan(&count).Error; err != nil {
		return fmt.Errorf("检查种子数据 %s (%s) 失败: %w", s.Version, s.TableName, err)
	}
	if count > 0 {
		logger.Scene("init").With("version", s.Version).With("table", s.TableName).Info("种子数据已存在，跳过")
		return nil
	}

	if err := db.Exec(s.SQL).Error; err != nil {
		return fmt.Errorf("执行种子 %s (%s) 失败: %w", s.Version, s.TableName, err)
	}

	logger.Scene("init").With("version", s.Version).With("table", s.TableName).Info("种子数据完成")
	return nil
}

// SplitStatements 按语句边界拆分 SQL 文本。
//
// 语义：
//   - 尊重 PostgreSQL 美元引用块（$$...$$，如 DO 块），块内分号不构成语句边界；
//   - 跳过 -- 行注释与 '...' 字符串字面量内的分号；
//   - 丢弃空语句；返回的语句不带结尾分号。
func SplitStatements(sqlText string) []string {
	var stmts []string
	var current strings.Builder

	appendCurrent := func() {
		s := strings.TrimSpace(current.String())
		s = strings.TrimSuffix(s, ";")
		if s != "" {
			stmts = append(stmts, s)
		}
		current.Reset()
	}

	i := 0
	for i < len(sqlText) {
		rest := sqlText[i:]
		switch {
		case strings.HasPrefix(rest, "$$"):
			end := strings.Index(rest[2:], "$$")
			if end < 0 {
				current.WriteString(rest)
				i = len(sqlText)
				continue
			}
			current.WriteString(rest[:2+end+2])
			i += 2 + end + 2
		case strings.HasPrefix(rest, "--"):
			nl := strings.IndexByte(rest, '\n')
			if nl < 0 {
				current.WriteString(rest)
				i = len(sqlText)
				continue
			}
			current.WriteString(rest[:nl])
			i += nl
		case rest[0] == '\'':
			end := strings.IndexByte(rest[1:], '\'')
			if end < 0 {
				current.WriteString(rest)
				i = len(sqlText)
				continue
			}
			current.WriteString(rest[:end+2])
			i += end + 2
		default:
			ch := rest[0]
			current.WriteByte(ch)
			i++
			if ch == ';' {
				appendCurrent()
			}
		}
	}
	appendCurrent()
	return stmts
}
