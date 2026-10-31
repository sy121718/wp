package productcontract

import (
	"context"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/facing"
)

type ProductTranslationsReader interface {
	QueryProductTranslations(context.Context, *productdto.ProductTranslationsPageReq) (*ProductTranslations, error)
	SaveProductTranslations(context.Context, *productdto.SaveProductTranslationsReq) (*TranslationSaveResult, error)
}

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

type TranslationSaveResult struct {
	Saved           bool
	Written         int
	Errors          []facing.Labeled
	StaleMarkFailed bool
}
