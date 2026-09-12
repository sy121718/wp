package contentcontract

import "go_wp/internal/builder/source"

// data_source.go — CMS 内容对外暴露的**构建期数据源**（issue #35）。
//
// 与 ContentService 的区别同商品侧：这里是构建期只读的数据源能力，
// 没有建内容 / 改内容 / 发版这些写入口。
type ContentDataSource interface {
	source.CollectionResolver
	source.CollectionSchemaProvider
}
