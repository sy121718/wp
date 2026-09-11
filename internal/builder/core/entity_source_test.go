package core

// entity_source_test.go — 实体类型注册表契约测试。
//
// 覆盖：注册 / 查询 / 白名单 / 解析器透传 / 重复与非法注册拒绝。
// 这张注册表是构建层「实体类型与字段白名单」的唯一运行时来源，
// 内容模板与发布实例都依赖它，因此必须有直接契约测试。

import (
	"context"
	"errors"
	"testing"
)

// errStubResolver 来源返回的哨兵错误（验证 ResolverFor 透传）。
var errStubResolver = errors.New("stub resolver error")

// stubSource 测试桩实体来源。
type stubSource struct {
	entityType string
	fields     []string
	err        error
}

func (s stubSource) EntityType() string       { return s.entityType }
func (s stubSource) FieldWhitelist() []string { return s.fields }
func (s stubSource) ResolverFor(_ context.Context, _ string) (ContentResolver, error) {
	return nil, s.err
}

func TestEntitySourceRegistryRegisterAndLookup(t *testing.T) {
	reg := NewEntitySourceRegistry()
	if err := reg.Register(stubSource{entityType: "article", fields: []string{"title", "body"}}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	src, ok := reg.Lookup("article")
	if !ok || src.EntityType() != "article" {
		t.Fatalf("Lookup 未命中已注册类型")
	}
	if _, ok := reg.Lookup("tag"); ok {
		t.Fatalf("未注册类型不应命中")
	}
	if !reg.IsValidType("article") || reg.IsValidType("tag") {
		t.Fatalf("IsValidType 判定错误")
	}
}

func TestEntitySourceRegistryFieldWhitelist(t *testing.T) {
	reg := NewEntitySourceRegistry()
	_ = reg.Register(stubSource{entityType: "article", fields: []string{"title"}})
	if reg.FieldWhitelist("tag") != nil {
		t.Fatalf("未知类型白名单应为 nil")
	}
	got := reg.FieldWhitelist("article")
	if len(got) != 1 || got[0] != "title" {
		t.Fatalf("白名单读取失败: %v", got)
	}
}

func TestEntitySourceRegistryResolverFor(t *testing.T) {
	reg := NewEntitySourceRegistry()
	_ = reg.Register(stubSource{entityType: "article", err: errStubResolver})
	if _, err := reg.ResolverFor(context.Background(), "tag", "id"); err == nil {
		t.Fatalf("未注册类型的 ResolverFor 应报错")
	}
	if _, err := reg.ResolverFor(context.Background(), "article", "id"); !errors.Is(err, errStubResolver) {
		t.Fatalf("应透传来源返回的错误，got %v", err)
	}
}

func TestEntitySourceRegistryRejectsInvalid(t *testing.T) {
	reg := NewEntitySourceRegistry()
	if err := reg.Register(nil); err == nil {
		t.Fatalf("nil 来源应被拒绝")
	}
	for _, bad := range []string{"", "  ", " article "} {
		if err := reg.Register(stubSource{entityType: bad}); err == nil {
			t.Fatalf("非法标识 %q 应被拒绝", bad)
		}
	}
	if err := reg.Register(stubSource{entityType: "article"}); err != nil {
		t.Fatalf("首次注册应成功: %v", err)
	}
	if err := reg.Register(stubSource{entityType: "article"}); err == nil {
		t.Fatalf("重复注册应被拒绝")
	}
}
