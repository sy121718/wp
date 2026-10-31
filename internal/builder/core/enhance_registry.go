package core

import (
	"strings"
	"sync"
)

type EnhanceBlock struct {
	Fns []string

	Feats []string

	Source string
}

var (
	enhanceBlocksMu sync.RWMutex
	ownEnhanceList  []EnhanceBlock
)

func RegisterEnhanceBlock(b EnhanceBlock) {
	if len(b.Fns) == 0 {
		panic("core.RegisterEnhanceBlock: 没有声明任何函数名")
	}
	for _, fn := range b.Fns {
		if strings.TrimSpace(fn) == "" {
			panic("core.RegisterEnhanceBlock: 函数名为空")
		}
	}
	if len(b.Feats) == 0 {
		panic("core.RegisterEnhanceBlock: " + b.Fns[0] + " 没有登记触发特征")
	}
	if strings.TrimSpace(b.Source) == "" {
		panic("core.RegisterEnhanceBlock: " + b.Fns[0] + " 源码为空")
	}
	enhanceBlocksMu.Lock()
	defer enhanceBlocksMu.Unlock()
	for _, ex := range ownEnhanceList {
		for _, prev := range ex.Fns {
			for _, fn := range b.Fns {
				if prev == fn {
					panic("core.RegisterEnhanceBlock: 重复注册 " + fn)
				}
			}
		}
	}
	ownEnhanceList = append(ownEnhanceList, b)
}

func OwnedEnhanceBlocks() []EnhanceBlock {
	enhanceBlocksMu.RLock()
	defer enhanceBlocksMu.RUnlock()
	out := make([]EnhanceBlock, len(ownEnhanceList))
	copy(out, ownEnhanceList)
	return out
}
