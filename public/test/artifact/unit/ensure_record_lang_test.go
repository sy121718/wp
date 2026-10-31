package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactservice "go_wp/internal/module/artifact/service"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

func langManifest(path, fileHash string) json.RawMessage {
	return json.RawMessage(`{"canonicalPath":"` + path + `","files":{"index.html":"` + fileHash + `"}}`)
}

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

	if zhResp.ID == enResp.ID {
		t.Fatalf("两个语言的行 ID 不应相同: %s", zhResp.ID)
	}
	if zhResp.ArtifactHash == enResp.ArtifactHash {
		t.Fatalf("两个语言的产物 hash 不应相同: %s", zhResp.ArtifactHash)
	}
	if got := rowsByLang(t, svc, testPageID); got["zh-CN"] != 1 || got["en-US"] != 1 || len(got) != 2 {
		t.Fatalf("同页两语言应各一行，实际 %v", got)
	}

	zhRow, err := svc.Model().GetByHash(ctx, testPageID, "hash-zh")
	if err != nil {
		t.Fatalf("按语言查询 zh-CN 行失败: %v", err)
	}
	if zhRow.ID != testArtifactID || zhRow.ArtifactHash != "hash-zh" || zhRow.ArtifactKey != "artifacts/hash-zh" {
		t.Fatalf("zh-CN 行被覆盖: id=%s hash=%s key=%s", zhRow.ID, zhRow.ArtifactHash, zhRow.ArtifactKey)
	}
	if zhRow.Lang != "zh-CN" {
		t.Fatalf("zh-CN 行 lang 错误: %q", zhRow.Lang)
	}
	enRow, err := svc.Model().GetByHash(ctx, testPageID, "hash-en")
	if err != nil {
		t.Fatalf("按语言查询 en-US 行失败: %v", err)
	}
	if enRow.ID != testArtifactID2 || enRow.ArtifactHash != "hash-en" || enRow.ArtifactKey != "artifacts/hash-en" {
		t.Fatalf("en-US 行内容错误: id=%s hash=%s key=%s", enRow.ID, enRow.ArtifactHash, enRow.ArtifactKey)
	}

	if zhResp.Lang != "zh-CN" || enResp.Lang != "en-US" {
		t.Fatalf("响应应回带语言: zh=%q en=%q", zhResp.Lang, enResp.Lang)
	}
}

func TestArtifactEnsureRecordSameLangSameVersionAppends(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first, err := svc.EnsureRecord(ctx, langReq("zh-CN", 1, testArtifactID, "hash-zh-v1", "artifacts/hash-zh-v1", "/zh-CN/about"))
	if err != nil {
		t.Fatalf("首次归档失败: %v", err)
	}

	if _, err = svc.EnsureRecord(ctx, langReq("en-US", 1, testArtifactID3, "hash-en", "artifacts/hash-en", "/en-US/about")); err != nil {
		t.Fatalf("归档 en-US 失败: %v", err)
	}

	second, err := svc.EnsureRecord(ctx, langReq("zh-CN", 1, testArtifactID2, "hash-zh-v2", "artifacts/hash-zh-v2", "/zh-CN/about"))
	if err != nil {
		t.Fatalf("同语言同版本重构建失败: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("同语言同版本新内容必须使用新ID: %s", first.ID)
	}
	if second.ArtifactHash != "hash-zh-v2" || second.ArtifactKey != "artifacts/hash-zh-v2" {
		t.Fatalf("新产物指针不正确: hash=%s key=%s", second.ArtifactHash, second.ArtifactKey)
	}
	if got := rowsByLang(t, svc, testPageID); got["zh-CN"] != 2 || got["en-US"] != 1 || len(got) != 2 {
		t.Fatalf("新旧中文两行、英文一行必须并存，实际 %v", got)
	}

	enRow, err := svc.Model().GetByHash(ctx, testPageID, "hash-en")
	if err != nil {
		t.Fatalf("查询 en-US 行失败: %v", err)
	}
	if enRow.ID != testArtifactID3 || enRow.ArtifactHash != "hash-en" {
		t.Fatalf("en-US 行被同版本 zh-CN 重构建污染: id=%s hash=%s", enRow.ID, enRow.ArtifactHash)
	}
}

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
	row, err := svc.Model().GetByHash(ctx, testPageID, "hash-default")
	if err != nil {
		t.Fatalf("按默认语言查询失败: %v", err)
	}
	if row.ID != testArtifactID || row.Lang != defaultLang {
		t.Fatalf("默认语言行错误: id=%s lang=%s", row.ID, row.Lang)
	}

	explicit, err := svc.EnsureRecord(ctx, langReq(defaultLang, 1, testArtifactID2, "hash-default-2", "artifacts/hash-default-2", "/about"))
	if err != nil {
		t.Fatalf("显式默认语言归档失败: %v", err)
	}
	if explicit.ID == testArtifactID || explicit.Lang != resp.Lang {
		t.Fatalf("新内容须独立归档且语言一致: old=%+v new=%+v", resp, explicit)
	}
	if got := rowsByLang(t, svc, testPageID); len(got) != 1 || got[defaultLang] != 2 {
		t.Fatalf("归一化后应有默认语言的新旧两行，实际 %v", got)
	}
}

func TestArtifactGetByHashPreservesLang(t *testing.T) {
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
		row, err := svc.Model().GetByHash(ctx, testPageID, c.want)
		if err != nil {
			t.Fatalf("查询 %s 行失败: %v", c.lang, err)
		}
		if row.ArtifactHash != c.want || row.Lang != c.lang {
			t.Fatalf("%s 行错配: hash=%s lang=%s", c.lang, row.ArtifactHash, row.Lang)
		}
	}
	if _, err := svc.Model().GetByHash(ctx, testPageID, "missing"); err != gorm.ErrRecordNotFound {
		t.Fatalf("未登记语言应返回 ErrRecordNotFound，实际 %v", err)
	}
}
