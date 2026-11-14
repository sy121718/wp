package migrations

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

const migrationAdvisoryLockExpr = `hashtextextended(current_database() || ':' || current_schema() || ':go_wp_migrations', 0)`

type Migration struct {
	Version   string
	TableName string
	CheckSQL  string
	SQL       string

	Func func(*gorm.DB) error
}

type Seed struct {
	Version      string
	TableName    string
	ConditionSQL string
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

func compareVersionCmp(a, b string) int {
	switch {
	case compareVersion(a, b):
		return -1
	case compareVersion(b, a):
		return 1
	default:
		return 0
	}
}

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

func All() []Migration {
	slices.SortStableFunc(allMigrations, func(a, b Migration) int {
		return compareVersionCmp(a.Version, b.Version)
	})
	return allMigrations
}

func AllSeeds() []Seed {
	slices.SortStableFunc(allSeeds, func(a, b Seed) int {
		return compareVersionCmp(a.Version, b.Version)
	})
	return allSeeds
}

func Run(db *gorm.DB) error {
	if err := ValidateRegistry(); err != nil {
		return err
	}
	if db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return runAll(db)
	}

	return db.Connection(func(conn *gorm.DB) error {
		if err := acquireMigrationAdvisoryLock(conn); err != nil {
			return fmt.Errorf("获取迁移锁失败: %w", err)
		}
		defer releaseMigrationAdvisoryLock(conn)

		if err := conn.Exec("SELECT set_config('search_path', current_setting('search_path') || ',ext_shared', false)").Error; err != nil {
			return fmt.Errorf("设置迁移 search_path 失败: %w", err)
		}
		return runAll(conn)
	})
}

func runAll(db *gorm.DB) error {
	skip := baselineSkipSet(db)
	for _, m := range All() {
		if skip[m.Version] {
			logger.Scene("init").With("version", m.Version).With("table", m.TableName).Info("基线已覆盖，跳过")
			continue
		}
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
	if m.Func != nil {
		if strings.TrimSpace(m.SQL) != "" {

			return fmt.Errorf("迁移 %s 同时声明了 SQL 与 Func：只能二选一", m.Version)
		}
		return ApplyFunc(db, m.Version, m.TableName, m.CheckSQL, m.Func)
	}

	return ApplyStatements(db, m.Version, m.TableName, m.CheckSQL, SplitStatements(m.SQL))
}

func ApplyFunc(db *gorm.DB, version, tableName, checkSQL string, fn func(*gorm.DB) error) error {
	if fn == nil {
		return fmt.Errorf("迁移 %s 的 Func 为 nil", version)
	}
	if strings.TrimSpace(checkSQL) == "" {
		checkSQL = defaultMigrationCheckSQL
	}
	executed := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var count int64
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
		if err := fn(tx); err != nil {
			return fmt.Errorf("执行 %s (%s) 失败: %w", version, tableName, err)
		}
		executed = true
		return nil
	})
	if err != nil {
		return err
	}
	scene := logger.Scene("init").With("version", version).With("table", tableName)
	if executed {
		scene.Info("迁移完成（Go 回调）")
	} else {
		scene.Info("迁移对象已存在，跳过")
	}
	return nil
}

func ApplyStatements(db *gorm.DB, version, tableName, checkSQL string, parts []string) error {

	if strings.TrimSpace(checkSQL) == "" {
		checkSQL = defaultMigrationCheckSQL
	}
	executed := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var count int64

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
