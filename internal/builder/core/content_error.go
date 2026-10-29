package core

import "errors"

// ErrContentResolverRequired 表示组件声明了实体字段绑定，但构建上下文没有内容解析器。
// 调用方按错误链识别该事实；合法集合项绑定或其它编译失败不属于此错误。
var ErrContentResolverRequired = errors.New("编译上下文缺少内容解析器")
