package navigationservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
)

type menuWriteRecorder struct {
	calls []string
	errAt string
	txGot *gorm.DB
}

func (r *menuWriteRecorder) record(name string, tx *gorm.DB) error {
	r.calls = append(r.calls, name)
	r.txGot = tx
	if r.errAt == name {
		return errors.New("端口失败：" + name)
	}
	return nil
}

type writePortsStub struct {
	rec  *menuWriteRecorder
	hits int64
}

func (s writePortsStub) hit() int64 {
	if s.hits == 0 {
		return 1
	}
	return s.hits
}

func (s writePortsStub) MarkMenuStaleTx(_ context.Context, tx *gorm.DB, _, kind string, _ time.Time) (int64, error) {
	return s.hit(), s.rec.record("menu:"+kind, tx)
}

func (s writePortsStub) MarkInstanceMenuStaleTx(_ context.Context, tx *gorm.DB, _, kind string, _ time.Time) (int64, error) {
	return s.hit(), s.rec.record("instance:"+kind, tx)
}

func (s writePortsStub) MarkNavigationStaleTx(_ context.Context, tx *gorm.DB, _, navigationID string) (int64, error) {
	return s.hit(), s.rec.record("nav:"+navigationID, tx)
}

func (s writePortsStub) MarkInstanceNavigationStaleTx(_ context.Context, tx *gorm.DB, _, navigationID string, _ time.Time) (int64, error) {
	return s.hit(), s.rec.record("navinstance:"+navigationID, tx)
}

func (s writePortsStub) EnqueueMenuStaleTx(_ context.Context, tx *gorm.DB, _, kind string) (int64, error) {
	return s.hit(), s.rec.record("queue:"+kind, tx)
}

var (
	_ navigationcontract.PageStalePort                 = writePortsStub{}
	_ navigationcontract.PresentationStalePort         = writePortsStub{}
	_ navigationcontract.MenuItemStalePort             = writePortsStub{}
	_ navigationcontract.MenuItemPresentationStalePort = writePortsStub{}
	_ navigationcontract.MenuStaleEnqueuePort          = writePortsStub{}
)

func newMenuWriteService(t *testing.T) (*Service, *menuWriteRecorder, *txRunnerStub) {
	t.Helper()
	rec := &menuWriteRecorder{}
	tx := &txRunnerStub{tx: newScopeOnlyTx(t)}
	svc := &Service{
		m:       navigationmodel.NewModel(nil),
		writeTx: tx,
	}
	setAllPorts(svc, rec, 0)
	return svc, rec, tx
}

func setAllPorts(svc *Service, rec *menuWriteRecorder, hits int64) {
	p := writePortsStub{rec: rec, hits: hits}
	svc.SetWritePorts(NavigationWriteDeps{Pages: p, Instances: p, Items: p, ItemInstances: p, Queue: p})
}

func TestMenuWriteFailsClosedWithoutPorts(t *testing.T) {
	cases := []struct {
		name string
		mk   func() *Service
	}{
		{"全部缺失", func() *Service { return &Service{m: navigationmodel.NewModel(nil)} }},
		{"缺页端口", func() *Service {
			s := &Service{m: navigationmodel.NewModel(nil)}
			s.SetWritePorts(NavigationWriteDeps{})
			return s
		}},
		{"缺实例端口", func() *Service {

			rec := &menuWriteRecorder{}
			s := &Service{m: navigationmodel.NewModel(nil)}
			p := writePortsStub{rec: rec}
			s.SetWritePorts(NavigationWriteDeps{Pages: p, Items: p, Queue: p})
			s.writeTx = &txRunnerStub{tx: newScopeOnlyTx(t)}
			return s
		}},
		{"缺按项实例端口", func() *Service {

			rec := &menuWriteRecorder{}
			s := &Service{m: navigationmodel.NewModel(nil)}
			p := writePortsStub{rec: rec}
			s.SetWritePorts(NavigationWriteDeps{Pages: p, Instances: p, Items: p, Queue: p})
			s.writeTx = &txRunnerStub{tx: newScopeOnlyTx(t)}
			return s
		}},
		{"缺事务", func() *Service {
			rec := &menuWriteRecorder{}
			s := &Service{m: navigationmodel.NewModel(nil)}
			setAllPorts(s, rec, 0)
			return s
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := tc.mk()
			err := svc.withMenuWriteTx(context.Background(), uuid.NewString(), func(*gorm.DB) error {
				t.Fatal("端口不齐时不应执行事务体")
				return nil
			})
			if !errors.Is(err, navigationenums.Err(navigationenums.ErrTranslationWriteUnavailable)) {
				t.Fatalf("缺端口应返回「未就绪」业务错误，实际 %v", err)
			}
		})
	}
}

func TestWithMenuWriteTxLocksAndScopes(t *testing.T) {
	svc, _, tx := newMenuWriteService(t)
	projectID := uuid.NewString()
	var bodyRan bool
	var bodyTx *gorm.DB
	if err := svc.withMenuWriteTx(context.Background(), projectID, func(got *gorm.DB) error {
		bodyRan = true
		bodyTx = got
		return nil
	}); err != nil {
		t.Fatalf("事务应成功: %v", err)
	}
	if !bodyRan {
		t.Fatal("事务体未执行")
	}
	if bodyTx != tx.tx {
		t.Fatal("事务体拿到的必须是同一个 tx 句柄（换连接就不再是同进同出）")
	}
	if tx.calls != 1 || tx.commits != 1 {
		t.Fatalf("应恰好开一次事务并提交一次，实际 calls=%d commits=%d", tx.calls, tx.commits)
	}
}

func TestWithMenuWriteTxRejectsBadProject(t *testing.T) {
	svc, _, tx := newMenuWriteService(t)
	for _, bad := range []string{"   ", "not-a-uuid"} {
		err := svc.withMenuWriteTx(context.Background(), bad, func(*gorm.DB) error { return nil })
		if !errors.Is(err, navigationenums.Err(navigationenums.ErrInvalidParam)) {
			t.Fatalf("工程 %q 应返回参数错误，实际 %v", bad, err)
		}
	}
	if tx.calls != 0 {
		t.Fatalf("参数错误不该开事务，实际 %d 次", tx.calls)
	}
}

func TestMarkMenuStaleInTxOrder(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	at := time.Unix(0, 0).UTC()
	if err := svc.markMenuStaleInTx(context.Background(), nil, uuid.NewString(), "header", at); err != nil {
		t.Fatalf("markMenuStaleInTx 失败: %v", err)
	}

	if got, want := strings.Join(rec.calls, ","), "menu:header,instance:header,queue:header"; got != want {
		t.Fatalf("失效顺序应为 %q，实际 %q", want, got)
	}

	failSvc, failRec, _ := newMenuWriteService(t)
	failRec.errAt = "queue:header"
	if err := failSvc.markMenuStaleInTx(context.Background(), nil, uuid.NewString(), "header", at); err == nil {
		t.Fatal("入队失败必须让整笔写失败（否则译文/菜单已提交但没人重建）")
	}
}

func TestMarkMenuStaleInTxCallsInstancesOnZeroHit(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)

	setAllPorts(svc, rec, 0)
	if err := svc.markMenuStaleInTx(context.Background(), nil, uuid.NewString(), "footer", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("零命中不该失败: %v", err)
	}
	want := "menu:footer,instance:footer,queue:footer"
	if got := strings.Join(rec.calls, ","); got != want {
		t.Fatalf("零命中也要逐个调用端口，顺序 %q，实际 %q", want, got)
	}
}

func TestMarkMenuStaleInTxInstanceFailureAborts(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	rec.errAt = "instance:header"
	if err := svc.markMenuStaleInTx(context.Background(), nil, uuid.NewString(), "header", time.Unix(0, 0).UTC()); err == nil {
		t.Fatal("实例标记失败必须让整笔写失败：否则菜单已提交、实例屏障缺失")
	}
	if got := strings.Join(rec.calls, ","); got != "menu:header,instance:header" {
		t.Fatalf("失败后不得继续入队，实际 %q", got)
	}
}

func TestMarkNavigationStaleInTxIgnoresEmptyID(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	if err := svc.markNavigationStaleInTx(context.Background(), nil, uuid.NewString(), "   "); err != nil {
		t.Fatalf("空项 id 应直接返回: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("空项 id 不该调用端口，实际 %v", rec.calls)
	}
}

func TestMarkNavigationStaleInTxPageThenInstance(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	id := uuid.NewString()
	if err := svc.markNavigationStaleInTx(context.Background(), nil, uuid.NewString(), id); err != nil {
		t.Fatalf("按项失效失败: %v", err)
	}
	want := "nav:" + id + ",navinstance:" + id
	if got := strings.Join(rec.calls, ","); got != want {
		t.Fatalf("按项失效顺序应为 %q，实际 %q", want, got)
	}
}

func TestMarkNavigationStaleInTxInstanceFailureAborts(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	id := uuid.NewString()
	rec.errAt = "navinstance:" + id
	if err := svc.markNavigationStaleInTx(context.Background(), nil, uuid.NewString(), id); err == nil {
		t.Fatal("实例按项标记失败必须让整笔写失败")
	}
	if got := strings.Join(rec.calls, ","); got != "nav:"+id+",navinstance:"+id {
		t.Fatalf("失败序列不符，实际 %q", got)
	}
}

func TestMarkNavigationStaleInTxInstanceZeroHitStillCalled(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	setAllPorts(svc, rec, 0)
	id := uuid.NewString()
	if err := svc.markNavigationStaleInTx(context.Background(), nil, uuid.NewString(), id); err != nil {
		t.Fatalf("零命中不该失败: %v", err)
	}
	if got := strings.Join(rec.calls, ","); got != "nav:"+id+",navinstance:"+id {
		t.Fatalf("零命中也要逐个调用，实际 %q", got)
	}
}

func TestValidateParentTxRejectsCycleAndStranger(t *testing.T) {
	svc := &Service{m: navigationmodel.NewModel(nil)}

	if err := svc.validateParentTx(context.Background(), nil, uuid.NewString(), "header", "", nil); err != nil {
		t.Fatalf("无父级（根节点）应直接放行: %v", err)
	}
}

func TestUpdateReqRejectsBeforeTx(t *testing.T) {
	svc, rec, tx := newMenuWriteService(t)
	for _, req := range []*navigationdto.UpdateReq{nil, {ID: "   "}} {
		if _, err := svc.Update(context.Background(), req); !errors.Is(err, navigationenums.Err(navigationenums.ErrInvalidParam)) {
			t.Fatalf("非法请求应返回参数错误，实际 %v", err)
		}
	}
	if tx.calls != 0 || len(rec.calls) != 0 {
		t.Fatalf("参数错误不该开事务或调端口，实际 tx=%d ports=%v", tx.calls, rec.calls)
	}
}

func TestCreateReqStillValidatesBeforeDB(t *testing.T) {
	svc := &Service{}
	_, err := svc.Create(context.Background(), &navigationdto.CreateReq{})
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrInvalidParam)) {
		t.Fatalf("空工程应返回参数错误且不开事务，实际 %v", err)
	}
}
