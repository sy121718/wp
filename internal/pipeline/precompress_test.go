package pipeline

import (
	"bytes"
	"compress/gzip"
	"io"
	"slices"
	"strings"
	"testing"
)

// gzipHeader 解析 gzip 流的固定 10 字节头（RFC 1952 §2.3.1）。
//
// 只解出确定性相关的那几个域：FLG / MTIME / OS。其余（XFL、deflate 流）不关心 ——
// 本测试要钉的是「产物字节里不许有任何与构建时刻、构建机器有关的东西」。
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

// mustGunzip 解压（测试用；解不开直接失败）。
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

// TestGzipDeterministicHeaderIsFrozen 钉住 gzip header 里两个会破坏确定性的域。
//
// **这条测试的红绿判据是 MTIME 全零**：把 GzipDeterministic 里的
// `zw.ModTime = time.Unix(0, 0)` 注释掉，MTIME 立刻变成
// uint32(零值 time.Time 的 Unix 秒) = 0x8873…（一个 2042 年的假时间戳），
// 本测试变红。
//
// 顺带说明一个与本仓注释常见说法相反的事实：**Go 的 compress/gzip 默认不把当前
// 时间写进 header**（Header 的零值 ModTime 是 time.Time{}，不是 time.Now()），
// 所以「不清零 ⇒ 两次构建字节不同」在 Go 上并不成立。清零真正解决的是**规范性**：
// 零值 time.Time 的 Unix 秒是一个非零的 uint32，会被解压器读成一个荒唐的修改时间。
// 判据因此写成「MTIME 必须恰好是 0」，而不是「两次构建字节相同」——后者即使不清零
// 也会通过，是一条抓不到东西的断言。
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
	// FLG 的 FNAME(0x08) / FCOMMENT(0x10)：两者都会把文件名或任意字符串写进产物字节。
	if h.flg&0x08 != 0 || h.flg&0x10 != 0 {
		t.Errorf("FLG = 0x%02x：不允许往产物字节里写 FNAME / FCOMMENT", h.flg)
	}
	if got := mustGunzip(t, gz); !bytes.Equal(got, plain) {
		t.Error("解压结果与明文不一致")
	}
}

// TestGzipDeterministicByteIdentical 同一输入多次派生必须逐字节相同。
//
// 这是 AGENTS.md 不变量 5 在预压缩产物上的直接落点：派生的全部输入就是明文，
// 因此派生的全部输出也必须只有一个可能值。
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

// TestPrecompressEntriesWhitelistAndThreshold 白名单与阈值：文本类且达到阈值才派生。
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
		// 已是压缩格式：再压一次是纯 CPU 浪费，产物往往比原文还大。
		{"logo.png", big, false},
		{"photo.jpg", big, false},
		{"hero.webp", big, false},
		{"icon.avif", big, false},
		{"font.woff2", big, false},
		{"archive.zip", big, false},
		{"bundle.js.br", big, false},
		// 文本类但不到阈值。
		{"tiny.html", big[:PrecompressMinSize-1], false},
		// 恰好等于阈值：判据是 `< MinSize 才跳过`，边界必须包含在内。
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

// TestArtifactCarriesPrecompressedDerivative 产物组装时带上 <name>.gz，
// 且这份派生**不进 Manifest.Files、不改产物 hash**。
//
// 三条断言各钉一个决定：
//   - 派生字节必须在 Entries 里 —— 落盘由 Entries 驱动（LocalStore.PutArtifact），
//     不进 Entries 就永远到不了磁盘，访问面也就永远探不到 .gz；
//   - 派生字节不得进 Manifest.Files —— 进了就等于让产物 hash 依赖压缩库实现
//     （Go 升版换了 flate 输出 ⇒ 全站 hash 变 ⇒ 全量重建）；
//   - 两次构建的产物 hash 与 .gz 字节都必须相同（不变量 5）。
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

// TestArtifactPrecompressedCoversGuardAndCompanionFiles 伴随文件（守卫页）同样被派生。
//
// 判据不是「guard.html 有没有 .gz」本身，而是「派生规则对 Entries 全体一视同仁」：
// 若派生只写死在 index.html 上，将来新增的文本类伴随文件会自动漏掉，而表现是
// 「只有一部分页面拿到了预压缩」——这种缺陷没有任何报错路径。
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
		// guard.html 的明文哈希必须在 Manifest.Files 里（PIPE-6 的既有语义），
		// 但它的派生字节不在 —— 两条结论互不干扰。
		if _, ok := a.Manifest.Files[name]; !ok {
			t.Errorf("%s 的明文哈希不在 Manifest.Files 里", name)
		}
		if _, ok := a.Manifest.Files[name+PrecompressedSuffix]; ok {
			t.Errorf("%s 的派生字节被登记进 Manifest.Files", name)
		}
	}
}

// TestAssetContentTypeHtmlHasCharset 访问面直出 HTML 的类型必须带 charset。
//
// 单源函数（明文路径 routers.serveArtifactFile 与预压缩层共用）——
// mime.TypeByExtension 对 .html 的返回取决于系统 mime.types，不保证带 charset。
func TestAssetContentTypeHtmlHasCharset(t *testing.T) {
	cases := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"about.HTML": "text/html; charset=utf-8",
		// .js 的具体串随系统 mime.types（本机给出 text/javascript; charset=utf-8，
		// 旧表给 application/javascript）—— 这里只断「是 JS 类型」，
		// 「能不能压缩」由 builtin 侧拿 isGzipType 对账（那才是真正要一致的那一条）。
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

// mapKeys 取 map 的键（仅测试用，失败信息里列出实际键）。
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
