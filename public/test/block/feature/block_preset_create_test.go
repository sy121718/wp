package feature

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
	"gorm.io/gorm"
)

type controlledPresetResolver struct{ calls atomic.Int32 }

func (r *controlledPresetResolver) ResolvePreset(_ context.Context, key string, version int) (json.RawMessage, error) {
	r.calls.Add(1)
	if key != "test.account" || version != 2 {
		return nil, blockenums.Err(blockenums.ErrBlockPresetUnavailable)
	}
	return json.RawMessage(docWithMarker("controlled-account")), nil
}

func TestBusinessPresetCreatePersistsAuthorityAndRejectsConflicts(t *testing.T) {
	env := newRefEnv(t)
	resolver := &controlledPresetResolver{}
	env.blocks.(interface {
		SetPresetResolver(blockcontract.PresetResolver)
	}).SetPresetResolver(resolver)
	ctx := context.Background()
	created, err := env.blocks.Create(ctx, &blockdto.CreateReq{ProjectID: env.projectID, Name: "账号中心", PresetKey: "test.account", PresetVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if resolver.calls.Load() != 1 || created.Version != 1 || created.ReuseMode != "global" || created.PresetKey == nil || *created.PresetKey != "test.account" || created.PresetVersion == nil || *created.PresetVersion != 2 {
		t.Fatalf("来源/版本未落库：%+v", created)
	}
	loaded, err := env.blocks.Detail(ctx, &blockdto.DetailReq{ProjectID: env.projectID, ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	var createdDoc, loadedDoc any
	if err := json.Unmarshal(created.Document, &createdDoc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(loaded.Document, &loadedDoc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedDoc, createdDoc) || loaded.PresetKey == nil || *loaded.PresetKey != *created.PresetKey || !strings.Contains(string(loaded.Document), "controlled-account") {
		t.Fatalf("回读事实与创建不一致：%+v", loaded)
	}
	_, err = env.blocks.Create(ctx, &blockdto.CreateReq{ProjectID: env.projectID, Name: "账号中心", PresetKey: "test.account", PresetVersion: 2})
	if !errors.Is(err, blockcontract.ErrDuplicate) {
		t.Fatalf("同名必须拒绝，不自动后缀：%v", err)
	}
	for _, req := range []*blockdto.CreateReq{
		{ProjectID: env.projectID, Name: "覆盖受控AST", PresetKey: "test.account", PresetVersion: 2, Document: json.RawMessage(emptyDoc)},
		{ProjectID: env.projectID, Name: "不存在版本", PresetKey: "test.account", PresetVersion: 3},
		{ProjectID: env.projectID, Name: "不存在预设", PresetKey: "missing", PresetVersion: 2},
	} {
		if _, err := env.blocks.Create(ctx, req); err == nil {
			t.Fatalf("非法创建未拒绝：%+v", req)
		} else if status, ok := facing.StatusOf(err); !ok || status != http.StatusBadRequest {
			t.Fatalf("非法来源应400：%v", err)
		}
	}
	rows, err := env.blocks.List(ctx, &blockdto.ListReq{ProjectID: env.projectID})
	if err != nil || len(rows) != 1 || rows[0].ID != created.ID {
		t.Fatalf("失败路径发生持久化副作用：%+v %v", rows, err)
	}
	if _, err := env.blocks.Create(ctx, &blockdto.CreateReq{ProjectID: env.projectID, Name: "普通空区块"}); err != nil {
		t.Fatalf("普通空文档创建回归：%v", err)
	}
}

func TestBusinessPresetWithoutRegistryCannotCreate(t *testing.T) {
	env := newRefEnv(t)
	_, err := env.blocks.Create(context.Background(), &blockdto.CreateReq{ProjectID: env.projectID, Name: "未装配", PresetKey: "test.account", PresetVersion: 1})
	if status, ok := facing.StatusOf(err); !ok || status != http.StatusBadRequest {
		t.Fatalf("未装配不能默默生成空业务块：%v", err)
	}
	rows, err := env.blocks.List(context.Background(), &blockdto.ListReq{ProjectID: env.projectID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("未装配写了空壳：%+v %v", rows, err)
	}
}

func TestBusinessPresetConcurrentNamesRejectWithConflict(t *testing.T) {
	env := newRefEnv(t)
	env.blocks.(interface {
		SetPresetResolver(blockcontract.PresetResolver)
	}).SetPresetResolver(&controlledPresetResolver{})
	const workers = 4
	var arrived atomic.Int32
	barrier := make(chan struct{})
	// 让所有调用都先通过ExistsByName，确实覆盖唯一索引的冲突分支。
	callback := "test:preset-create-name-barrier"
	if err := env.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "blocks" {
			return
		}
		if arrived.Add(1) == workers {
			close(barrier)
		}
		select {
		case <-barrier:
		case <-time.After(5 * time.Second):
			tx.AddError(errors.New("并发创建屏障未完成"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.db.Callback().Create().Remove(callback) })
	errorsOut := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "AccountCenter"
			if i%2 == 1 {
				name = "accountcenter"
			}
			_, err := env.blocks.Create(context.Background(), &blockdto.CreateReq{ProjectID: env.projectID, Name: name, PresetKey: "test.account", PresetVersion: 2})
			errorsOut <- err
		}(i)
	}
	wg.Wait()
	close(errorsOut)
	success, conflicts := 0, 0
	for err := range errorsOut {
		if err == nil {
			success++
			continue
		}
		status, ok := facing.StatusOf(err)
		if !errors.Is(err, blockcontract.ErrDuplicate) || !ok || status != http.StatusConflict {
			t.Fatalf("并发重名未返回409同名冲突：%v", err)
		}
		conflicts++
	}
	rows, err := env.blocks.List(context.Background(), &blockdto.ListReq{ProjectID: env.projectID})
	if err != nil || success != 1 || conflicts != workers-1 || len(rows) != success {
		t.Fatalf("并发创建非唯一：成功%d/冲突%d/行%d，%v", success, conflicts, len(rows), err)
	}
}
