package navigationservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
)

type TranslationWriter struct {
	rows RowSourceTx

	sources SourceIndexerTx

	projects ProjectLister

	content ContentStore

	pages navigationcontract.PageStalePort

	instances navigationcontract.PresentationStalePort

	queue navigationcontract.MenuStaleEnqueuePort

	tx TxRunner

	now func() time.Time
}

type TranslationWriterDeps struct {
	Rows RowSourceTx

	Sources SourceIndexerTx

	Projects ProjectLister

	Content ContentStore

	Pages     navigationcontract.PageStalePort
	Instances navigationcontract.PresentationStalePort
	Queue     navigationcontract.MenuStaleEnqueuePort

	Tx TxRunner

	Now func() time.Time
}

func NewTranslationWriter(deps TranslationWriterDeps) *TranslationWriter {
	w := &TranslationWriter{
		rows: deps.Rows, sources: deps.Sources, projects: deps.Projects, content: deps.Content,
		pages: deps.Pages, instances: deps.Instances, queue: deps.Queue, tx: deps.Tx, now: deps.Now,
	}
	if w.now == nil {
		w.now = func() time.Time { return time.Now().UTC() }
	}
	return w
}

var _ navigationcontract.TranslationWriteService = (*TranslationWriter)(nil)

type RowSourceTx interface {
	ListForKindTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (rows []*navigationmodel.NavigationEntity, err error)
}

type rowSource = RowSourceTx

type SourceIndexerTx interface {
	OpenSourceIndexTx(ctx context.Context, tx *gorm.DB, projectID string) (navigationcontract.SourceIndex, error)
}

type ProjectLister interface {
	List(ctx context.Context) (res []ProjectEntry, err error)
}

type ProjectEntry struct {
	ID string
}

type ProjectListerFunc func(ctx context.Context) ([]ProjectEntry, error)

func (f ProjectListerFunc) List(ctx context.Context) ([]ProjectEntry, error) { return f(ctx) }

type ContentStore interface {
	LoadDetailsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)

	UpsertTx(ctx context.Context, tx *gorm.DB, items []i18n.ContentWriteItem) (written int, err error)
}

type TxRunner interface {
	Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
}

func (w *TranslationWriter) WriteTranslations(ctx context.Context, req *navigationcontract.TranslationWriteReq) (*navigationcontract.TranslationWriteResult, error) {
	if w == nil {
		return nil, navigationenums.ErrTranslationWriteUnavailableError()
	}
	if req == nil {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	projectID, lang := strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.Lang)

	canonical, perr := uuid.Parse(projectID)
	if perr != nil {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	projectID = canonical.String()

	if !navigationTranslationLangSupported(lang) {
		return nil, facing.WithStatus(navigationenums.Err(navigationenums.ErrInvalidParam), http.StatusBadRequest)
	}

	if err := w.requireDeps(); err != nil {
		return nil, err
	}

	if err := w.requireProject(ctx, projectID); err != nil {
		return nil, err
	}

	kept := make([]navigationcontract.TranslationWriteRow, 0, len(req.Rows))
	for _, row := range req.Rows {
		target := strings.TrimSpace(row.Target)
		if target == "" {
			continue
		}
		kept = append(kept, navigationcontract.TranslationWriteRow{
			Context: strings.TrimSpace(row.Context), Hash: strings.TrimSpace(row.Hash), Target: target,
		})
	}
	if len(kept) == 0 {

		return &navigationcontract.TranslationWriteResult{Written: 0, ChangedKinds: []string{}}, nil
	}

	contextName := navigationLabelContext()
	if err := validateContexts(kept, contextName); err != nil {
		return nil, err
	}
	if err := rejectDuplicates(kept, contextName); err != nil {
		return nil, err
	}

	var result *navigationcontract.TranslationWriteResult
	err := w.tx.Transaction(ctx, func(tx *gorm.DB) error {

		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}

		if lerr := i18n.LockProjectContentWrite(ctx, tx, projectID); lerr != nil {
			return lerr
		}

		index, ierr := w.sources.OpenSourceIndexTx(ctx, tx, projectID)
		if ierr != nil {
			return ierr
		}
		if rerr := index.Require(); rerr != nil {
			return rerr
		}

		items := make([]i18n.ContentWriteItem, 0, len(kept))
		keys := make([]string, 0, len(kept))
		kindsOf := make([][]string, 0, len(kept))
		if verr := validateSources(kept, contextName, projectID, lang, index, &items, &keys, &kindsOf); verr != nil {
			return verr
		}

		hashes := make([]string, 0, len(items))
		for _, it := range items {
			hashes = append(hashes, it.SourceHash)
		}
		before, berr := w.content.LoadDetailsForProjectTx(ctx, tx, projectID, lang, hashes)
		if berr != nil {
			return berr
		}

		pending := make([]i18n.ContentWriteItem, 0, len(items))
		changedKinds := make([]string, 0, len(navigationSourceKinds))
		seenKind := make(map[string]bool, len(navigationSourceKinds))
		for i, item := range items {
			prev := before[keys[i]]
			if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
				continue
			}
			pending = append(pending, item)
			for _, kind := range kindsOf[i] {
				if !seenKind[kind] {
					seenKind[kind] = true
					changedKinds = append(changedKinds, kind)
				}
			}
		}
		if len(pending) == 0 {

			result = &navigationcontract.TranslationWriteResult{Written: 0, ChangedKinds: []string{}}
			return nil
		}

		written, uerr := w.content.UpsertTx(ctx, tx, pending)
		if uerr != nil {
			return uerr
		}

		kinds := sortKinds(changedKinds)
		at := w.now()
		for _, kind := range kinds {
			if _, merr := w.pages.MarkMenuStaleTx(ctx, tx, projectID, kind, at); merr != nil {
				return merr
			}
			if _, merr := w.instances.MarkInstanceMenuStaleTx(ctx, tx, projectID, kind, at); merr != nil {
				return merr
			}
			if _, qerr := w.queue.EnqueueMenuStaleTx(ctx, tx, projectID, kind); qerr != nil {
				return qerr
			}
		}
		result = &navigationcontract.TranslationWriteResult{Written: written, ChangedKinds: kinds}
		return nil
	})
	if err != nil {

		return nil, w.classify(ctx, projectID, lang, err)
	}
	if result == nil {
		return &navigationcontract.TranslationWriteResult{Written: 0, ChangedKinds: []string{}}, nil
	}
	logger.SceneCtx("navigation", ctx).With("project", projectID).With("lang", lang).
		With("written", result.Written).With("kinds", strings.Join(result.ChangedKinds, ",")).
		Info("导航译文已写入并同事务派发失效")
	return result, nil
}

func (w *TranslationWriter) classify(ctx context.Context, projectID, lang string, err error) error {
	var blocked *TranslationRejectedError
	if errors.As(err, &blocked) {

		return err
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {

		return err
	}
	logger.SceneCtx("navigation", ctx).With("project", projectID).With("lang", lang).
		Error(err, "导航译文写入失败，事务已回滚")
	return navigationenums.ErrTranslationWriteFailedError()
}

func (w *TranslationWriter) requireDeps() error {
	missing := make([]string, 0, 7)
	if w.projects == nil {
		missing = append(missing, "projects")
	}
	if w.rows == nil {
		missing = append(missing, "rows")
	}
	if w.sources == nil {
		missing = append(missing, "sources")
	}
	if w.content == nil {
		missing = append(missing, "content")
	}
	if w.pages == nil {
		missing = append(missing, "pages")
	}
	if w.instances == nil {
		missing = append(missing, "instances")
	}
	if w.queue == nil {
		missing = append(missing, "queue")
	}
	if w.tx == nil {
		missing = append(missing, "tx")
	}
	if len(missing) == 0 {
		return nil
	}

	logger.Scene("navigation").With("missing", strings.Join(missing, ",")).
		Warn("导航译文写服务装配不全，拒绝写入")
	return navigationenums.ErrTranslationWriteUnavailableError()
}

func (w *TranslationWriter) requireProject(ctx context.Context, projectID string) error {
	list, err := w.projects.List(ctx)
	if err != nil {

		logger.SceneCtx("navigation", ctx).With("project", projectID).Error(err, "读取工程清单失败，无法校验译文目标工程")
		return navigationenums.ErrTranslationWriteFailedError()
	}
	for _, p := range list {
		if strings.EqualFold(strings.TrimSpace(p.ID), projectID) {
			return nil
		}
	}
	return facing.WithStatus(navigationenums.Err(navigationenums.ErrInvalidParam), http.StatusBadRequest)
}

func validateContexts(rows []navigationcontract.TranslationWriteRow, contextName string) error {
	items := make([]facing.BlockedItem, 0, len(rows))
	for _, row := range rows {
		if row.Context == contextName {
			continue
		}

		items = append(items, facing.BlockedItem{
			ID: row.Hash, Name: row.Hash,
			ReasonKey: navigationenums.ErrContextInvalid,
			Fallback:  navigationenums.FallbackFor(navigationenums.ErrContextInvalid),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return &TranslationRejectedError{
		Err:   facing.WithStatus(navigationenums.Err(navigationenums.ErrContextInvalid), http.StatusBadRequest),
		Items: items,
	}
}

func rejectDuplicates(rows []navigationcontract.TranslationWriteRow, contextName string) error {
	seen := make(map[string]int, len(rows))
	items := make([]facing.BlockedItem, 0, len(rows))
	for i, row := range rows {
		key := i18n.ContentIndexKey(row.Hash, contextName)
		first, ok := seen[key]
		if !ok {
			seen[key] = i
			continue
		}

		items = append(items, facing.BlockedItem{
			ID:        row.Hash,
			Name:      fmt.Sprintf("%s / %s", rows[first].Target, row.Target),
			ReasonKey: navigationenums.ErrTranslationDuplicate,
			Fallback:  navigationenums.FallbackFor(navigationenums.ErrTranslationDuplicate),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return &TranslationRejectedError{Err: navigationenums.ErrTranslationDuplicateError(), Items: items}
}

func validateSources(rows []navigationcontract.TranslationWriteRow, contextName, projectID, lang string,
	index navigationcontract.SourceIndex, items *[]i18n.ContentWriteItem, keys *[]string, kindsOf *[][]string) error {
	var unknown []facing.BlockedItem
	var untranslatable []facing.BlockedItem
	for _, row := range rows {
		entry, ok := index.Locate(row.Hash)
		if !ok {

			unknown = append(unknown, facing.BlockedItem{
				ID: row.Hash, Name: row.Hash,
				ReasonKey: navigationenums.ErrSourceChanged,
				Fallback:  navigationenums.FallbackFor(navigationenums.ErrSourceChanged),
			})
			continue
		}
		if !i18n.ShouldTranslateContent(entry.Source) {
			untranslatable = append(untranslatable, facing.BlockedItem{
				ID: entry.Hash, Name: sourceNameOrHash(entry.Source, entry.Hash),
				ReasonKey: navigationenums.ErrNotTranslatable,
				Fallback:  navigationenums.FallbackFor(navigationenums.ErrNotTranslatable),
			})
			continue
		}
		*items = append(*items, i18n.ContentWriteItem{

			ProjectID:  projectID,
			SourceHash: entry.Hash, Context: contextName, Lang: lang,
			SourceText: entry.Source, TargetText: row.Target, Engine: i18n.ContentEngineManual,
		})
		*keys = append(*keys, i18n.ContentIndexKey(entry.Hash, contextName))

		*kindsOf = append(*kindsOf, append([]string{}, entry.Kinds...))
	}

	if len(unknown) > 0 {
		return &TranslationRejectedError{
			Err:   facing.WithStatus(navigationenums.Err(navigationenums.ErrSourceChanged), http.StatusBadRequest),
			Items: unknown,
		}
	}
	if len(untranslatable) > 0 {
		return &TranslationRejectedError{
			Err:   navigationenums.ErrNotTranslatableRejected(),
			Items: untranslatable,
		}
	}
	return nil
}

func sourceNameOrHash(source, hash string) string {
	if s := strings.TrimSpace(source); s != "" {
		return s
	}
	return hash
}

func navigationTranslationLangSupported(lang string) bool {
	for _, code := range i18n.AvailableLangs() {
		if code == lang {
			return true
		}
	}
	return false
}

func sortKinds(kinds []string) []string {
	out := append([]string{}, kinds...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && navigationKindRank(out[j]) < navigationKindRank(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
