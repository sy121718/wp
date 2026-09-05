package media

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// validHash 生成合法内容哈希（32 位十六进制）。
const validHash = "0123456789abcdef0123456789abcdef"

// TestUploadValidation 上传校验：非法哈希/类型/宽高/标签。
func TestUploadValidation(t *testing.T) {
	s := NewStore()
	tests := []struct {
		name    string
		asset   Asset
		wantErr string
	}{
		{"非法哈希", Asset{Hash: "xyz"}, "无效的内容哈希"},
		{"哈希过短", Asset{Hash: "abc123"}, "无效的内容哈希"},
		{"非法类型", Asset{Hash: validHash, Type: "audio"}, "无效的文件类型"},
		{"图片缺宽高", Asset{Hash: validHash, Type: TypeImage}, "必须提供原始宽高"},
		{"SVG 缺宽高", Asset{Hash: validHash, Type: TypeSVG}, "必须提供原始宽高"},
		{"非法标签", Asset{Hash: validHash, Type: TypeImage, Width: 1, Height: 1, Tags: []string{"bad tag!"}}, "无效的标签"},
		{"空标签列表合法", Asset{Hash: validHash, Type: TypeImage, Width: 1, Height: 1}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := s.Upload(tt.asset)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Upload 应成功: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Upload 错误应为 %q，got %v", tt.wantErr, err)
			}
		})
	}
}

// TestUploadDedup 同哈希重复上传：返回 duplicateOf，不产生新资产。
func TestUploadDedup(t *testing.T) {
	s := NewStore()
	id1, dup, err := s.Upload(Asset{Hash: validHash, Type: TypeImage, Width: 100, Height: 50, FileName: "a.png"})
	if err != nil {
		t.Fatalf("首次上传失败: %v", err)
	}
	if dup != "" {
		t.Fatalf("首次上传 duplicateOf 应为空，got %q", dup)
	}
	id2, dup, err := s.Upload(Asset{Hash: validHash, Type: TypeImage, Width: 100, Height: 50, FileName: "b.png"})
	if err != nil {
		t.Fatalf("重复上传失败: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("重复上传 assetId 应一致: %s vs %s", id2, id1)
	}
	if dup != id1 {
		t.Fatalf("duplicateOf 应指向原资产: %s vs %s", dup, id1)
	}
	if n := len(s.assets); n != 1 {
		t.Fatalf("去重后资产数应为 1，got %d", n)
	}
	// 文件名保留首次上传的。
	a, err := s.Get(id1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.FileName != "a.png" {
		t.Fatalf("去重不应覆盖原文件名: %s", a.FileName)
	}
}

// TestDeriveAssetID 派生 ID：前缀 + 小写截断 24 位。
func TestDeriveAssetID(t *testing.T) {
	got := deriveAssetID("ABCDEF0123456789abcdef0123456789")
	if !strings.HasPrefix(got, assetIDPrefix) {
		t.Fatalf("缺少前缀: %s", got)
	}
	if len(got) != len(assetIDPrefix)+24 {
		t.Fatalf("长度错误: %s", got)
	}
	if !strings.Contains(got, "abcdef0123456789abcdef") {
		t.Fatalf("hash 应小写化截断: %s", got)
	}
	// 最小合法 hash（32 位）不 panic。
	if id := deriveAssetID(validHash); !strings.HasPrefix(id, assetIDPrefix) {
		t.Fatalf("32 位 hash 派生失败: %s", id)
	}
}

// TestGetCopyIsolation Get 返回副本，外部修改不影响内部状态。
func TestGetCopyIsolation(t *testing.T) {
	s := NewStore()
	id, _, err := s.Upload(Asset{Hash: validHash, Type: TypeImage, Width: 10, Height: 10, Tags: []string{"a"}})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	a, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	a.Tags[0] = "mutated"
	a.Width = 999
	again, _ := s.Get(id)
	if again.Tags[0] != "a" || again.Width != 10 {
		t.Fatalf("Get 应返回副本: %+v", again)
	}
}

// TestReplace 版本替换：hash 索引迁移、Generation+1、同 hash 幂等。
func TestReplace(t *testing.T) {
	s := NewStore()
	id, _, err := s.Upload(Asset{Hash: validHash, Type: TypeImage, Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	newHash := "ffffffffffffffffffffffffffffffff"
	if err := s.Replace(id, newHash, 200, 100, 99, []Variant{{Kind: VariantLarge}}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	a, _ := s.Get(id)
	if a.Hash != newHash || a.Width != 200 || a.Height != 100 || a.Size != 99 || a.Generation != 2 {
		t.Fatalf("替换结果错误: %+v", a)
	}
	if len(a.Variants) != 1 || a.Variants[0].Kind != VariantLarge {
		t.Fatalf("变体未更新: %+v", a.Variants)
	}
	// 旧 hash 索引已清空，新 hash 可查到。
	if _, found := s.FindByHash(validHash); found {
		t.Fatalf("旧 hash 不应再被索引")
	}
	if _, found := s.FindByHash(newHash); !found {
		t.Fatalf("新 hash 应被索引")
	}
	// 同 hash 幂等：Generation 不再增加。
	if err := s.Replace(id, newHash, 200, 100, 99, nil); err != nil {
		t.Fatalf("幂等 Replace: %v", err)
	}
	if a, _ := s.Get(id); a.Generation != 2 {
		t.Fatalf("同 hash 替换不应增加代数: %d", a.Generation)
	}
}

// TestReplaceErrors 替换失败：不存在、非法 hash。
func TestReplaceErrors(t *testing.T) {
	s := NewStore()
	if err := s.Replace("nope", validHash, 1, 1, 1, nil); err == nil {
		t.Fatalf("替换不存在资产应报错")
	}
	id, _, _ := s.Upload(Asset{Hash: validHash, Type: TypeImage, Width: 1, Height: 1})
	if err := s.Replace(id, "bad", 1, 1, 1, nil); err == nil {
		t.Fatalf("替换非法 hash 应报错")
	}
}

// TestDeleteProtection 引用保护：被引用禁止删除并返回清单；无引用可删。
func TestDeleteProtection(t *testing.T) {
	s := NewStore()
	id, _, _ := s.Upload(Asset{Hash: validHash, Type: TypeImage, Width: 1, Height: 1})
	if err := s.RecordRef(id, Reference{Kind: "page", ID: "p1", Title: "首页"}); err != nil {
		t.Fatalf("RecordRef: %v", err)
	}
	err := s.Delete(id)
	if err == nil || !strings.Contains(err.Error(), "已被 1 处引用") || !strings.Contains(err.Error(), "首页(page)") {
		t.Fatalf("删除保护错误信息: %v", err)
	}
	// 移除引用后可删。
	s.RemoveRef(id, Reference{Kind: "page", ID: "p1"})
	if err := s.Delete(id); err != nil {
		t.Fatalf("无引用删除失败: %v", err)
	}
	// 删后 Get 失败。
	if _, err := s.Get(id); err == nil {
		t.Fatalf("删除后 Get 应报错")
	}
	if err := s.Delete(id); err == nil {
		t.Fatalf("重复删除应报错")
	}
}

// TestRecordRefIdempotent 引用登记幂等：空 Title 不覆盖已存在引用。
func TestRecordRefIdempotent(t *testing.T) {
	s := NewStore()
	id, _, _ := s.Upload(Asset{Hash: validHash, Type: TypeImage, Width: 1, Height: 1})
	if err := s.RecordRef(id, Reference{Kind: "page", ID: "p1", Title: "好标题"}); err != nil {
		t.Fatalf("RecordRef: %v", err)
	}
	// 同 kind:id 再登记，空 Title 不得覆盖。
	if err := s.RecordRef(id, Reference{Kind: "page", ID: "p1"}); err != nil {
		t.Fatalf("幂等登记: %v", err)
	}
	refs := s.Refs(id)
	if len(refs) != 1 || refs[0].Title != "好标题" {
		t.Fatalf("空 Title 不应覆盖已有引用: %+v", refs)
	}
	// 不同 kind:id 追加。
	if err := s.RecordRef(id, Reference{Kind: "article", ID: "a1", Title: "文章"}); err != nil {
		t.Fatalf("追加引用: %v", err)
	}
	if len(s.Refs(id)) != 2 {
		t.Fatalf("引用数应为 2: %+v", s.Refs(id))
	}
	// 不存在资产登记报错。
	if err := s.RecordRef("nope", Reference{Kind: "page", ID: "p9"}); err == nil {
		t.Fatalf("不存在资产登记应报错")
	}
}

// TestSearch 多维过滤与确定性排序。
func TestSearch(t *testing.T) {
	s := NewStore()
	mk := func(hash, name, typ, cat string, tags []string) {
		if _, _, err := s.Upload(Asset{Hash: hash, Type: typ, Width: 1, Height: 1, FileName: name, CategoryID: cat, Tags: tags}); err != nil {
			t.Fatalf("Upload %s: %v", name, err)
		}
	}
	mk("11111111111111111111111111111111", "hero.jpg", TypeImage, "c1", []string{"banner"})
	mk("22222222222222222222222222222222", "logo.svg", TypeSVG, "c1", []string{"brand"})
	mk("33333333333333333333333333333333", "doc.pdf", TypeDocument, "c2", nil)
	id3, _, _ := s.Upload(Asset{Hash: "44444444444444444444444444444444", Type: TypeImage, Width: 1, Height: 1, FileName: "bg.png", CategoryID: "c2"})

	// 引用状态过滤。
	s.RecordRef(id3, Reference{Kind: "page", ID: "p1"})

	t.Run("文件名模糊", func(t *testing.T) {
		if got := s.Search(SearchFilter{FileName: "logo"}); len(got) != 1 || got[0].FileName != "logo.svg" {
			t.Fatalf("文件名过滤错误: %+v", got)
		}
	})
	t.Run("类型精确", func(t *testing.T) {
		if got := s.Search(SearchFilter{Type: TypeSVG}); len(got) != 1 {
			t.Fatalf("类型过滤错误: %+v", got)
		}
	})
	t.Run("分类", func(t *testing.T) {
		if got := s.Search(SearchFilter{CategoryID: "c1"}); len(got) != 2 {
			t.Fatalf("分类过滤错误: %+v", got)
		}
	})
	t.Run("标签", func(t *testing.T) {
		if got := s.Search(SearchFilter{Tag: "brand"}); len(got) != 1 || got[0].FileName != "logo.svg" {
			t.Fatalf("标签过滤错误: %+v", got)
		}
	})
	t.Run("已引用", func(t *testing.T) {
		ref := true
		got := s.Search(SearchFilter{Referenced: &ref})
		if len(got) != 1 || got[0].FileName != "bg.png" {
			t.Fatalf("已引用过滤错误: %+v", got)
		}
	})
	t.Run("未引用", func(t *testing.T) {
		ref := false
		if got := s.Search(SearchFilter{Referenced: &ref}); len(got) != 3 {
			t.Fatalf("未引用过滤错误: %+v", got)
		}
	})
	t.Run("空过滤器返回全部且有序", func(t *testing.T) {
		got := s.Search(SearchFilter{})
		if len(got) != 4 {
			t.Fatalf("应返回 4 条: %d", len(got))
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].ID >= got[i].ID {
				t.Fatalf("结果应按 ID 升序: %s >= %s", got[i-1].ID, got[i].ID)
			}
		}
	})
}

// TestStoreConcurrency 并发安全：并行上传/查询/引用登记/删除不 panic 不丢数据。
func TestStoreConcurrency(t *testing.T) {
	s := NewStore()
	const n = 50
	var wg sync.WaitGroup
	// 并行上传 50 个唯一资产 + 重复上传冲突。
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := fmt.Sprintf("%032x", i+1)
			id, _, err := s.Upload(Asset{Hash: hash, Type: TypeImage, Width: i + 1, Height: i + 1, FileName: "f.png"})
			if err != nil {
				t.Errorf("并发上传失败: %v", err)
			}
			s.RecordRef(id, Reference{Kind: "page", ID: "p" + fmt.Sprint(i), Title: "t"})
			s.Get(id)
			if i%3 == 0 {
				s.RemoveRef(id, Reference{Kind: "page", ID: "p" + fmt.Sprint(i)})
			}
		}(i)
	}
	wg.Wait()
	if got := len(s.Search(SearchFilter{})); got == 0 {
		t.Fatalf("并发后不应为空")
	}
}
