package feature

// block_version_conflict_test.go — 全局块保存的乐观并发（B2 迁移 676 / B1 契约 §3）。
//
// 守的缺陷形态：`UpdateDocument` 原本是**覆盖式整树保存**。两个管理员同时编辑同一个
// 全局块时，后保存的静默覆盖前一个 —— 而全局块被多个页面引用、可能正跑在线上。
// 这类丢失不报错、不留痕，只表现为「我改的东西又回去了」，且下一次构建才在产物里现形。
//
// 判据分三层，缺一层就挡不住真问题：
//
//	1  过期版本被拒（409），且**错误里带当前版本号** —— 只说"已被修改"时前端只能让用户
//	   刷新整页；带上版本才能提示"当前第 N 版"。
//	2  正确版本能保存，且保存后 version 自增 —— 不自增等于"第一次之后永远冲突"。
//	3  **真实并发**：N 个 goroutine 拿同一个快照同时保存，必须只有一个成功。
//	   串行断言过了不代表并发对（这正是 -race 之外还需要真实并发的原因）。

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/pkg/facing"
)

// newVersionBlock 建一个全局块并返回它（version 应为 1）。
func newVersionBlock(t *testing.T, env *refEnv, name string) *blockdto.BlockResp {
	t.Helper()
	got, err := env.blocks.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: env.projectID, Name: name, Document: []byte(docWithMarker("initial")),
	})
	if err != nil {
		t.Fatalf("建块失败: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("新建块 version 应为 1，实际 %d", got.Version)
	}
	return got
}

// TestBlockSaveRejectsStaleVersion 过期版本被拒，错误里带当前版本号与 409。
func TestBlockSaveRejectsStaleVersion(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	block := newVersionBlock(t, env, "版本冲突块")
	ctx := context.Background()

	// 第一次用当前版本保存：成功，version 1 → 2。
	saved, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
		ID: block.ID, Document: []byte(docWithMarker("v2")),
		ExpectedVersion: uint64(block.Version),
	})
	if err != nil {
		t.Fatalf("正确版本保存应成功: %v", err)
	}
	if saved.Version != 2 {
		t.Fatalf("保存后 version 应自增到 2，实际 %d", saved.Version)
	}

	// 再用**旧**版本（1）保存：必须 409，且不覆盖已保存的内容。
	_, err = env.blocks.Update(ctx, &blockdto.UpdateReq{
		ID: block.ID, Document: []byte(docWithMarker("999")),
		ExpectedVersion: uint64(block.Version),
	})
	if err == nil {
		t.Fatal("过期版本保存必须被拒（否则就是覆盖式保存，别人的编辑静默丢失）")
	}
	if !errors.Is(err, blockcontract.ErrBlockVersionConflict) {
		t.Fatalf("应是 ErrBlockVersionConflict，实际 %v", err)
	}
	status, ok := facing.StatusOf(err)
	if !ok || status != http.StatusConflict {
		t.Errorf("版本冲突应声明 409，实际 %d（ok=%v）", status, ok)
	}
	// 错误要能交出版本号，出口据此提示「当前第 N 版」。
	//
	// 断言的是 Version() 而不是 err.Error()：错误串是**内部标识**（key），
	// 对用户说的话由出口用 key + Args 取词生成（词条里是 %s）。
	// 让 Error() 直接带中文，就等于把一句成品句塞进错误值 —— 换语言时它不会跟着变。
	var withVersion interface{ Version() string }
	if !errors.As(err, &withVersion) {
		t.Fatalf("冲突错误应实现 Version() 供出口填充占位符，实际 %T", err)
	}
	if withVersion.Version() != "2" {
		t.Errorf("冲突错误应带当前版本 2，实际 %q", withVersion.Version())
	}

	// 内容没被覆盖。
	cur, err := env.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil {
		t.Fatalf("读块失败: %v", err)
	}
	if containsVersion(string(cur.Document), 999) {
		t.Error("过期版本的保存把内容覆盖了 —— 这正是要被消灭的丢更新")
	}
}

// TestBlockSaveConcurrentOnlyOneWins 真实并发：N 个请求持同一快照，只能有一个成功。
//
// 为什么必须真并发：串行的「先读后写」断言在覆盖式保存下**也会通过**
// （每次都是最新版本），只有同时读同一版本才能暴露丢更新。
func TestBlockSaveConcurrentOnlyOneWins(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	block := newVersionBlock(t, env, "并发保存块")
	ctx := context.Background()

	const racers = 8
	snapshot := uint64(block.Version) // 所有 racer 拿到同一个版本

	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // 尽量让它们同时进入
			_, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
				ID: block.ID,
				// 每个 racer 写不同内容，方便判断谁赢了。
				Document:        []byte(docWithMarker(strconv.Itoa(idx))),
				ExpectedVersion: snapshot,
			})
			results[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()

	okCount, conflictCount := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, blockcontract.ErrBlockVersionConflict):
			conflictCount++
		default:
			t.Errorf("racer %d 出现非冲突错误: %v", i, err)
		}
	}
	if okCount != 1 {
		t.Fatalf("%d 个 racer 持同一版本，应恰好 1 个成功，实际成功 %d / 冲突 %d",
			racers, okCount, conflictCount)
	}
	if conflictCount != racers-1 {
		t.Errorf("其余 %d 个都应报版本冲突，实际冲突 %d", racers-1, conflictCount)
	}

	// 版本只涨 1（不是涨 N —— 那说明有 racer 在"冲突"之后仍然写了）。
	cur, err := env.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil {
		t.Fatalf("读块失败: %v", err)
	}
	if cur.Version != 2 {
		t.Errorf("8 个并发只有 1 个成功，version 应为 2，实际 %d", cur.Version)
	}
}

// TestBlockSaveWithoutExpectedVersionOverwrites 0 表示不校验（显式覆盖式保存）。
//
// 这条不是"允许随便覆盖"，而是把两种语义**显式区分**：传 0 的调用方明确表示
// "我知道我在覆盖"（新建后立即保存、迁移、测试夹具）。浏览器与 AI 入口一律带真值。
func TestBlockSaveWithoutExpectedVersionOverwrites(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	block := newVersionBlock(t, env, "不校验版本块")
	ctx := context.Background()

	if _, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
		ID: block.ID, Document: []byte(docWithMarker("a1")),
	}); err != nil {
		t.Fatalf("不带 expectedVersion 的保存应成功: %v", err)
	}
	again, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
		ID: block.ID, Document: []byte(docWithMarker("a2")),
	})
	if err != nil {
		t.Fatalf("第二次不带版本的保存也应成功: %v", err)
	}
	// 不校验时不涨版本：version 只在"按版本改"这条路上自增。
	if again.Version != 1 {
		t.Errorf("不校验版本的保存不该改 version，实际 %d", again.Version)
	}
}

// docWithMarker 一份**合法**的块文档，正文里带一个标记串。
//
// 为什么不能写空 root：service 的 validateDocument 会解析并校验整棵 AST，
// 形状不对直接 ErrBlockInvalidDoc。这里刻意复用合法形状（与 block_source_ref_guard_test.go
// 的 emptyDoc 同源），让断言聚焦在版本语义上，不被文档校验失败掩盖。
func docWithMarker(marker string) string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"txt","type":"core.text","props":{"text":"` + marker + `"}}]}`
}

// containsVersion 判断文本里是否出现某个版本号（用于「带当前版本号」这类断言）。
func containsVersion(s string, v int64) bool {
	return strings.Contains(s, strconv.FormatInt(v, 10))
}
