package presentationservice

import (
	"context"
	"testing"

	presentationcontract "go_wp/internal/module/presentation/contract"
)

func TestRebuildStaleSyncRequiresProject(t *testing.T) {
	s := &Service{}
	if n, err := s.RebuildStaleSync(context.Background(), "  "); err == nil {
		t.Fatalf("空 projectID 必须报错（否则空集被当成「没有待办」）, got rebuilt=%d", n)
	}
}

func TestRebuildStaleSyncIgnoresQueue(t *testing.T) {
	s := &Service{}
	q := newFakeBuildQueue()
	s.SetBuildQueue(q)

	if _, err := s.RebuildStaleSync(context.Background(), ""); err == nil {
		t.Fatal("空 projectID 必须报错")
	}
	if got := q.snapshot(); len(got) != 0 {
		t.Fatalf("同步重建入口不得入队，实际入队 %v", got)
	}
}

func TestRebuildStaleSyncSatisfiesPort(t *testing.T) {
	var port presentationcontract.StaleRebuildPort = &Service{}
	if port == nil {
		t.Fatal("不可达")
	}
}
