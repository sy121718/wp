package presentationservice

import (
	"context"
	"testing"

	"github.com/google/uuid"

	presentationenums "go_wp/internal/module/presentation/enums"
)

func TestResolveProjectIDFailsClosedWithoutContract(t *testing.T) {
	s := &Service{}
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
