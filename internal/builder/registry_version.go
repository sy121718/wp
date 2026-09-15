package builder

// registry_version.go — 组件注册表版本（真实值，替代原先写入的常量）。

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	"go_wp/internal/builder/core"
)

var (
	buildFingerprintOnce  sync.Once
	buildFingerprintValue string
)

// RegistryVersion 返回当前组件注册表版本（16 位十六进制）。
//
// 为什么需要它：组件是编译进二进制的（Go 实现 + embed 的 .jet 模板），部署新组件后
// 没有任何运行时事件提示「已有产物需要重建」；而 page_artifacts.registry_version
// 此前写入的是常量 "internal-builder"，无从比对（也就无法自动发现该重建的页面）。
//
// 组成两部分：
//
//  1. 构建指纹：运行二进制的 vcs.revision + vcs.modified。覆盖 Go 代码与 embed 模板的
//     任何改动 —— 包括「Props 没变、只有 BuildView/CompileCSS 改了」这种最难察觉的情形。
//  2. 组件清单指纹：全部类型 + Props 的 json/ct 标签结构 + 可翻译白名单，排序后哈希。
//     覆盖运行时注册的组件（插件注册的组件不在二进制里，revision 抓不到）。
//
// 实现上有意**不做** sync.Once 缓存：组件是通过各组件包的 init() 注册的，包的加载
// 顺序决定「计算时注册表是否已完整」。缓存会把早期调用算出的不完整指纹永久固化，
// 而那种错值是静默的（表现为「每次启动都标记全站待重建」或「永远不标记」）。
// 实时计算的成本是 33 个组件的 SHA256（微秒级），调用点也只有启动比对与产物归档。
// 若将来要缓存完整 RegistryVersion，必须在「全部 init 注册完成之后」的明确时点
// （例如 routes 装配结束）再启用，并加断言防止提前调用把不完整指纹固化。
func RegistryVersion() string {
	types := core.Types()
	if len(types) == 0 {
		// 组件尚未注册（在包 init 阶段被调用）：返回空串表示「版本不可确定」，
		// 调用方据此跳过比对 —— 绝不拿不完整指纹去标记页面。
		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte(buildFingerprint()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(componentManifestFingerprint(types)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// buildFingerprint 运行二进制的构建指纹（vcs.revision|vcs.modified）。
//
// 这一项可以安全缓存：它只依赖二进制自身的 build info，与组件注册时机无关。
func buildFingerprint() string {
	buildFingerprintOnce.Do(func() {
		buildFingerprintValue = computeBuildFingerprint()
	})
	return buildFingerprintValue
}

func computeBuildFingerprint() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, mod := "", ""
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			mod = s.Value
		}
	}
	if rev == "" {
		// 无 VCS 信息（go run 单文件模式、非 git 目录构建、-buildvcs=false 等）。
		//
		// 绝不能用 bi.Main.Path 兜底：它随构建方式变化 —— go run cmd/main.go 编译的是
		// 单文件包（Main.Path = command-line-arguments），go run ./pkg 或 go build ./cmd
		// 是模块路径。用它会让同一个 commit 因构建命令不同算出不同版本，表现为
		// 「每次切换构建方式就误判全站待重建」。
		//
		// 退化为固定占位符：这一项失去分辨力（检测不到 BuildView 之类的 Go 代码改动），
		// 但保证稳定。需要完整分辨力时，确保构建产物带 VCS 信息即可
		//（在 git 工作区内 go build ./cmd 是最简单的办法）。
		rev = "novcs"
	}
	return rev + "|" + mod
}

// componentManifestFingerprint 组件清单指纹（类型 + Props 结构 + 可翻译白名单）。
func componentManifestFingerprint(types []string) string {
	h := sha256.New()
	for _, t := range types { // 已按字典序排序，确定性
		_, _ = h.Write([]byte(t))
		_, _ = h.Write([]byte{'|'})
		comp, err := core.Lookup(t)
		if err != nil {
			continue
		}
		if sp, ok := comp.(core.SpecProvider); ok {
			if spec := sp.PropsSpec(); spec != nil {
				_, _ = h.Write([]byte(propsSchemaFingerprint(reflect.TypeOf(spec), map[reflect.Type]bool{})))
			}
		}
		_, _ = h.Write([]byte{'|'})
		if tp, ok := comp.(core.TranslatableProvider); ok {
			fields := append([]string(nil), tp.Translatable()...)
			sort.Strings(fields)
			_, _ = h.Write([]byte(strings.Join(fields, ",")))
		}
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// propsSchemaFingerprint 结构体 schema 指纹：递归收集导出字段的 json / ct 标签。
//
// 用标签而非 json.Marshal 的结果：后者只反映 json tag，字段的控件声明（ct tag：
// 控件类型、取值范围、分区）变化不会体现 —— 而那同样改变编辑体验与构建期校验。
func propsSchemaFingerprint(t reflect.Type, seen map[reflect.Type]bool) string {
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return ""
	}
	seen[t] = true
	defer delete(seen, t) // 同一类型出现在不同分支时各自展开，同时阻断循环引用
	var b strings.Builder
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		b.WriteString(f.Name)
		b.WriteByte(':')
		b.WriteString(f.Tag.Get("json"))
		b.WriteByte(':')
		b.WriteString(f.Tag.Get("ct"))
		b.WriteByte(';')
		if sub := propsSchemaFingerprint(f.Type, seen); sub != "" {
			b.WriteString(sub)
			b.WriteByte(';')
		}
	}
	return b.String()
}
