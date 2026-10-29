package pagecontract

import "context"

// PublishedPageLocator 仅按本工程和精确语言取得已发布最终路径。
// 未发布返回空路径，不用草稿、默认语言或唯一系统页替代；不存在或跨工程返回错误。
type PublishedPageLocator interface {
	PublishedPagePath(ctx context.Context, projectID, pageID, lang string) (string, error)
}
