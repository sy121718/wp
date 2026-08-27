package unit

import (
	"os"
	"path/filepath"
	"testing"

	"go_wp/internal/pipeline"
)

// TestLocalStorePutGetVerify 内容寻址写入/读取/校验闭环。
func TestLocalStorePutGetVerify(t *testing.T) {
	s := &pipeline.LocalStore{Root: t.TempDir()}
	a, err := pipeline.NewArtifact([]byte("<html>v1</html>"), baseManifest())
	if err != nil {
		t.Fatalf("构建产物失败: %v", err)
	}
	loc, err := s.PutArtifact(a)
	if err != nil {
		t.Fatalf("PutArtifact 失败: %v", err)
	}
	if got, err := s.GetArtifact(loc); err != nil {
		t.Fatalf("GetArtifact 失败: %v", err)
	} else if got.Hash != a.Hash || string(got.Entries["index.html"]) != "<html>v1</html>" {
		t.Errorf("读回的产物不一致: %+v", got)
	}
	if err := s.VerifyArtifact(loc, a.Hash); err != nil {
		t.Errorf("VerifyArtifact 失败: %v", err)
	}
	if err := s.VerifyArtifact(loc, "deadbeef"); err == nil {
		t.Error("错误 hash 校验应失败")
	}
}

// TestLocalStorePutIdempotent 重复写入同 hash 幂等返回，不覆盖历史文件。
func TestLocalStorePutIdempotent(t *testing.T) {
	root := t.TempDir()
	s := &pipeline.LocalStore{Root: root}
	a, _ := pipeline.NewArtifact([]byte("<html>v1</html>"), baseManifest())
	loc1, err := s.PutArtifact(a)
	if err != nil {
		t.Fatalf("PutArtifact 失败: %v", err)
	}
	loc2, err := s.PutArtifact(a)
	if err != nil {
		t.Fatalf("重复 PutArtifact 失败: %v", err)
	}
	if loc1 != loc2 {
		t.Errorf("幂等写入应返回相同定位器: %+v vs %+v", loc1, loc2)
	}
	// 文件仍在且内容未变。
	data, err := os.ReadFile(filepath.Join(root, loc1.Key, "index.html"))
	if err != nil || string(data) != "<html>v1</html>" {
		t.Errorf("历史文件应保持不变: %v %q", err, data)
	}
}

// TestLocalStoreDeleteArtifact 删除保护：hash 不符拒绝，删除幂等。
func TestLocalStoreDeleteArtifact(t *testing.T) {
	s := &pipeline.LocalStore{Root: t.TempDir()}
	a, _ := pipeline.NewArtifact([]byte("<html>v1</html>"), baseManifest())
	loc, _ := s.PutArtifact(a)

	if err := s.DeleteArtifact(loc, "wrong-hash"); err == nil {
		t.Error("hash 不符删除应被拒绝")
	}
	if err := s.DeleteArtifact(loc, a.Hash); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if err := s.DeleteArtifact(loc, a.Hash); err != nil {
		t.Errorf("重复删除应幂等: %v", err)
	}
}

// TestLocalStoreRedirectRoundTrip 重定向产物写入/读取/删除。
func TestLocalStoreRedirectRoundTrip(t *testing.T) {
	s := &pipeline.LocalStore{Root: t.TempDir()}
	r, _ := pipeline.NewRedirectArtifact("/new-url", 301)
	loc, err := s.PutRedirect(r)
	if err != nil {
		t.Fatalf("PutRedirect 失败: %v", err)
	}
	got, err := s.GetRedirect(loc)
	if err != nil {
		t.Fatalf("GetRedirect 失败: %v", err)
	}
	if got.Directive.TargetPath != "/new-url" || got.Directive.StatusCode != 301 || got.Hash != r.Hash {
		t.Errorf("读回重定向产物不一致: %+v", got)
	}
	if err := s.DeleteRedirect(loc, r.Hash); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if err := s.DeleteRedirect(loc, r.Hash); err != nil {
		t.Errorf("重复删除应幂等: %v", err)
	}
}

// TestLocalStoreDeleteObject 单文件删除幂等。
func TestLocalStoreDeleteObject(t *testing.T) {
	s := &pipeline.LocalStore{Root: t.TempDir()}
	a, _ := pipeline.NewArtifact([]byte("<html>x</html>"), baseManifest())
	loc, _ := s.PutArtifact(a)
	if err := s.DeleteArtifactObject(loc, "index.html"); err != nil {
		t.Fatalf("删除 index.html 失败: %v", err)
	}
	if err := s.DeleteArtifactObject(loc, "index.html"); err != nil {
		t.Errorf("重复删除应幂等: %v", err)
	}
	if err := s.DeleteArtifactObject(loc, "../escape"); err == nil {
		t.Error("路径穿越应被拒绝")
	}
}

// TestLocalStoreGetMissing 读取不存在产物返回错误。
func TestLocalStoreGetMissing(t *testing.T) {
	s := &pipeline.LocalStore{Root: t.TempDir()}
	if _, err := s.GetArtifact(pipeline.Locator{Provider: "local", Key: "artifacts/none"}); err == nil {
		t.Error("读取不存在产物应报错")
	}
}
