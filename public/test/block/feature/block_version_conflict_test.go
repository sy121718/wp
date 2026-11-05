package feature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockhttp "go_wp/internal/module/block/inbound/http"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

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

func TestBlockSaveRejectsStaleVersion(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	block := newVersionBlock(t, env, "版本冲突块")
	ctx := context.Background()

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

	var withVersion interface{ Version() string }
	if !errors.As(err, &withVersion) {
		t.Fatalf("冲突错误应实现 Version() 供出口填充占位符，实际 %T", err)
	}
	if withVersion.Version() != "2" {
		t.Errorf("冲突错误应带当前版本 2，实际 %q", withVersion.Version())
	}

	cur, err := env.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil {
		t.Fatalf("读块失败: %v", err)
	}
	if containsVersion(string(cur.Document), 999) {
		t.Error("过期版本的保存把内容覆盖了 —— 这正是要被消灭的丢更新")
	}
}

func TestBlockSaveConcurrentOnlyOneWins(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	block := newVersionBlock(t, env, "并发保存块")
	ctx := context.Background()

	const racers = 8
	snapshot := uint64(block.Version)

	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
				ID: block.ID,

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

	cur, err := env.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil {
		t.Fatalf("读块失败: %v", err)
	}
	if cur.Version != 2 {
		t.Errorf("8 个并发只有 1 个成功，version 应为 2，实际 %d", cur.Version)
	}
}

func TestBlockSaveWithoutExpectedVersionRejects(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	block := newVersionBlock(t, env, "缺版本不能覆盖")
	ctx := context.Background()
	before, readErr := env.blocks.Detail(ctx, &blockdto.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if readErr != nil {
		t.Fatal(readErr)
	}
	_, err := env.blocks.Update(ctx, &blockdto.UpdateReq{ID: block.ID, Document: []byte(docWithMarker("must-not-save"))})
	if status, ok := facing.StatusOf(err); !ok || status != http.StatusBadRequest {
		t.Fatalf("缺版本必须400: %v", err)
	}
	current, err := env.blocks.Detail(ctx, &blockdto.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != block.Version || string(current.Document) != string(before.Document) {
		t.Fatalf("拒绝后版本与文档应保持: before=%+v after=%+v", block, current)
	}
}

func docWithMarker(marker string) string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"txt","type":"core.text","props":{"text":"` + marker + `"}}]}`
}

func containsVersion(s string, v int64) bool {
	return strings.Contains(s, strconv.FormatInt(v, 10))
}

func TestVersionConflictMapsTo409AndCarriesVersion(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	ctx := context.Background()

	i18n.InjectForTest(map[string]map[string]string{
		blockenums.ErrBlockVersionConflict: {"zh-CN": "该全局块已被其他人修改（当前第 %s 版），请刷新后重试"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	block := newVersionBlock(t, env, "版本冲突对外形态块")
	if _, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
		ID: block.ID, Document: []byte(docWithMarker("v2")),
		ExpectedVersion: uint64(block.Version),
	}); err != nil {
		t.Fatalf("第一次保存应成功: %v", err)
	}

	_, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
		ID: block.ID, Document: []byte(docWithMarker("stale")),
		ExpectedVersion: uint64(block.Version),
	})
	if err == nil {
		t.Fatal("过期版本保存必须失败")
	}
	status, ok := facing.StatusOf(err)
	if !ok || status != http.StatusConflict {
		t.Fatalf("版本冲突应声明 409（业务错误），实际 %d（ok=%v）—— "+
			"退成 500 会让用户以为出了 bug，不会想到刷新重试", status, ok)
	}

	fact, bok := facing.FactOf(err)
	if !bok || !fact.Business {
		t.Fatalf("版本冲突应命中业务判据，实际 ok=%v business=%v（err=%v）", bok, fact.Business, err)
	}
	// 版本号是位置参数：key 以「key|参数」形态交给失败片段的取词。
	if want := blockenums.ErrBlockVersionConflict + "|2"; fact.Key != want {
		t.Errorf("key 应为 %s，实际 %q", want, fact.Key)
	}

	var versioned interface{ Version() string }
	if !errors.As(err, &versioned) || versioned.Version() != "2" {
		t.Fatalf("错误应携带当前版本2: %v", err)
	}
}

func TestSaveContentVersionConflictReturns409(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	ctx := context.Background()

	i18n.InjectForTest(map[string]map[string]string{
		blockenums.ErrBlockVersionConflict: {"zh-CN": "该全局块已被其他人修改（当前第 %s 版），请刷新后重试"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	block := newVersionBlock(t, env, "HTTP 409 断言块")

	if _, err := env.blocks.Update(ctx, &blockdto.UpdateReq{
		ID: block.ID, Document: []byte(docWithMarker("v2")),
		ExpectedVersion: uint64(block.Version),
	}); err != nil {
		t.Fatalf("第一次保存应成功: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.POST("/admin/blocks/save-content",
		blockhttp.NewBlockPageHandle(env.blocks, nil).SaveBlockContent)

	body := `{"id":"` + block.ID + `","document":` + docWithMarker("stale") + `,"expectedVersion":1}`
	req := httptest.NewRequest(http.MethodPost, "/admin/blocks/save-content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("版本冲突的 HTTP 状态码应为 409，实际 %d（体=%s）—— "+
			"退成 500 会把它误报成基础设施故障", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/admin/blocks/save-content",
		strings.NewReader(`{"id":`))
	req2.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("参数绑定失败的 HTTP 状态码应为 400，实际 %d", rec2.Code)
	}
}
