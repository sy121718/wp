package navigationservice

// navigation_write_tx_test.go — 菜单 CRUD 的同事务同锁闭环（纯逻辑契约）。
//
// 钉四件事（此前全部是静默不收敛）：
//  1. 写入口在端口缺失时 **fail closed**（不是「写进去但不派发失效」）；
//  2. 每个写入口都恰好开一次事务（写入与三类失效同进同出）；
//  3. 失效顺序 = 页标记 → 实例标记 → 入队（按位置），再按项键；任一步失败整笔失败；
//  4. 译文写路径与 CRUD 用的是**同一把锁**：由 withMenuWriteTx 的实现在两处共用约定，
//     这里断言 CRUD 也调了 i18n.LockProjectContentWrite（空转 tx 下 load 会走到它）；
//  5. Update 的一切判定都基于**锁内重读**的当前行，不基于锁外定位拿到的快照。
//
// 不碰 PG：事务桩直接执行闭包（回滚语义由 feature 测试验），锁与作用域两步走的是
// 「形状为真事务、后端空转」的句柄（newScopeOnlyTx，见 navigation_translation_write_test.go）。

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

// menuWriteRecorder 记录四类失效端口的调用（顺序敏感：页 → 实例 → 入队）。
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

// writePortsStub 四个同事务端口的最小实现（页位置 / 实例位置 / 项键 / 入队）。
//
// hits 是逐端口的命中数（默认 1）：实例侧「零命中」是**合法结果**，用例要能把它与
// 「根本没调用」区分开 —— 两者在线上都表现为「页面没变」，但前者正确、后者是缺陷。
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

// 编译期断言：桩的形状与 contract 声明的端口一致（签名漂移在这里断掉）。
var (
	_ navigationcontract.PageStalePort                 = writePortsStub{}
	_ navigationcontract.PresentationStalePort         = writePortsStub{}
	_ navigationcontract.MenuItemStalePort             = writePortsStub{}
	_ navigationcontract.MenuItemPresentationStalePort = writePortsStub{}
	_ navigationcontract.MenuStaleEnqueuePort          = writePortsStub{}
)

// newMenuWriteService 组装一个端口齐备、事务桩可用的 Service。
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

// setAllPorts 注入四个端口；hits>0 时所有端口都返回该命中数（0 = 各端口默认命中 1）。
func setAllPorts(svc *Service, rec *menuWriteRecorder, hits int64) {
	p := writePortsStub{rec: rec, hits: hits}
	svc.SetWritePorts(NavigationWriteDeps{Pages: p, Instances: p, Items: p, ItemInstances: p, Queue: p})
}

// TestMenuWriteFailsClosedWithoutPorts 端口缺失时写入口整体拒绝（不是「写进去但不派发」）。
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
			// 实例屏障缺失必须与缺页端口同样 fail closed：只标页面 + 入队的形态
			// 正是「旧编译晚到覆盖新 HTML」那一类。这条用例是主审退回的直接回归位。
			rec := &menuWriteRecorder{}
			s := &Service{m: navigationmodel.NewModel(nil)}
			p := writePortsStub{rec: rec}
			s.SetWritePorts(NavigationWriteDeps{Pages: p, Items: p, Queue: p})
			s.writeTx = &txRunnerStub{tx: newScopeOnlyTx(t)}
			return s
		}},
		{"缺按项实例端口", func() *Service {
			// 按项实例端口缺席同样 fail closed：只引用某一支的实例既不在位置键反查里，
			// 也没有别的屏障。缺它就不是「可选优化」，而是漏一类产物。
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

// TestWithMenuWriteTxLocksAndScopes 事务体在锁与作用域之内执行（两步都在同一 tx 上）。
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

// TestWithMenuWriteTxRejectsBadProject 工程参数非法时在动库之前拒绝。
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

// TestMarkMenuStaleInTxOrder 位置键失效顺序：先标手工页面，再入队（任一步失败即返回错误）。
func TestMarkMenuStaleInTxOrder(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	at := time.Unix(0, 0).UTC()
	if err := svc.markMenuStaleInTx(context.Background(), nil, uuid.NewString(), "header", at); err != nil {
		t.Fatalf("markMenuStaleInTx 失败: %v", err)
	}
	// 顺序：页面标记 → 实例标记 → 入队。实例标记必须夹在两者之间不是随便排的：
	// 它推进 input_version 后，即使旧编译此刻正在路上，激活谓词也会把它挡掉；
	// 而入队只是「提交后再重建一次」，晚一步不影响屏障成立。
	if got, want := strings.Join(rec.calls, ","), "menu:header,instance:header,queue:header"; got != want {
		t.Fatalf("失效顺序应为 %q，实际 %q", want, got)
	}

	// 入队失败 → 整笔失败（调用方事务回滚，绝不留下「页面已标、任务丢了」）。
	failSvc, failRec, _ := newMenuWriteService(t)
	failRec.errAt = "queue:header"
	if err := failSvc.markMenuStaleInTx(context.Background(), nil, uuid.NewString(), "header", at); err == nil {
		t.Fatal("入队失败必须让整笔写失败（否则译文/菜单已提交但没人重建）")
	}
}

// TestMarkMenuStaleInTxCallsInstancesOnZeroHit 「零命中」是合法结果，但**必须调用**。
//
// 主审退回的缺陷形态就是这个：把「新增位置命中不到实例」当成「不需要标记实例」，
// 于是连**已有位置**的实例也不再推进 input_version。零命中与不调用在返回值上一样
// （都是 0 行受影响），区别只在端口是否被调到 —— 所以判据必须落在调用记录上。
func TestMarkMenuStaleInTxCallsInstancesOnZeroHit(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	// 四个端口全部回报 0 命中（新位置、无实例依赖时的真实情形）。
	setAllPorts(svc, rec, 0)
	if err := svc.markMenuStaleInTx(context.Background(), nil, uuid.NewString(), "footer", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("零命中不该失败: %v", err)
	}
	want := "menu:footer,instance:footer,queue:footer"
	if got := strings.Join(rec.calls, ","); got != want {
		t.Fatalf("零命中也要逐个调用端口，顺序 %q，实际 %q", want, got)
	}
}

// TestMarkMenuStaleInTxInstanceFailureAborts 实例标记失败 → 整笔失败（不发队列、由调用方回滚）。
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

// TestMarkNavigationStaleInTxIgnoresEmptyID 空项 id 不调用端口（避免一次无意义的全表过滤）。
func TestMarkNavigationStaleInTxIgnoresEmptyID(t *testing.T) {
	svc, rec, _ := newMenuWriteService(t)
	if err := svc.markNavigationStaleInTx(context.Background(), nil, uuid.NewString(), "   "); err != nil {
		t.Fatalf("空项 id 应直接返回: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("空项 id 不该调用端口，实际 %v", rec.calls)
	}
}

// TestMarkNavigationStaleInTxPageThenInstance 按项失效顺序：手工页 → 自动实例。
//
// 两者是两类产物、两条依赖键（页面侧与实例侧各自的 navigation:{itemID} 行）。
// 实例侧**不能被位置键或页面标记替代**：core 的 UseNavigation 只记 navigation:{itemID}，
// 只引用这一支的实例产物位置反查一条都不命中。
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

// TestMarkNavigationStaleInTxInstanceFailureAborts 实例按项标记失败 → 整笔失败。
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

// TestMarkNavigationStaleInTxInstanceZeroHitStillCalled 零命中仍须调用实例按项端口。
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

// TestValidateParentTxRejectsCycleAndStranger 事务内父校验：不存在 / 成环一律 ErrInvalidParent。
func TestValidateParentTxRejectsCycleAndStranger(t *testing.T) {
	svc := &Service{m: navigationmodel.NewModel(nil)}
	// 行桩替身：ListTx 走真实 SQL，这里只验「参数与空父直接放行」这一条不需要库的判据。
	if err := svc.validateParentTx(context.Background(), nil, uuid.NewString(), "header", "", nil); err != nil {
		t.Fatalf("无父级（根节点）应直接放行: %v", err)
	}
}

// TestUpdateReqRejectsBeforeTx 参数级拒绝仍先于事务（零库访问、零端口调用）。
//
// Update 的锁外部分只允许做「定位 + 纯字符串校验」，任何需要库内当前值的判定都必须
// 留到锁内 —— 这条用例把「请求为空就该在动库之前返回」这半边钉住。
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

// TestCreateReqStillValidatesBeforeDB 参数与白名单校验仍先于事务（零库访问）。
func TestCreateReqStillValidatesBeforeDB(t *testing.T) {
	svc := &Service{}
	_, err := svc.Create(context.Background(), &navigationdto.CreateReq{})
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrInvalidParam)) {
		t.Fatalf("空工程应返回参数错误且不开事务，实际 %v", err)
	}
}
