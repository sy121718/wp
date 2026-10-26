package productservice

import (
	"context"
	"testing"
)

// 空工程不解释成「全部工程」：否则一个工程的页面会列出所有工程的 SKU。
func TestListAllVariantOptionsRejectsEmptyProject(t *testing.T) {
	s := &Service{}
	if _, err := s.ListAllVariantOptions(context.Background(), "   "); err == nil {
		t.Fatal("空工程应报参数错误，实际拿到了 nil error")
	}
}
