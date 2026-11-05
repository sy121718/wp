package source

import (
	"context"
	"testing"
)

func TestPreviewStateIsClosedAndRequestScoped(t *testing.T) {
	if PreviewStateFrom(nil) != PreviewDefault {
		t.Fatal("缺省必须default")
	}
	parent := WithPreviewState(context.Background(), PreviewSuccess)
	child := WithPreviewState(parent, PreviewForbidden)
	if PreviewStateFrom(parent) != PreviewSuccess || PreviewStateFrom(child) != PreviewForbidden {
		t.Fatal("请求状态不得污染父上下文")
	}
	if PreviewStateFrom(WithPreviewState(nil, "arbitrary")) != PreviewDefault {
		t.Fatal("不能传自由模拟状态")
	}
}
