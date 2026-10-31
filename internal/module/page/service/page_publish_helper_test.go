package pageservice

import (
	"errors"
	"fmt"
	"testing"

	"go_wp/internal/pipeline"
)

func TestPageHasPublishedState(t *testing.T) {
	cases := []struct {
		name string
		rec  *pipeline.PageRecord
		want bool
	}{
		{"nil 记录", nil, false},
		{"空记录", &pipeline.PageRecord{}, false},
		{"有激活指针", &pipeline.PageRecord{ActiveHash: "h1"}, true},
		{"无指针但有已发布历史", &pipeline.PageRecord{
			Histories: []*pipeline.HistoryEntry{{Status: pipeline.StatePublished, Hash: "h2"}},
		}, true},
		{"只有草稿历史", &pipeline.PageRecord{
			Histories: []*pipeline.HistoryEntry{{Status: pipeline.StateSuperseded, Hash: "h3"}},
		}, false},
		{"指针为空串 + 草稿历史", &pipeline.PageRecord{
			ActiveHash: "",
			Histories:  []*pipeline.HistoryEntry{{Status: pipeline.StateSuperseded}},
		}, false},
	}
	for _, c := range cases {
		if got := pageHasPublishedState(c.rec); got != c.want {
			t.Errorf("%s: 期望 %v，实际 %v", c.name, c.want, got)
		}
	}
}

func TestActiveHashOf(t *testing.T) {
	if got := activeHashOf(nil); got != "" {
		t.Errorf("nil 记录应返回空串，实际 %q", got)
	}
	if got := activeHashOf(&pipeline.PageRecord{
		ActiveHash: "",
		Histories:  []*pipeline.HistoryEntry{{Status: pipeline.StatePublished, Hash: "h-old"}},
	}); got != "" {
		t.Errorf("指针为空时应返回空串（历史里的 hash 不代表线上内容），实际 %q", got)
	}
	if got := activeHashOf(&pipeline.PageRecord{ActiveHash: "h-live"}); got != "h-live" {
		t.Errorf("应返回激活 hash，实际 %q", got)
	}
}

func TestMapPublishError(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"版本冲突", pipeline.ErrVersionConflict, ErrDraftVersionConflict},
		{"无暂存产物", pipeline.ErrNoStagedArtifact, ErrNoStagedArtifact},
		{"回滚路径不符", pipeline.ErrRollbackPathMismatch, ErrRebuildRequired},
		{"页面不存在", pipeline.ErrPageNotFound, ErrPageNotFound},
	}
	for _, c := range cases {
		if got := mapPublishError(c.in); !errors.Is(got, c.want) {
			t.Errorf("%s: 期望 %v，实际 %v", c.name, c.want, got)
		}

		wrapped := fmt.Errorf("构建失败: %w", c.in)
		if got := mapPublishError(wrapped); !errors.Is(got, c.want) {
			t.Errorf("%s（包裹）: 期望 %v，实际 %v", c.name, c.want, got)
		}
	}
}

func TestMapPublishErrorPassesUnknownThrough(t *testing.T) {
	inner := errors.New("磁盘写满了")
	if got := mapPublishError(inner); !errors.Is(got, inner) {
		t.Fatalf("未知错误应原样透出（吞掉会丢掉唯一的排查线索），实际 %v", got)
	}
	if got := mapPublishError(nil); got != nil {
		t.Fatalf("nil 应返回 nil，实际 %v", got)
	}
}
