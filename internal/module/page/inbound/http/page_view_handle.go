package pagehttp

import (
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/facing"
	"go_wp/pkg/utils"
)

type PageLangResp struct {
	Lang string

	IsDefault bool

	Excluded bool

	Published bool

	StatusKey      string
	StatusFallback string

	CanRepublish bool

	NoteKey      string
	NoteFallback string
}

type PageRowResp struct {
	ID           string
	ProjectID    string
	Kind         string
	DraftPath    string
	ActivePath   string
	Publications []pagedto.PagePublicationResp
	Active       bool
	Staged       bool
	Stale        bool
	Version      int64
	UpdatedAt    utils.JSONTime

	LangScope     string
	LangStatusKey string
	LangStatusFb  string
}

type TranslationLangOptionResp struct {
	Code   string
	Label  string
	Active bool
}

type PageListLangOptionResp struct {
	Value   string
	Label   string
	Default bool
	Active  bool
}

type PageListStateOptionResp struct {
	Value    string
	Key      string
	Fallback string
	Active   bool
}

type TranslationRowResp struct {
	Context    string
	Component  string
	Field      string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	Rich       bool
	Limit      int

	ReusePages int

	ReuseTotal int

	ReusePaths []string

	Origin string
}

type TranslationGroupResp struct {
	Component string
	Rows      []TranslationRowResp
}

type TranslationMissPageResp struct {
	CSRFToken       string
	Projects        any
	SelectedProject string
	Rows            any

	Err any
}

type PageHistoryListResp struct {
	Revisions []HistoryRowResp

	Error any
}

type TranslationErrorsResp struct {
	Errors []TranslationRowErrorResp
}

type HistoryRowResp struct {
	Version   int64
	Path      string
	CreatedAt utils.JSONTime
}

type TranslationRowErrorResp struct {
	Label string
	Msg   facing.Msg
}
