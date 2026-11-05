package publish

import (
	"errors"
	"strings"
	"testing"
)

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

func stagedPublisher(path, stagedHash string) (*Publisher, *fakeStore, *fakePub) {
	store := newFakeStore()
	pub := newFakePub()
	if stagedHash != "" {
		store.arts[stagedHash] = &Artifact{Hash: stagedHash, CanonicalPath: path}
	}
	p := NewPublisher(store, pub, func([]byte, string) (map[string][]byte, *ManifestAccess, error) { return nil, nil, nil })
	p.pages["p1"] = &PageRecord{
		ID: "p1", Path: path, Version: 1, Status: StateReady,
		DocumentJSON: []byte(`{"root":[]}`), StagedHash: stagedHash,
	}
	return p, store, pub
}

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

func TestStageArtifactRejectsPathMismatch(t *testing.T) {
	p, store, _ := stagedPublisher("/about", "h-old")
	store.arts["h-old"].CanonicalPath = "/legacy"

	if _, err := p.StageArtifact("p1"); err == nil {
		t.Fatal("路径不一致必须拒绝激活")
	}
}

func TestStageArtifactWithoutStaged(t *testing.T) {
	p, _, _ := stagedPublisher("/about", "")

	_, err := p.StageArtifact("p1")
	if !errors.Is(err, ErrNoStagedArtifact) {
		t.Fatalf("期望 ErrNoStagedArtifact，实际 %v", err)
	}
}

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
