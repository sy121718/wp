package i18n

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync/atomic"
)

var contentSkipValue = regexp.MustCompile(`^[\p{N}\p{P}\p{S}\s]*$`)

func ContentHash(sourceText string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceText)))
	return hex.EncodeToString(sum[:])
}

func ShouldTranslateContent(sourceText string) bool {
	s := strings.TrimSpace(sourceText)
	return s != "" && !contentSkipValue.MatchString(s)
}

func ContentContext(typ, field string) string {
	typ = strings.TrimSpace(typ)
	field = strings.TrimSpace(field)
	if typ == "" || field == "" {
		return ""
	}
	return typ + "." + field
}

const contentIndexSep = "\x00"

func ContentIndexKey(sourceHash, contextName string) string {
	return sourceHash + contentIndexSep + contextName
}

type ContentTranslator struct {
	lang string

	projectID string
	index     map[string]string
	misses    atomic.Int64
}

func NewContentTranslator(ctx context.Context, lang string, hashes []string) *ContentTranslator {
	return NewContentTranslatorWith(ctx, defaultContentStore(), lang, hashes)
}

func NewContentTranslatorWith(ctx context.Context, store ContentStore, lang string, hashes []string) *ContentTranslator {
	return NewContentTranslatorScoped(ctx, "", store, lang, hashes)
}

func NewContentTranslatorScoped(ctx context.Context, projectID string, store ContentStore, lang string, hashes []string) *ContentTranslator {
	projectID = strings.TrimSpace(projectID)
	if store == nil {
		store = defaultProjectContentStore(projectID)
	} else {
		store = ContentStoreForProject(store, projectID)
	}
	t := &ContentTranslator{lang: strings.TrimSpace(lang), projectID: projectID, index: map[string]string{}}
	if store == nil {
		return t
	}

	targets, err := store.LoadTargets(ctx, t.lang, hashes)
	if err != nil {

		logContentStoreFailure(t.lang, err)
		return t
	}

	for key, target := range targets {
		if target != "" {
			t.index[key] = target
		}
	}
	return t
}

func (t *ContentTranslator) TranslateContent(sourceText, contentContext string) string {
	if t == nil {
		return sourceText
	}
	if !ShouldTranslateContent(sourceText) {
		return sourceText
	}

	contentContext = strings.TrimSpace(contentContext)
	if contentContext == "" {
		return sourceText
	}

	if target, ok := t.index[ContentIndexKey(ContentHash(sourceText), contentContext)]; ok && target != "" {
		return target
	}

	t.misses.Add(1)
	return sourceText
}

func (t *ContentTranslator) Lang() string {
	if t == nil {
		return ""
	}
	return t.lang
}

func (t *ContentTranslator) ProjectID() string {
	if t == nil {
		return ""
	}
	return t.projectID
}

func (t *ContentTranslator) Size() int {
	if t == nil {
		return 0
	}
	return len(t.index)
}

func (t *ContentTranslator) Misses() int64 {
	if t == nil {
		return 0
	}
	return t.misses.Load()
}
