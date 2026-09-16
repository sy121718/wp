package projectservice

// theme_bundle_media.go — 主题包的媒体依赖策略（审计 VIS-014 第 4 步）。
//
// 死线：**不允许静默产生死链**。媒体引用只有三种结局，且每一种都要在报告里说清楚：
//  1. 内嵌（embed）：导出侧读得到字节 → 写进包里 media/<key>，导入侧落盘回 /storage/<原路径>，
//     引用直接可用；
//  2. 目标已有：包内没有（导出侧读不到，或导出时选了 declare）但目标环境该路径已有文件，
//     引用可用 —— 按 sha256 判定是「同内容」还是「同名不同内容」（后者如实报冲突，不覆盖）；
//  3. 缺失：既未内嵌、目标也没有 → 进缺失清单（URL + 引用来源 + willDeadLink），
//     调用方拿到的是**可执行的补件清单**，而不是一个静默 404 的站点。
//
// 为什么不登记媒体库附件（sys_attachment）：那是 media 模块的表，project 模块伸进去
// 就等于绕过对方的仓储方法（表隔离约定）。因此内嵌媒体只落文件、不建附件记录 ——
// 静态面 /storage 直接按路径提供文件，引用可用；这一点写进导入报告的 warnings。

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	projectdto "go_wp/internal/module/project/dto"
)

// themeBundleMediaRoot 媒体落盘根目录：与 /storage 静态面同一目录（见 routers 的
// gin.Dir("public/storage")）。环境变量可覆盖 —— 测试用隔离目录，别把字节写进仓库目录。
func themeBundleMediaRoot() string {
	if root := strings.TrimSpace(os.Getenv("GO_WP_THEME_MEDIA_ROOT")); root != "" {
		return root
	}
	return filepath.Join("public", "storage")
}

// themeBundleMediaRef 一条媒体引用（URL + 引用来源）。
type themeBundleMediaRef struct {
	URL          string
	Path         string
	ReferencedBy []string
}

// collectBundleMediaRefs 汇总文档集合里的媒体引用（按 URL 去重合并引用来源）。
func collectBundleMediaRefs(docs []themeBundleDocRef) []themeBundleMediaRef {
	acc := map[string]*themeBundleMediaRef{}
	for _, d := range docs {
		for _, url := range collectThemeMediaURLs(d.Raw) {
			rel, ok := storageRelativePath(url)
			if !ok {
				continue
			}
			item, hit := acc[url]
			if !hit {
				item = &themeBundleMediaRef{URL: url, Path: rel}
				acc[url] = item
			}
			item.ReferencedBy = appendUnique(item.ReferencedBy, d.Label)
		}
	}
	if len(acc) == 0 {
		return nil
	}
	out := make([]themeBundleMediaRef, 0, len(acc))
	for _, item := range acc {
		sort.Strings(item.ReferencedBy)
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out
}

// themeBundleDocRef 参与媒体扫描的文档（Raw 为原始字节，Label 形如 block:b1 / page:p1）。
type themeBundleDocRef struct {
	Raw   []byte
	Label string
}

// themeBundleMediaFileName 包内媒体文件名：key + 原始 basename（安全化后）。
//
// 保留 basename 是为了让人打开包时能看出这是什么图；前缀 key 保证不同目录下的同名文件
// 不互相覆盖（/storage/a/logo.png 与 /storage/b/logo.png）。
func themeBundleMediaFileName(key, rel string) string {
	base := path.Base(rel)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	safe := b.String()
	if safe == "" || strings.HasPrefix(safe, ".") {
		safe = "file"
	}
	return key + "-" + safe
}

// appendUnique 追加不重复的字符串。
func appendUnique(list []string, v string) []string {
	for _, item := range list {
		if item == v {
			return list
		}
	}
	return append(list, v)
}

// mediaFileSHA256 读取存储根下的文件并计算 sha256；读不到返回 ok=false。
func mediaFileSHA256(root, rel string) (sum string, size int64, data []byte, ok bool) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return "", 0, nil, false
	}
	data, err = os.ReadFile(full)
	if err != nil {
		return "", 0, nil, false
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), int64(len(data)), data, true
}

// writeMediaFile 把内嵌媒体落到目标存储路径，返回处置动作。
//
// 三条分支都明确：目标不存在 → 写入；目标存在且内容一致 → 跳过（幂等，重复导入不产生写放大）；
// 目标存在但内容不同 → **不覆盖**（目标站点的既有资产不该被一个导入包改写），并在报告中作为
// 冲突单列 —— 引用仍可用，但指向的是目标那份字节，使用者需要知道这个差异。
func writeMediaFile(root, rel string, data []byte) (action string, err error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err = os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if existing, rerr := os.ReadFile(full); rerr == nil {
		oldSum := sha256.Sum256(existing)
		newSum := sha256.Sum256(data)
		if oldSum == newSum {
			return projectdto.ThemeBundleMediaActionSkipped, nil
		}
		return projectdto.ThemeBundleMediaActionConflict, nil
	}
	if werr := os.WriteFile(full, data, 0o644); werr != nil {
		return "", werr
	}
	return projectdto.ThemeBundleMediaActionWritten, nil
}
