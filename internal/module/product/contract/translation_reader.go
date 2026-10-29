package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/facing"
)

// ProductTranslationsReader 查询译文事实与执行保存，两个结果互相独立。
type ProductTranslationsReader interface {
	QueryProductTranslations(context.Context, *productdto.ProductTranslationsPageReq) (*ProductTranslations, error)
	SaveProductTranslations(context.Context, *productdto.SaveProductTranslationsReq) (*TranslationSaveResult, error)
}

// ProductTranslations 商品域的原文与译文集合，不包含下拉选中态和界面标题。
type ProductTranslations struct {
	ProjectID  string
	ProductID  string
	Lang       string
	Langs      []string
	SourceLang string
	Groups     []TranslationEntity
	Done       int
	Total      int
}

// TranslationEntity 同一实体的可翻译字段。
type TranslationEntity struct {
	EntityType string
	EntityID   string
	EntityName string
	Rows       []TranslationValue
}

type TranslationValue struct {
	Context    string
	Field      string
	Source     string
	SourceHash string
	Target     string
	Engine     string
}

// TranslationSaveResult 保存结论；不携带查询数据或页面回显。
type TranslationSaveResult struct {
	Saved           bool
	Written         int
	Errors          []facing.Labeled
	StaleMarkFailed bool
}
