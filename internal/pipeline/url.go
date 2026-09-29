// Package pipeline 实现 go_wp 发布管道内核（对应规范 docs/03-pipeline.md 与 0-A1）。
//
// 覆盖控制面发布链路：Draft 草稿冻结 → 确定性编译 → 不可变 Artifact（内容寻址）
// → 符号链接式原子激活 → 状态机（Draft/Building/Ready/Failed/Published/Superseded）
// 与秒级回滚、URL 修改自动 301。
//
// 设计边界：
//   - 本包不依赖数据库：持久化由 Phase 0-A1 的 page/build/artifact/publication
//     模块以 GORM 落地，本内核保持纯 Go + 文件系统；
//   - 删除/取消发布/GC 与构建队列（docs/03-pipeline.md §7/§8）属后续阶段。
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go_wp/pkg/pathkit"
)

// 系统保留路径前缀（docs/03-pipeline.md §5.1）：Page 不可占用。
var reservedPrefixes = []string{
	"/admin",
	"/api",
	"/_fragments",
	"/assets",
	"/objects",
}

// NormalizeURL 规范化页面访问路径（docs/03-pipeline.md §5.1）。
//
// **归一化规则本身不在这里**：唯一实现在 pkg/pathkit.NormalizeRoutePath
// （审计 CQ-012 —— 此前 pipeline / publication / dashboard / nav 各有一份，
// 对尾斜杠、多重斜杠、query 与百分号编码穿越的处理互不相同，同一路径在不同
// 入口得到不同结果，路由占用判断因此与访问面产物分裂）：
//
//   - 只保存 path，不含 scheme/host/query 与锚点；
//   - 必须以 "/" 开头；根路径保留 "/"，其余去除结尾斜杠；
//   - 拒绝 ".." 段、重复分隔符、控制字符、空格、反斜杠与编码后的路径穿越（%2e）；
//   - 超过 pkg/pathkit.MaxRoutePathLen 的路径拒绝；
//   - "/index"、"/index.html"（含带尾斜杠写法）归一为根路径。
//
// 这里只叠加发布管道自己的策略：拒绝系统保留路径
// （/admin、/api、/_fragments、/assets、/objects 及子路径）。
func NormalizeURL(raw string) (string, error) {
	p, err := pathkit.NormalizeRoutePath(raw)
	if err != nil {
		return "", err
	}
	for _, rp := range reservedPrefixes {
		if p == rp || strings.HasPrefix(p, rp+"/") {
			return "", fmt.Errorf("路径 %q 为系统保留路径（%s）", p, rp)
		}
	}
	return p, nil
}

// CleanSiteRel 站点内相对路径归一（访问面专用）：去掉首尾斜杠、Clean 后拒绝越界。
//
// 唯一实现。此前访问面（routers.cleanSiteRel）与守卫中间件各写一份 —— 两份
// 分叉的后果不是「行为略有差异」，而是**守卫被绕过**：归一规则松掉一处
// （少拒绝一种 `..` 写法、或不做 Clean），静态面把请求解析成受限页面的产物，
// 而守卫按另一个路径判定为「没有守卫」，于是受限内容直接被直出。
// routers 侧已改为委托本函数（routers/siteFileServeMiddleware）。
func CleanSiteRel(raw string) (rel string, ok bool) {
	rel = strings.Trim(raw, "/")
	if rel == "" {
		return "", true
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
		return "", false
	}
	return clean, true
}

// ResolveActiveEntry 站点内相对路径 → 访问面**实际会直出**的激活条目名。
//
// 这是本任务的第一安全要点，也是本仓最容易分叉的一条映射：访问面既能用
// `/about` 取到产物，也能用 `/about/index.html` 取到同一份产物。守卫若只认前者，
// 一次 `GET /about/index.html` 就是一行就写完的绕过 —— 守卫判 `/about` 受限、
// 而静态面对显式文件名走「产物内普通文件」分支原样直出受限内容。
//
// 两个分支与 routers.siteFileServeMiddleware 逐字对应（routers.activeEntryFile 已
// 改为委托本函数，两侧不再各持一份规则）：
//
//	① URL 路径即条目名：<clean>/index.html 是常规文件 → 条目 = clean
//	② 显式文件名退路：clean 以 /index.html 结尾、且其前缀是激活符号链接、
//	   且链接目标里有 index.html → 条目 = 该前缀
//
// ② 在 ① 之后：页面 URL 真叫 /foo/index.html 时，① 会命中它自己的产物目录，
// 那时它才是本次请求的答案（`foo` 是另一份页面）。
//
// 产物内其余文件（manifest.json 等）不在本函数的判定范围内 —— 它们不含秘密，
// 且 guard.json / guard.html 由 AccessGuardMiddleware 单独拦截。将来产物若携带
// 非公开文件，必须把这里扩展为「以受限条目为前缀的任何子路径」。
func ResolveActiveEntry(root, rel string) (entry string, ok bool) {
	clean := strings.Trim(strings.TrimSpace(rel), "/")

	direct := clean
	if direct == "" {
		direct = "index"
	}
	if isRegularFile(filepath.Join(root, filepath.FromSlash(direct), "index.html")) {
		return direct, true
	}

	base, found := strings.CutSuffix(clean, "/index.html")
	if !found || base == "" {
		return "", false
	}
	link := filepath.Join(root, filepath.FromSlash(base))
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(link), target))
	if !isRegularFile(filepath.Join(dir, "index.html")) {
		return "", false
	}
	return base, true
}

// isRegularFile 路径是常规文件（存在且不是目录）。
func isRegularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
