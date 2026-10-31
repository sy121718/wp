package buildmodel

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestCreateTxRequiresTransaction(t *testing.T) {
	m := NewModel(nil)
	for _, tx := range []*gorm.DB{nil, &gorm.DB{}, {}} {
		err := m.CreateTx(tx, &Entity{SourceType: SourceTypeMenuStale, SourceID: "x"})
		if !errors.Is(err, ErrTxRequired) {
			t.Fatalf("非事务句柄 %#v 应被拒绝，实际 %v", tx, err)
		}
	}
}

func TestNextMenuStaleVersionTxRequiresTransaction(t *testing.T) {
	m := NewModel(nil)
	for _, tx := range []*gorm.DB{nil, &gorm.DB{}} {
		if _, err := m.NextMenuStaleVersionTx(tx, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", "header"); !errors.Is(err, ErrTxRequired) {
			t.Fatalf("非事务句柄 %#v 应被拒绝，实际 %v", tx, err)
		}
	}
}

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
