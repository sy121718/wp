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
