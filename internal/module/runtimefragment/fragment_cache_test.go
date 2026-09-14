package runtimefragment

import (
	"context"
	"testing"
)

func TestFragmentCacheKeyDeterministic(t *testing.T) {
	req := &Request{
		Lang: "zh-CN",
		Params: map[string]string{
			"projectId":  "p1",
			"nodeId":     "n1",
			"page":       "2",
			"categoryId": "c1",
		},
	}
	k1, ok1 := fragmentCacheKey(context.Background(), "productList", req)
	k2, ok2 := fragmentCacheKey(context.Background(), "productList", req)
	if !ok1 || !ok2 || k1 == "" || k1 != k2 {
		t.Fatalf("cache key not stable: %q %q ok=%v", k1, k2, ok1)
	}
}

func TestFragmentCacheKeySkipsNonCacheable(t *testing.T) {
	req := &Request{Params: map[string]string{"projectId": "p1"}}
	if _, ok := fragmentCacheKey(context.Background(), "cartView", req); ok {
		t.Fatal("cartView must not be cacheable")
	}
}
