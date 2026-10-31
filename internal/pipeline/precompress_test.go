package pipeline

import (
	"bytes"
	"compress/gzip"
	"io"
	"slices"
	"strings"
	"testing"
)

type gzipHeader struct {
	flg   byte
	mtime [4]byte
	os    byte
	ok    bool
}

func parseGzipHeader(stream []byte) gzipHeader {
	var h gzipHeader
	if len(stream) < 10 || stream[0] != 0x1f || stream[1] != 0x8b || stream[2] != 0x08 {
		return h
	}
	h.flg = stream[3]
	copy(h.mtime[:], stream[4:8])
	h.os = stream[9]
	h.ok = true
	return h
}

func mustGunzip(t *testing.T, stream []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("产物不是可读的 gzip 流: %v", err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip 流解压失败: %v", err)
	}
	return out
}

func TestGzipDeterministicHeaderIsFrozen(t *testing.T) {
	plain := bytes.Repeat([]byte("<p>确定性构建</p>\n"), 200)

	gz, err := GzipDeterministic(plain)
	if err != nil {
		t.Fatalf("GzipDeterministic 失败: %v", err)
	}
	h := parseGzipHeader(gz)
	if !h.ok {
		t.Fatal("产物不是合法 gzip 流")
	}
	t.Logf("明文 %d B → .gz %d B（级别 BestCompression）", len(plain), len(gz))
	t.Logf("header: FLG=0x%02x MTIME=% x OS=%d", h.flg, h.mtime, h.os)
	t.Logf(".gz sha256 = %s", SHA256(gz))
	if h.mtime != [4]byte{} {
		t.Errorf("MTIME = % x（期望全零）：RFC 1952 规定 0 = 无时间戳，非零表示"+
			"「这份产物诞生于某个具体时刻」，会把构建时刻烙进不可变产物", h.mtime)
	}
	if h.os != gzipOSUnknown {
		t.Errorf("OS 字节 = %d（期望 %d = unknown）：不写死它，同一份文档在不同构建"+
			"机器上会产出不同字节", h.os, gzipOSUnknown)
	}

	if h.flg&0x08 != 0 || h.flg&0x10 != 0 {
		t.Errorf("FLG = 0x%02x：不允许往产物字节里写 FNAME / FCOMMENT", h.flg)
	}
	if got := mustGunzip(t, gz); !bytes.Equal(got, plain) {
		t.Error("解压结果与明文不一致")
	}
}

func TestGzipDeterministicByteIdentical(t *testing.T) {
	plain := []byte("<!doctype html>\n" + strings.Repeat("<p>正文 bytes</p>\n", 300))

	first, err := GzipDeterministic(plain)
	if err != nil {
		t.Fatalf("GzipDeterministic 失败: %v", err)
	}
	t.Logf("第 1 次：%d B  sha256=%s", len(first), SHA256(first))
	for i := range 4 {
		again, err := GzipDeterministic(plain)
		if err != nil {
			t.Fatalf("第 %d 次派生失败: %v", i+1, err)
		}
		t.Logf("第 %d 次：%d B  sha256=%s", i+2, len(again), SHA256(again))
		if !bytes.Equal(first, again) {
			t.Fatalf("第 %d 次派生与首次逐字节不同：sha256 %s vs %s",
				i+1, SHA256(first), SHA256(again))
		}
	}
	t.Log("5 次派生逐字节相同")
}

func TestPrecompressEntriesWhitelistAndThreshold(t *testing.T) {
	big := bytes.Repeat([]byte("x"), PrecompressMinSize)

	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"index.html", big, true},
		{"guard.html", big, true},
		{"app.js", big, true},
		{"theme.mjs", big, true},
		{"style.css", big, true},
		{"sitemap.xml", big, true},
		{"icon.svg", big, true},
		{"data.json", big, true},
		{"robots.txt", big, true},
		{"site.webmanifest", big, true},

		{"logo.png", big, false},
		{"photo.jpg", big, false},
		{"hero.webp", big, false},
		{"icon.avif", big, false},
		{"font.woff2", big, false},
		{"archive.zip", big, false},
		{"bundle.js.br", big, false},

		{"tiny.html", big[:PrecompressMinSize-1], false},

		{"exact.html", big, true},
	}

	for _, tc := range cases {
		entries := map[string][]byte{tc.name: tc.data}
		if err := PrecompressEntries(entries); err != nil {
			t.Fatalf("PrecompressEntries(%s) 失败: %v", tc.name, err)
		}
		gz, got := entries[tc.name+PrecompressedSuffix]
		if got != tc.want {
			t.Errorf("%s：派生 = %v，期望 %v", tc.name, got, tc.want)
			continue
		}
		if got && bytes.Equal(gz, tc.data) {
			t.Errorf("%s：派生字节与明文相同，说明没有真的压缩", tc.name)
		}
	}
}

func TestArtifactCarriesPrecompressedDerivative(t *testing.T) {
	html := []byte("<!doctype html>\n" + strings.Repeat("<p>页面正文</p>\n", 300))

	build := func() *Artifact {
		t.Helper()
		m := &Manifest{
			CanonicalPath: "/about",
			SourceID:      "page-1",
			SourceType:    SourceTypePage,
			Files:         map[string]string{},
		}
		a, err := NewArtifact(html, m)
		if err != nil {
			t.Fatalf("组装产物失败: %v", err)
		}
		return a
	}

	a := build()
	gzName := "index.html" + PrecompressedSuffix
	gz, ok := a.Entries[gzName]
	if !ok {
		keys := mapKeys(a.Entries)
		slices.Sort(keys)
		t.Fatalf("产物 Entries 缺少 %s，实际键：%v", gzName, keys)
	}
	if got := mustGunzip(t, gz); !bytes.Equal(got, html) {
		t.Error("预压缩产物解压后与入口 HTML 不一致")
	}
	if _, registered := a.Manifest.Files[gzName]; registered {
		t.Errorf("派生字节被登记进 Manifest.Files：产物 hash 会随压缩库实现变化（不变量 5 的可用性一面）")
	}

	b := build()
	if a.Hash != b.Hash {
		t.Errorf("同一份文档两次构建产物 hash 不同：%s vs %s", a.Hash, b.Hash)
	}
	if !bytes.Equal(gz, b.Entries[gzName]) {
		t.Errorf("同一份文档两次构建的 .gz 字节不同：sha256 %s vs %s",
			SHA256(gz), SHA256(b.Entries[gzName]))
	}
}

func TestArtifactPrecompressedCoversGuardAndCompanionFiles(t *testing.T) {
	html := []byte("<!doctype html>" + strings.Repeat("<!--p-->", 300))
	guard := []byte("<!doctype html>" + strings.Repeat("<p>请输入密码</p>", 200))

	m := &Manifest{CanonicalPath: "/secret", SourceID: "page-2", Files: map[string]string{}}
	a, err := NewArtifactWithEntries(html, m, map[string][]byte{"guard.html": guard})
	if err != nil {
		t.Fatalf("组装产物失败: %v", err)
	}
	for _, name := range []string{"index.html", "guard.html"} {
		gz, ok := a.Entries[name+PrecompressedSuffix]
		if !ok {
			t.Errorf("伴随文件 %s 没有派生 %s", name, PrecompressedSuffix)
			continue
		}
		plain := a.Entries[name]
		if got := mustGunzip(t, gz); !bytes.Equal(got, plain) {
			t.Errorf("%s 的派生字节与明文不一致", name)
		}

		if _, ok := a.Manifest.Files[name]; !ok {
			t.Errorf("%s 的明文哈希不在 Manifest.Files 里", name)
		}
		if _, ok := a.Manifest.Files[name+PrecompressedSuffix]; ok {
			t.Errorf("%s 的派生字节被登记进 Manifest.Files", name)
		}
	}
}

func TestAssetContentTypeHtmlHasCharset(t *testing.T) {
	cases := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"about.HTML": "text/html; charset=utf-8",

		"app.js":        "javascript",
		"style.css":     "text/css",
		"icon.svg":      "image/svg",
		"sitemap.xml":   "xml",
		"noext-unknown": "application/octet-stream",
	}
	for path, want := range cases {
		got := AssetContentType(path)
		if strings.HasSuffix(want, "charset=utf-8") {
			if got != want {
				t.Errorf("AssetContentType(%q) = %q，期望 %q", path, got, want)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("AssetContentType(%q) = %q，期望包含 %q", path, got, want)
		}
	}
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
