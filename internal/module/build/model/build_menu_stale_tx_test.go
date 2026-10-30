package buildmodel

// build_menu_stale_tx_test.go — 事务内入队入口的**形状守卫**（不碰数据库）。
//
// CreateTx / NextMenuStaleVersionTx 的第一条判据都是「句柄真的在事务里」。
// 这两条用例用 nil / 非事务句柄驱动，实现若先访问数据库会直接 panic ——
// 它们钉住的正是「校验在碰库之前」这一点。真实事务语义见
// public/test/build/feature/build_menu_stale_test.go（真实 PG）。

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestCreateTxRequiresTransaction 非事务句柄被一律拒绝。
func TestCreateTxRequiresTransaction(t *testing.T) {
	m := NewModel(nil)
	for _, tx := range []*gorm.DB{nil, &gorm.DB{}, {}} {
		err := m.CreateTx(tx, &Entity{SourceType: SourceTypeMenuStale, SourceID: "x"})
		if !errors.Is(err, ErrTxRequired) {
			t.Fatalf("非事务句柄 %#v 应被拒绝，实际 %v", tx, err)
		}
	}
}

// TestNextMenuStaleVersionTxRequiresTransaction 取号同样只接受真实事务。
func TestNextMenuStaleVersionTxRequiresTransaction(t *testing.T) {
	m := NewModel(nil)
	for _, tx := range []*gorm.DB{nil, &gorm.DB{}} {
		if _, err := m.NextMenuStaleVersionTx(tx, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", "header"); !errors.Is(err, ErrTxRequired) {
			t.Fatalf("非事务句柄 %#v 应被拒绝，实际 %v", tx, err)
		}
	}
}

// TestEntityHasMenuStaleColumns 实体必须同时带 input_version 与 context_key，且都不得 NOT NULL。
//
// 为什么在 model 的单测里钉它：两列都是 698 新增，漏掉任何一列都会让 menu_stale 的形状在
// 库里缺一段 —— 少了 context_key，uq_build_jobs_context_version 的键里就永远是 NULL，
// PG 的 UNIQUE 允许多个 NULL，于是「同一版本只排一次」静默失效（队列被同一份工作填满，
// 而且没有任何报错）；少了 input_version，版本号落不下去，A→B→A 又被去重成一条。
// 标签里多一个 not null 则会让 page / presentation 的既有入队当场失败。
func TestEntityHasMenuStaleColumns(t *testing.T) {
	for _, c := range []struct {
		field string
		typ   reflect.Type
	}{
		{"InputVersion", reflect.TypeOf((*int64)(nil))},
		{"ContextKey", reflect.TypeOf((*string)(nil))},
	} {
		sf, ok := reflect.TypeOf(Entity{}).FieldByName(c.field)
		if !ok || sf.Type != c.typ {
			t.Fatalf("Entity.%s 必须是指针类型 %v（menu_stale 之外的来源保持 NULL）", c.field, c.typ)
		}
		column := strings.ToLower(strings.ReplaceAll(c.field, "ContextKey", "context_key"))
		if c.field == "InputVersion" {
			column = "input_version"
		}
		tag := sf.Tag.Get("gorm")
		if !strings.Contains(tag, "column:"+column) {
			t.Fatalf("%s 的列名必须是 %s，实际标签 %q", c.field, column, tag)
		}
		if strings.Contains(tag, "not null") {
			t.Fatalf("%s 不得声明 NOT NULL（历史来源不回填），实际标签 %q", c.field, tag)
		}
	}
}
