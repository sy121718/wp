package unit

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	artifactdto "go_wp/internal/module/artifact/dto"
)

func TestArtifactRecordSameDraftVersionKeepsDistinctContent(t *testing.T) {
	svc := newService(t)
	first := mustRecord(t, svc, validReq())
	next := validReq()
	next.ArtifactID = testArtifactID2
	next.ArtifactHash = artifactHashV2
	second := mustRecord(t, svc, next)
	if first.ID == second.ID || artifactRowCount(t, svc) != 2 {
		t.Fatal("同草稿版本的不同内容必须有独立身份")
	}
	old, err := svc.DetailByID(context.Background(), &artifactdto.DetailByIDReq{ID: first.ID})
	if err != nil || old.ArtifactHash != first.ArtifactHash {
		t.Fatalf("新归档改变旧身份: %+v %v", old, err)
	}
}

func TestArtifactConcurrentSameContentHasOneIdentity(t *testing.T) {
	svc := newService(t)
	const count = 8
	start := make(chan struct{})
	type result struct {
		id  string
		err error
	}
	results := make(chan result, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := validReq()
			req.ArtifactID = uuid.NewString()
			req.Version = int64(i + 1)
			<-start
			var out *artifactdto.ArtifactResp
			var err error
			if i%2 == 0 {
				out, err = svc.Record(context.Background(), req)
			} else {
				out, err = svc.EnsureRecord(context.Background(), req)
			}
			id := ""
			if out != nil {
				id = out.ID
			}
			results <- result{id, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	id := ""
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id == "" {
			id = r.id
		}
		if r.id != id {
			t.Fatalf("同内容出现两个身份: %s %s", id, r.id)
		}
	}
	if artifactRowCount(t, svc) != 1 || closureCount(t, svc, id) != 2 {
		t.Fatal("并发幂等必须只归档一份完整产物与闭包")
	}
}
