package contentenums

const (
	OkCreated = "admin.article.ok.created"
	OkSaved   = "admin.article.ok.saved"
	OkDeleted = "admin.article.ok.deleted"

	ErrMissingID    = "admin.article.err.missingID"
	ErrMissingTitle = "admin.article.err.missingTitle"
	ErrMissingSlug  = "admin.article.err.missingSlug"

	PreviewNeedSave    = "admin.article.preview.needSave"
	PreviewUnavailable = "admin.article.preview.unavailable"
	PreviewFailed      = "admin.article.preview.failed"
	PreviewEmpty       = "admin.article.preview.empty"

	ImportErrEmptyBody        = "admin.article.import.err.emptyBody"
	ImportErrNoProject        = "admin.article.import.err.noProject"
	ImportErrNoPath           = "admin.article.import.err.noPath"
	ImportErrNoPageCapability = "admin.article.import.err.noPageCapability"
	ImportErrDepsMissing      = "admin.article.import.err.depsMissing"

	ImportResultCount = "admin.article.import.resultCount"

	ImportNodeHeading   = "admin.article.import.node.heading"
	ImportNodeText      = "admin.article.import.node.text"
	ImportNodeList      = "admin.article.import.node.list"
	ImportNodeQuote     = "admin.article.import.node.quote"
	ImportNodeImage     = "admin.article.import.node.image"
	ImportNodeDivider   = "admin.article.import.node.divider"
	ImportNodeTable     = "admin.article.import.node.table"
	ImportNodeContainer = "admin.article.import.node.container"

	ImportActionUnwrap      = "admin.article.import.action.unwrap"
	ImportActionDrop        = "admin.article.import.action.drop"
	ImportActionTrim        = "admin.article.import.action.trim"
	ImportActionPlaceholder = "admin.article.import.action.placeholder"

	TranslationsHeading   = "admin.article.translations.heading"
	TranslationsSaved     = "admin.article.translations.saved"
	TranslationsSavedNone = "admin.article.translations.savedNone"

	TranslationsFieldTitle   = "admin.article.translations.field.title"
	TranslationsFieldBody    = "admin.article.translations.field.body"
	TranslationsFieldExcerpt = "admin.article.translations.field.excerpt"

	TranslationsErrPageChanged        = "admin.article.translations.err.pageChanged"
	TranslationsErrRowCountMismatch   = "admin.article.translations.err.rowCountMismatch"
	TranslationsErrStorageUnavailable = "admin.article.translations.err.storageUnavailable"
	TranslationsErrSourceChanged      = "admin.article.translations.err.sourceChanged"
	TranslationsErrSaveFailed         = "admin.article.translations.err.saveFailed"
	TranslationsErrContextInvalid     = "admin.article.translations.err.contextInvalid"
	TranslationsErrNotTranslatable    = "admin.article.translations.err.notTranslatable"
	TranslationsErrRichMismatch       = "admin.article.translations.err.richMismatch"
	TranslationsErrDepsMissing        = "admin.article.translations.err.depsMissing"
	TranslationsErrCountFailed        = "admin.article.translations.err.countFailed"
	TranslationsErrListFailed         = "admin.article.translations.err.listFailed"

	PublishOkPublished        = "admin.article.publish.ok.published"
	PublishOkRebuilt          = "admin.article.publish.ok.rebuilt"
	PublishOkURLUpdated       = "admin.article.publish.ok.urlUpdated"
	PublishErrNoProject       = "admin.article.publish.err.noProject"
	PublishErrNoURLPath       = "admin.article.publish.err.noURLPath"
	PublishErrNoTemplatePick  = "admin.article.publish.err.noTemplatePick"
	PublishHintNoTemplate     = "admin.article.publish.hint.noTemplate"
	PublishErrUnavailable     = "admin.article.publish.err.unavailable"
	PublishErrDepsMissing     = "admin.article.publish.err.depsMissing"
	PublishErrNewPathRequired = "admin.article.publish.err.newPathRequired"
	PublishHintSaveFirst      = "admin.article.publish.hint.saveFirst"
)
