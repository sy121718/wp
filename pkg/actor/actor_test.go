package actor

import (
	"context"
	"testing"
)

func TestFromWithoutActorIsSystem(t *testing.T) {
	if a := From(context.Background()); !a.System() {
		t.Fatalf("未注入时应是系统身份，得到 %+v", a)
	}
	// nil ctx 同样按系统身份处理：调用链里可能有 Background() 被漏掉的地方。
	if a := From(nil); !a.System() {
		t.Fatalf("nil ctx 应是系统身份，得到 %+v", a)
	}
}

func TestWithActorRoundTrip(t *testing.T) {
	ctx := WithActor(context.Background(), Actor{ID: 42, Name: "仓管小王"})
	got := From(ctx)
	if got.ID != 42 || got.Name != "仓管小王" {
		t.Fatalf("取回的身份不对：%+v", got)
	}
	if got.System() {
		t.Fatal("有 id 时不该判成系统身份")
	}
}

func TestWithActorOverridesEarlier(t *testing.T) {
	ctx := WithActor(context.Background(), Actor{ID: 1, Name: "a"})
	ctx = WithActor(ctx, Actor{ID: 2, Name: "b"})
	if got := From(ctx); got.ID != 2 || got.Name != "b" {
		t.Fatalf("后写应覆盖前写，得到 %+v", got)
	}
}

func TestWithActorNilContext(t *testing.T) {
	// 显式传 nil 也不该 panic：装配期错误不该变成请求期的空指针。
	ctx := WithActor(nil, Actor{ID: 7, Name: "sys"})
	if got := From(ctx); got.ID != 7 {
		t.Fatalf("nil ctx 注入后应能取回，得到 %+v", got)
	}
}
