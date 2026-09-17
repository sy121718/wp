package unit

// ensure_record_lang_test.go — page_artifacts 的语言维度（多语言 P3 前置，
// docs/06-D-site-i18n.md §15.5 第 1 条）。
//
// 覆盖：同页两语言各登记一行互不覆盖、同语言同版本仍为替换语义、
// 空语言兜底站点默认语言、按语言取行（GetByPageVersion）、响应回带 lang。

import (
	"context"
	"encoding/json"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactservice "go_wp/internal/module/artifact/service"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

// langManifest 构造带语言专属路径与文件闭包的 manifest。
func langManifest(path, fileHash string) json.RawMessage {
	return json.RawMessage(`{"canonicalPath":"` + path + `","files":{"index.html":"` + fileHash + `"}}`)
}

// langReq 在 validReq 基础上指定语言 / 版本 / 产物行 ID / hash / 产物 key。
func langReq(lang string, version int64, artifactID, artifactHash, artifactKey, path string) *artifactdto.RecordReq {
	req := validReq()
	req.Lang = lang
	req.Version = version
	req.ArtifactID = artifactID
	req.ArtifactHash = artifactHash
	req.ArtifactKey = artifactKey
	req.Manifest = langManifest(path, "content-"+artifactHash)
	return req
}

// rowsByLang 统计某页各语言的产物行（lang → 行数）。
func rowsByLang(t *testing.T, svc *artifactservice.Service, pageID string) map[string]int64 {
	t.Helper()
	var rows []struct {
		Lang string
		N    int64
	}
	if err := svc.Model().DB(context.Background()).Table("page_artifacts").
		Select("lang, COUNT(*) AS n").Where("page_id = ?", pageID).
		Group("lang").Scan(&rows).Error; err != nil {
		t.Fatalf("按语言统计产物行失败: %v", err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Lang] = r.N
	}
	return out
}

// TestArtifactEnsureRecordSamePageTwoLangsCoexist 同一页面同一草稿版本下，
// 两个语言各登记一行且互不覆盖（旧 UNIQUE(page_id, version) 下第二个语言会替换第一行）。
func TestArtifactEnsureRecordSamePageTwoLangsCoexist(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	zhReq := langReq("zh-CN", 1, testArtifactID, "hash-zh", "artifacts/hash-zh", "/zh-CN/about")
	zhResp, err := svc.EnsureRecord(ctx, zhReq)
	if err != nil {
		t.Fatalf("归档 zh-CN 产物失败: %v", err)
	}

	enReq := langReq("en-US", 1, testArtifactID2, "hash-en", "artifacts/hash-en", "/en-US/about")
	enResp, err := svc.EnsureRecord(ctx, enReq)
	if err != nil {
		t.Fatalf("归档 en-US 产物失败: %v", err)
	}

	// 两行并存，行 ID 与产物指针互不相同。
	if zhResp.ID == enResp.ID {
		t.Fatalf("两个语言的行 ID 不应相同: %s", zhResp.ID)
	}
	if zhResp.ArtifactHash == enResp.ArtifactHash {
		t.Fatalf("两个语言的产物 hash 不应相同: %s", zhResp.ArtifactHash)
	}
	if got := rowsByLang(t, svc, testPageID); got["zh-CN"] != 1 || got["en-US"] != 1 || len(got) != 2 {
		t.Fatalf("同页两语言应各一行，实际 %v", got)
	}

	// 先登记的语言行未被后续语言覆盖（核心回归点）。
	zhRow, err := svc.Model().GetByPageVersion(ctx, testPageID, 1, "zh-CN")
	if err != nil {
		t.Fatalf("按语言查询 zh-CN 行失败: %v", err)
	}
	if zhRow.ID != testArtifactID || zhRow.ArtifactHash != "hash-zh" || zhRow.ArtifactKey != "artifacts/hash-zh" {
		t.Fatalf("zh-CN 行被覆盖: id=%s hash=%s key=%s", zhRow.ID, zhRow.ArtifactHash, zhRow.ArtifactKey)
	}
	if zhRow.Lang != "zh-CN" {
		t.Fatalf("zh-CN 行 lang 错误: %q", zhRow.Lang)
	}
	enRow, err := svc.Model().GetByPageVersion(ctx, testPageID, 1, "en-US")
	if err != nil {
		t.Fatalf("按语言查询 en-US 行失败: %v", err)
	}
	if enRow.ID != testArtifactID2 || enRow.ArtifactHash != "hash-en" || enRow.ArtifactKey != "artifacts/hash-en" {
		t.Fatalf("en-US 行内容错误: id=%s hash=%s key=%s", enRow.ID, enRow.ArtifactHash, enRow.ArtifactKey)
	}

	// 响应回带语言：路由登记 artifact_id 时据此指向正确语言的产物。
	if zhResp.Lang != "zh-CN" || enResp.Lang != "en-US" {
		t.Fatalf("响应应回带语言: zh=%q en=%q", zhResp.Lang, enResp.Lang)
	}
}

// TestArtifactEnsureRecordSameLangSameVersionReplaces 同一语言同一版本重构建
// （编译器升级导致 hash 变化）仍是替换语义：行 ID 不变、指针更新、行数不增。
func TestArtifactEnsureRecordSameLangSameVersionReplaces(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first, err := svc.EnsureRecord(ctx, langReq("zh-CN", 1, testArtifactID, "hash-zh-v1", "artifacts/hash-zh-v1", "/zh-CN/about"))
	if err != nil {
		t.Fatalf("首次归档失败: %v", err)
	}
	// 另一语言先行登记：替换必须只发生在本语言内。
	if _, err = svc.EnsureRecord(ctx, langReq("en-US", 1, testArtifactID3, "hash-en", "artifacts/hash-en", "/en-US/about")); err != nil {
		t.Fatalf("归档 en-US 失败: %v", err)
	}

	second, err := svc.EnsureRecord(ctx, langReq("zh-CN", 1, testArtifactID2, "hash-zh-v2", "artifacts/hash-zh-v2", "/zh-CN/about"))
	if err != nil {
		t.Fatalf("同语言同版本重构建失败: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("同语言同版本应原地替换，行 ID 应保持 %s，实际 %s", first.ID, second.ID)
	}
	if second.ArtifactHash != "hash-zh-v2" || second.ArtifactKey != "artifacts/hash-zh-v2" {
		t.Fatalf("替换后指针未更新: hash=%s key=%s", second.ArtifactHash, second.ArtifactKey)
	}
	if got := rowsByLang(t, svc, testPageID); got["zh-CN"] != 1 || got["en-US"] != 1 || len(got) != 2 {
		t.Fatalf("替换后行数应保持 2（每语言一行），实际 %v", got)
	}
	// en-US 行未被替换波及。
	enRow, err := svc.Model().GetByPageVersion(ctx, testPageID, 1, "en-US")
	if err != nil {
		t.Fatalf("查询 en-US 行失败: %v", err)
	}
	if enRow.ID != testArtifactID3 || enRow.ArtifactHash != "hash-en" {
		t.Fatalf("en-US 行被同版本 zh-CN 重构建污染: id=%s hash=%s", enRow.ID, enRow.ArtifactHash)
	}
}

// TestArtifactEnsureRecordEmptyLangFallsBackToDefault 空语言（存量调用方）落库为
// 站点默认语言，绝不留空 —— 否则唯一键退化为 (page_id, version)。
func TestArtifactEnsureRecordEmptyLangFallsBackToDefault(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	defaultLang := i18n.GetDefaultLang()

	req := langReq("", 1, testArtifactID, "hash-default", "artifacts/hash-default", "/about")
	resp, err := svc.EnsureRecord(ctx, req)
	if err != nil {
		t.Fatalf("空语言归档失败: %v", err)
	}
	if resp.Lang != defaultLang {
		t.Fatalf("空语言应兜底为默认语言 %q，实际 %q", defaultLang, resp.Lang)
	}
	row, err := svc.Model().GetByPageVersion(ctx, testPageID, 1, defaultLang)
	if err != nil {
		t.Fatalf("按默认语言查询失败: %v", err)
	}
	if row.ID != testArtifactID || row.Lang != defaultLang {
		t.Fatalf("默认语言行错误: id=%s lang=%s", row.ID, row.Lang)
	}

	// 显式传默认语言与空语言命中同一行 → 归一化口径一致（不会各占一行）。
	explicit, err := svc.EnsureRecord(ctx, langReq(defaultLang, 1, testArtifactID2, "hash-default-2", "artifacts/hash-default-2", "/about"))
	if err != nil {
		t.Fatalf("显式默认语言归档失败: %v", err)
	}
	if explicit.ID != testArtifactID {
		t.Fatalf("显式默认语言应替换同一行，行 ID 应保持 %s，实际 %s", testArtifactID, explicit.ID)
	}
	if got := rowsByLang(t, svc, testPageID); len(got) != 1 || got[defaultLang] != 1 {
		t.Fatalf("归一化后应只有默认语言一行，实际 %v", got)
	}
}

// TestArtifactGetByPageVersionLangScoped 按语言取行：语言维度参与查询，
// 不存在的语言返回 ErrRecordNotFound（而不是随机命中其他语言的行）。
func TestArtifactGetByPageVersionLangScoped(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	if _, err := svc.EnsureRecord(ctx, langReq("zh-CN", 7, testArtifactID, "hash-zh-7", "artifacts/hash-zh-7", "/zh-CN/p")); err != nil {
		t.Fatalf("归档 zh-CN 失败: %v", err)
	}
	if _, err := svc.EnsureRecord(ctx, langReq("en-US", 7, testArtifactID2, "hash-en-7", "artifacts/hash-en-7", "/en-US/p")); err != nil {
		t.Fatalf("归档 en-US 失败: %v", err)
	}

	cases := []struct {
		lang string
		want string
	}{
		{"zh-CN", "hash-zh-7"},
		{"en-US", "hash-en-7"},
	}
	for _, c := range cases {
		row, err := svc.Model().GetByPageVersion(ctx, testPageID, 7, c.lang)
		if err != nil {
			t.Fatalf("查询 %s 行失败: %v", c.lang, err)
		}
		if row.ArtifactHash != c.want || row.Lang != c.lang {
			t.Fatalf("%s 行错配: hash=%s lang=%s", c.lang, row.ArtifactHash, row.Lang)
		}
	}
	if _, err := svc.Model().GetByPageVersion(ctx, testPageID, 7, "ja-JP"); err != gorm.ErrRecordNotFound {
		t.Fatalf("未登记语言应返回 ErrRecordNotFound，实际 %v", err)
	}
}
