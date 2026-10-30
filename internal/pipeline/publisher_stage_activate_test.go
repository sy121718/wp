package pipeline

// publisher_stage_activate_test.go — Publish 拆成 StageArtifact / ActivateStagedArtifact 之后的
// 可观察契约（本 owner 的激活闸门改动依赖这几条）。
//
// 拆分的目的只有一个：让调用方能在「校验暂存产物」与「切访问面」之间插入一次数据库判定
//（实体行锁 + 输入代次比对）。所以这几条必须钉死：
//   - StageArtifact 是**读**，不动访问面、不改记录状态 —— 否则插在它前面的判定白做；
//   - ActivateStagedArtifact 失败时线上保持原样 —— 否则「判定通过 → 激活失败」会留下
//     一个数据库说没切、磁盘说切了的半截状态；
//   - Publish 仍是两步的组合 —— 既有调用点的语义不能因为拆分而变；
//   - 内核不认识「任务/租约」：租约只保护 job，不代表激活闸门。

import (
	"errors"
	"strings"
	"testing"
)

// fakeStore 只实现发布路径真正用到的方法；其余是空实现。
type fakeStore struct {
	arts   map[string]*Artifact
	getErr error
}

func newFakeStore() *fakeStore { return &fakeStore{arts: map[string]*Artifact{}} }

func (s *fakeStore) PutArtifact(a *Artifact) (Locator, error) {
	s.arts[a.Hash] = a
	return ArtifactLocator(a.Hash), nil
}

func (s *fakeStore) GetArtifact(loc Locator) (*Artifact, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	hash := strings.TrimPrefix(loc.Key, "artifacts/")
	a, ok := s.arts[hash]
	if !ok {
		return nil, errors.New("产物不存在: " + hash)
	}
	return a, nil
}

func (s *fakeStore) VerifyArtifact(loc Locator, expectedHash string) error {
	_, err := s.GetArtifact(loc)
	return err
}

func (s *fakeStore) DeleteArtifact(Locator, string) error       { return nil }
func (s *fakeStore) DeleteArtifactObject(Locator, string) error { return nil }
func (s *fakeStore) PutRedirect(*RedirectArtifact) (Locator, error) {
	return Locator{}, nil
}
func (s *fakeStore) GetRedirect(Locator) (*RedirectArtifact, error) {
	return nil, errors.New("未实现")
}
func (s *fakeStore) DeleteRedirect(Locator, string) error { return nil }

// fakePub 记录访问面切换次数与当前激活目标，并可注入失败。
type fakePub struct {
	active      map[string]string
	activate    int
	deactive    int
	activateErr error
}

func newFakePub() *fakePub { return &fakePub{active: map[string]string{}} }

func (p *fakePub) Activate(path string, loc Locator) error {
	if p.activateErr != nil {
		return p.activateErr
	}
	p.activate++
	p.active[path] = loc.Key
	return nil
}

func (p *fakePub) Deactivate(path string) error {
	p.deactive++
	delete(p.active, path)
	return nil
}

func (p *fakePub) Inspect(path string) (*PublicationState, error) {
	h, ok := p.active[path]
	if !ok {
		return &PublicationState{Kind: PublicationNone}, nil
	}
	loc := Locator{Provider: "local", Key: h}
	return &PublicationState{Kind: PublicationPage, Locator: &loc}, nil
}

// stagedPublisher 造一个「已有一份暂存产物、尚未激活」的发布器。
func stagedPublisher(path, stagedHash string) (*Publisher, *fakeStore, *fakePub) {
	store := newFakeStore()
	pub := newFakePub()
	if stagedHash != "" {
		store.arts[stagedHash] = &Artifact{Hash: stagedHash, CanonicalPath: path}
	}
	p := NewPublisher(store, pub)
	p.pages["p1"] = &PageRecord{
		ID: "p1", Path: path, Version: 1, Status: StateReady,
		DocumentJSON: []byte(`{"root":[]}`), StagedHash: stagedHash,
	}
	return p, store, pub
}

// TestStageArtifactIsReadOnly StageArtifact 只校验并回读哈希，不动访问面、不改状态。
func TestStageArtifactIsReadOnly(t *testing.T) {
	p, _, pub := stagedPublisher("/about", "h-new")

	hash, err := p.StageArtifact("p1")
	if err != nil {
		t.Fatalf("StageArtifact 不应失败：%v", err)
	}
	if hash != "h-new" {
		t.Fatalf("应回读暂存产物哈希 h-new，实际 %q", hash)
	}
	if pub.activate != 0 {
		t.Fatalf("StageArtifact 不得切换访问面（调用方还没做数据库判定），实际切换 %d 次", pub.activate)
	}
	if rec := p.pages["p1"]; rec.ActiveHash != "" || rec.Status != StateReady {
		t.Fatalf("StageArtifact 不得改记录状态：active=%q status=%q", rec.ActiveHash, rec.Status)
	}
}

// TestStageArtifactRejectsPathMismatch 暂存产物绑定的路径与当前路径不一致时拒绝。
// 这是「改了 URL 但没重建」的唯一拦截点：放行会把旧路径的产物挂到新 URL 上。
func TestStageArtifactRejectsPathMismatch(t *testing.T) {
	p, store, _ := stagedPublisher("/about", "h-old")
	store.arts["h-old"].CanonicalPath = "/legacy"

	if _, err := p.StageArtifact("p1"); err == nil {
		t.Fatal("路径不一致必须拒绝激活")
	}
}

// TestStageArtifactWithoutStaged 无暂存产物时返回 ErrNoStagedArtifact。
func TestStageArtifactWithoutStaged(t *testing.T) {
	p, _, _ := stagedPublisher("/about", "")

	_, err := p.StageArtifact("p1")
	if !errors.Is(err, ErrNoStagedArtifact) {
		t.Fatalf("期望 ErrNoStagedArtifact，实际 %v", err)
	}
}

// TestActivateStagedArtifactSwitchesAndMarksPublished 激活成功：访问面切到暂存产物，记录转 published。
func TestActivateStagedArtifactSwitchesAndMarksPublished(t *testing.T) {
	p, _, pub := stagedPublisher("/about", "h-new")

	if err := p.ActivateStagedArtifact("p1"); err != nil {
		t.Fatalf("激活不应失败：%v", err)
	}
	if pub.active["/about"] != "artifacts/h-new" {
		t.Fatalf("访问面应指向新产物，实际 %q", pub.active["/about"])
	}
	rec := p.pages["p1"]
	if rec.ActiveHash != "h-new" || rec.Status != StatePublished {
		t.Fatalf("激活后应为 published/h-new，实际 %q/%q", rec.Status, rec.ActiveHash)
	}
}

// TestActivateStagedArtifactFailureLeavesFaceIntact 激活失败：线上保持不变。
// 调用方据此把「FS 没切」与「FS 切了但 DB 失败」区分开 —— 两条路的补偿完全不同。
func TestActivateStagedArtifactFailureLeavesFaceIntact(t *testing.T) {
	p, _, pub := stagedPublisher("/about", "h-new")
	pub.activateErr = errors.New("rename 失败")

	if err := p.ActivateStagedArtifact("p1"); err == nil {
		t.Fatal("激活失败必须返回错误")
	}
	if _, ok := pub.active["/about"]; ok {
		t.Fatal("激活失败时访问面必须保持原样")
	}
	if rec := p.pages["p1"]; rec.ActiveHash != "" {
		t.Fatalf("激活失败时记录不得指向该产物，实际 %q", rec.ActiveHash)
	}
}

// TestPublishKeepsTwoStepSemantics 旧 Publish 仍是两步组合：先校验后切换，返回激活后的哈希。
func TestPublishKeepsTwoStepSemantics(t *testing.T) {
	p, _, pub := stagedPublisher("/about", "h-new")

	hash, err := p.Publish("p1")
	if err != nil {
		t.Fatalf("Publish 不应失败：%v", err)
	}
	if hash != "h-new" || pub.activate != 1 {
		t.Fatalf("Publish 应校验并切换一次：hash=%q activate=%d", hash, pub.activate)
	}
}

// TestPublishStopsBeforeActivateOnStageFailure 校验失败时不得进入切换。
func TestPublishStopsBeforeActivateOnStageFailure(t *testing.T) {
	p, store, pub := stagedPublisher("/about", "h-new")
	store.getErr = errors.New("产物读取失败")

	if _, err := p.Publish("p1"); err == nil {
		t.Fatal("校验失败必须返回错误")
	}
	if pub.activate != 0 {
		t.Fatalf("校验失败不得切换访问面，实际切换 %d 次", pub.activate)
	}
}

// TestActivateIsNotGatedByLeaseOrJob 内核不持有任何「任务租约」概念：同一份暂存产物
// 可被连续激活两次。租约只保护 job，不代表激活闸门 —— 闸门在调用方的实体行锁与输入代次上。
func TestActivateIsNotGatedByLeaseOrJob(t *testing.T) {
	p, _, pub := stagedPublisher("/about", "h-new")
	for i := 0; i < 2; i++ {
		if err := p.ActivateStagedArtifact("p1"); err != nil {
			t.Fatalf("第 %d 次激活不应失败：%v", i+1, err)
		}
	}
	if pub.activate != 2 {
		t.Fatalf("内核只按「切换成功」计数，不按任务身份去重，实际 %d 次", pub.activate)
	}
}
