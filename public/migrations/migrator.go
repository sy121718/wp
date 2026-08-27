// Package migrations 提供数据库表结构迁移和种子数据管理。
//
// 迁移按 Version 排序依次执行；每个 Migration 通过 CheckSQL 检查目标是否已存在，
// 存在则跳过，不存在则按 ";" 分割逐条执行 SQL。
package migrations

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"gorm.io/gorm"
)

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

// All 返回按版本号排序的全部迁移。
func All() []Migration {
	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].Version < allMigrations[j].Version
	})
	return allMigrations
}

// AllSeeds 返回按版本号排序的全部种子数据。
func AllSeeds() []Seed {
	sort.Slice(allSeeds, func(i, j int) bool {
		return allSeeds[i].Version < allSeeds[j].Version
	})
	return allSeeds
}

// Run 依次执行全部迁移。
func Run(db *gorm.DB) error {
	for _, m := range All() {
		if err := apply(db, m); err != nil {
			return fmt.Errorf("迁移 %s (%s) 失败: %w", m.Version, m.TableName, err)
		}
	}
	return nil
}

func apply(db *gorm.DB, m Migration) error {
	checkSQL := m.CheckSQL
	if checkSQL == "" {
		checkSQL = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?"
	}

	var count int64
	if err := db.Raw(checkSQL, m.TableName).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		log.Printf("跳过 %s (%s)：迁移对象已存在", m.Version, m.TableName)
		return nil
	}

	parts := SplitStatements(m.SQL)
	for _, part := range parts {
		if part == "" {
			continue
		}
		if err := db.Exec(part).Error; err != nil {
			return fmt.Errorf("执行 %s (%s) 失败: %w", m.Version, m.TableName, err)
		}
	}

	log.Printf("完成 %s (%s)", m.Version, m.TableName)
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
		log.Printf("跳过种子 %s (%s)：数据已存在", s.Version, s.TableName)
		return nil
	}

	if err := db.Exec(s.SQL).Error; err != nil {
		return fmt.Errorf("执行种子 %s (%s) 失败: %w", s.Version, s.TableName, err)
	}

	log.Printf("完成种子 %s (%s)", s.Version, s.TableName)
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
