package core

// enhance_registry.go — 组件自带客户端增强块的注册表。
//
// 背景：客户端增强此前集中在一个 538 行的 enhance.js 里（8 个互不相关的增强：计数器 / 轮播 /
// 图集 / 倒计时 / 灯箱 / 卡片拖拽 / 卡片轮播 / 全屏分页）。组件自己的行为却要跑到那个大文件里改。
//
// 「一个组件一个目录」要求行为就近：
//
//     internal/builder/components/counter/
//       counter.go      props + Validate + compileCSS
//       counter.css     样式
//       counter.jet     模板
//       counter.js      行为（本文件让它可就地 //go:embed 并注册）
//
// 依赖方向与模板注册一致：本文件只提供注册与读取，不认识 builder 包；
// 由装配层（internal/builder 的 enhance_select.go）主动来取。

import (
	"strings"
	"sync"
)

// EnhanceBlock 一个组件自带的客户端增强块。
type EnhanceBlock struct {
	// Fn 初始化函数名（产物拼装时据此决定 onReady 里保留哪些调用项）。
	Fn string
	// Feats 触发注入的产物特征（任一命中即注入）。
	Feats []string
	// Source 块源码（含它自己的注释头），原样内联进产物。
	Source string
}

var (
	enhanceBlocksMu sync.RWMutex
	ownEnhanceList  []EnhanceBlock
)

// RegisterEnhanceBlock 注册组件自带的增强块（在组件包 init 中调用）。
//
// 顺序即产物内联顺序。注册期做基本校验并 panic —— 这是 init 期错误：
// 漏登记特征会让用到它的页面静默失去交互（「轮播不能拖、灯箱点不开」），
// 比启动失败难查得多。
func RegisterEnhanceBlock(b EnhanceBlock) {
	if strings.TrimSpace(b.Fn) == "" {
		panic("core.RegisterEnhanceBlock: 函数名为空")
	}
	if len(b.Feats) == 0 {
		panic("core.RegisterEnhanceBlock: " + b.Fn + " 没有登记触发特征")
	}
	if strings.TrimSpace(b.Source) == "" {
		panic("core.RegisterEnhanceBlock: " + b.Fn + " 源码为空")
	}
	enhanceBlocksMu.Lock()
	defer enhanceBlocksMu.Unlock()
	for _, ex := range ownEnhanceList {
		if ex.Fn == b.Fn {
			panic("core.RegisterEnhanceBlock: 重复注册 " + b.Fn)
		}
	}
	ownEnhanceList = append(ownEnhanceList, b)
}

// OwnedEnhanceBlocks 返回组件自带的增强块（注册顺序，即内联顺序）。
func OwnedEnhanceBlocks() []EnhanceBlock {
	enhanceBlocksMu.RLock()
	defer enhanceBlocksMu.RUnlock()
	out := make([]EnhanceBlock, len(ownEnhanceList))
	copy(out, ownEnhanceList)
	return out
}
