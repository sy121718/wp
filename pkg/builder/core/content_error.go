package core

import "errors"

var ErrContentResolverRequired = errors.New("编译上下文缺少内容解析器")
