package uploadprovider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// newTempLocalProvider 返回一个根目录落在临时目录的 local provider。
func newTempLocalProvider(t *testing.T) (Provider, string) {
	t.Helper()
	dir := t.TempDir()
	v := viper.New()
	v.Set("upload.local_dir", dir)
	p := NewLocalProvider()
	if err := p.Init(v); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, dir
}

func uploadBody(p Provider, key string, body string) (Result, error) {
	return p.Upload(context.Background(), RuntimeConfig{}, File{
		Filename: "x.jpg",
		Reader:   strings.NewReader(body),
		Size:     int64(len(body)),
	}, Request{ObjectKey: key})
}

// 调用方显式指定键、磁盘上已有同名的上一代文件时：既不能覆盖，也不该让整次上传失败。
// 场景来源：库表重置后 sys_attachment 主键从 1 重来，而 public/storage 目录长期累积，
// 媒体库的 <id>.<ext> 与遗留文件撞名（实测 3495 个遗留文件里大量 id 复用）。
func TestUploadExplicitKeyCollisionKeepsPreviousFile(t *testing.T) {
	p, dir := newTempLocalProvider(t)

	old := filepath.Join(dir, "1.jpg")
	if err := os.WriteFile(old, []byte("previous-generation"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := uploadBody(p, "1.jpg", "fresh")
	if err != nil {
		t.Fatalf("键冲突时不应失败: %v", err)
	}
	if res.Key == "1.jpg" {
		t.Fatalf("冲突时应换键，实际仍返回 %q", res.Key)
	}
	if !strings.HasPrefix(res.Key, "1-") || !strings.HasSuffix(res.Key, ".jpg") {
		t.Fatalf("相邻键应形如 1-<rand>.jpg，实际 %q", res.Key)
	}

	if got, _ := os.ReadFile(old); string(got) != "previous-generation" {
		t.Fatalf("遗留文件被改写: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, res.Key)); string(got) != "fresh" {
		t.Fatalf("新文件内容不对: %q", got)
	}
	if !strings.HasSuffix(res.URL, "/"+res.Key) {
		t.Fatalf("URL 必须跟着实际落盘键走，实际 %q / key %q", res.URL, res.Key)
	}
}

// 无人指定键（PreserveName 同名）时语义不变：同名二次上传必须失败，否则用户以为覆盖了旧的。
func TestUploadPreserveNameKeepsFailureOnDuplicate(t *testing.T) {
	p, dir := newTempLocalProvider(t)

	if err := os.WriteFile(filepath.Join(dir, "same.jpg"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := p.Upload(context.Background(), RuntimeConfig{}, File{
		Filename: "same.jpg",
		Reader:   strings.NewReader("new"),
		Size:     3,
	}, Request{PreserveName: true})
	if err == nil {
		t.Fatal("同名二次上传应失败")
	}
	if !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("错误信息应说明已存在，实际 %q", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "same.jpg")); string(got) != "old" {
		t.Fatalf("同名失败时不得改写原文件: %q", got)
	}
}

// 子目录里的键冲突：相邻键必须留在同一目录，不能掉到根目录。
func TestCollisionKeyKeepsDirectory(t *testing.T) {
	p, dir := newTempLocalProvider(t)

	sub := filepath.Join(dir, "image")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "7.png"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := uploadBody(p, "image/7.png", "fresh")
	if err != nil {
		t.Fatalf("子目录键冲突不应失败: %v", err)
	}
	if !strings.HasPrefix(res.Key, "image/7-") {
		t.Fatalf("相邻键应留在 image/ 下，实际 %q", res.Key)
	}
	if _, statErr := os.Stat(filepath.Join(dir, res.Key)); statErr != nil {
		t.Fatalf("文件未落在预期位置 %q: %v", res.Key, statErr)
	}
}

// 越界防护：随机后缀不改变路径归属，不允许因为换键跑到根目录之外。
func TestCollisionKeyStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	key, target, err := collisionKey(root, "a/b.jpg")
	if err != nil {
		t.Fatalf("collisionKey: %v", err)
	}
	if !strings.HasPrefix(key, "a/b-") {
		t.Fatalf("键形状不对: %q", key)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("目标越界: rel=%q err=%v", rel, err)
	}
}
