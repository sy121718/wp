package presentationservice

// presentation_project_scope_test.go — 工程解析的 fail-closed 护栏（评审 H1-b）。
//
// 为什么单独钉这一条：resolveProjectID 是全部实例写路径的入口（16 处调用），
// 原来的写法是「project 契约非 nil 才校验存在性」—— 契约缺失时**显式 projectID 直接放行**，
// 任意工程 id 都能跳过存在性校验直达写入。这是典型的「校验不了就通过」：装配缺口
// 不该变成授权缺口。边界取证（为什么应用层做不了更强的归属校验）写在
// presentation_render.go 的 resolveProjectID 注释里。
//
// 断言具备失败能力：把 s.project == nil 的早退删掉、恢复成 `if s.project != nil { … }` 包裹，
// 第一个用例立刻红。

import (
	"context"
	"testing"

	"github.com/google/uuid"

	presentationenums "go_wp/internal/module/presentation/enums"
)

// TestResolveProjectIDFailsClosedWithoutContract 契约缺失 + 显式工程 id：必须拒绝，不得放行。
func TestResolveProjectIDFailsClosedWithoutContract(t *testing.T) {
	s := &Service{} // 装配缺口：project 契约未注入
	got, err := s.resolveProjectID(context.Background(), uuid.NewString())
	if err == nil {
		t.Fatalf("契约缺失时不得放行显式工程 id（实际返回 %q）", got)
	}
	if err.Error() != presentationenums.ErrProjectRequired {
		t.Fatalf("契约缺失应报 %s，实际 %v", presentationenums.ErrProjectRequired, err)
	}
	if got != "" {
		t.Fatalf("被拒绝的解析不得同时给出工程 id（实际 %q）", got)
	}
}

// TestResolveProjectIDWithoutExplicitNeedsContract 未显式指定且契约缺失：同样拒绝（原有语义）。
func TestResolveProjectIDWithoutExplicitNeedsContract(t *testing.T) {
	s := &Service{}
	got, err := s.resolveProjectID(context.Background(), "  ")
	if err == nil {
		t.Fatalf("契约缺失且未显式指定工程时应报错（实际返回 %q）", got)
	}
	if err.Error() != presentationenums.ErrProjectRequired {
		t.Fatalf("应报 %s，实际 %v", presentationenums.ErrProjectRequired, err)
	}
}
