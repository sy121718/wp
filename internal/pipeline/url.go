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
	"strings"
)

// 系统保留路径前缀（docs/03-pipeline.md §5.1）：Page 不可占用。
var reservedPrefixes = []string{
	"/admin",
	"/api",
	"/_fragments",
	"/assets",
	"/objects",
}

// maxURLPathLen 路径长度上限（与 publication normalizePath 的 500 对齐，
// docs/03-pipeline.md §5.1）：超长路径会导致 FS 激活 ENAMETOOLONG 与 DB 行膨胀。
const maxURLPathLen = 500

// NormalizeURL 规范化页面访问路径（docs/03-pipeline.md §5.1）。
//
// 规则：
//   - 只保存 path，不含 scheme/host/query；
//   - 必须以 "/" 开头；根路径保留 "/"，其余去除结尾斜杠；
//   - 拒绝 ".." 段、重复分隔符、控制字符、空格、反斜杠与编码后的路径穿越（%2e）；
//   - 拒绝超过 maxURLPathLen（500）的路径；
//   - 拒绝系统保留路径（/admin、/api、/_fragments、/assets、/objects 及子路径）。
func NormalizeURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("路径必须以 / 开头: %q", raw)
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("路径含非法字符: %q", raw)
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("路径含控制字符: %q", raw)
		}
		if r == ' ' {
			return "", fmt.Errorf("路径含空格: %q", raw)
		}
	}

	// 根路径直接放行。
	if raw == "/" {
		return "/", nil
	}

	// 去除结尾斜杠（根路径已提前放行；重复分隔符在分段时拒绝）。
	p := strings.TrimSuffix(raw, "/")
	if len(p) > maxURLPathLen {
		return "", fmt.Errorf("路径过长（最大 %d 字符）: %q", maxURLPathLen, raw)
	}

	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, seg := range segs {
		if seg == "" {
			return "", fmt.Errorf("路径含重复分隔符: %q", raw)
		}
		if seg == "." || seg == ".." || strings.Contains(strings.ToLower(seg), "%2e") {
			return "", fmt.Errorf("路径含非法段 %q（拒绝路径穿越）: %q", seg, raw)
		}
	}

	// 保留路径校验：精确匹配或处于其子路径下。
	for _, rp := range reservedPrefixes {
		if p == rp || strings.HasPrefix(p, rp+"/") {
			return "", fmt.Errorf("路径 %q 为系统保留路径（%s）", p, rp)
		}
	}
	return p, nil
}
