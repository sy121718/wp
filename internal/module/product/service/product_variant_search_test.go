package productservice

import (
	"context"
	"testing"
)

func TestListAllVariantOptionsRejectsEmptyProject(t *testing.T) {
	s := &Service{}
	if _, err := s.ListAllVariantOptions(context.Background(), "   "); err == nil {
		t.Fatal("空工程应报参数错误，实际拿到了 nil error")
	}
}
