package feature

import (
	"fmt"
	"testing"
	"time"
)

func TestBenchPublishCost(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	start := time.Now()
	for i := 0; i < 5; i++ {
		slug := fmt.Sprintf("bench-%d", i)
		id := f.createProduct(t, "基准", slug, "d", 10, 20)
		f.publish(t, id, "/"+slug)
	}
	t.Logf("5 次发布耗时 %v（每次约 %v）", time.Since(start), time.Since(start)/5)
}
