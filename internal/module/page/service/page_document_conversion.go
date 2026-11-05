package pageservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go_wp/pkg/builder"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	json "go_wp/pkg/jsonx"
	"gorm.io/gorm"
)

func (s *Service) ApplyDocumentConversions(ctx context.Context, projectID string, input []pagecontract.DocumentConversion, templates contenttemplatecontract.DraftDocumentWriterTx) ([]pagecontract.DocumentConversionRecord, error) {
	if _, err := uuid.Parse(projectID); err != nil || len(input) == 0 || len(input) > 32 {
		return nil, ErrInvalidParam
	}
	items := append([]pagecontract.DocumentConversion(nil), input...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].ID < items[j].ID
	})
	targetHashes := make([]string, len(items))
	for i := range items {
		item := &items[i]
		if (item.Kind != "page" && item.Kind != "template") || item.ExpectedVersion < 1 || len(item.ExpectedHash) != 64 {
			return nil, ErrInvalidParam
		}
		if _, err := uuid.Parse(item.ID); err != nil {
			return nil, ErrInvalidParam
		}
		if i > 0 && item.Kind == items[i-1].Kind && item.ID == items[i-1].ID {
			return nil, ErrInvalidParam
		}
		if item.Kind == "template" && templates == nil {
			return nil, ErrInvalidParam
		}
		parsed, err := builder.ParsePage(item.Document)
		if err != nil {
			return nil, ErrInvalidDocument
		}
		if _, err := builder.ValidatePageTolerant(parsed); err != nil {
			return nil, ErrInvalidDocument
		}
		item.Document, err = json.Marshal(parsed)
		if err != nil {
			return nil, err
		}
		targetHashes[i], err = builder.DocumentDigest(item.Document)
		if err != nil {
			return nil, err
		}
	}
	records := make([]pagecontract.DocumentConversionRecord, len(items))
	err := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		pages := make(map[string]*pagemodel.PageEntity)
		before, after := 0, 0
		var conflicts []string
		for i, item := range items {
			var version int64
			var document json.RawMessage
			if item.Kind == "page" {
				row, err := s.model.LockDraftTx(ctx, tx, projectID, item.ID)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrPageNotFound
				}
				if err != nil {
					return err
				}
				pages[item.ID], version, document = row, row.DraftVersion, row.DraftDocument
			} else {
				row, err := templates.LockDraftTx(ctx, tx, projectID, item.ID)
				if err != nil {
					return err
				}
				version, document = row.Version, row.Document
			}
			currentHash, err := builder.DocumentDigest(document)
			if err != nil {
				return err
			}
			switch {
			case version == item.ExpectedVersion && currentHash == item.ExpectedHash:
				before++
			case version == item.ExpectedVersion+1 && currentHash == targetHashes[i]:
				after++
			default:
				conflicts = append(conflicts, item.Kind+"/"+item.ID)
			}
			records[i] = pagecontract.DocumentConversionRecord{Kind: item.Kind, ID: item.ID,
				BeforeVersion: item.ExpectedVersion, AfterVersion: item.ExpectedVersion + 1,
				BeforeHash: item.ExpectedHash, AfterHash: targetHashes[i]}
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("%w: %s", ErrDraftVersionConflict, strings.Join(conflicts, ", "))
		}
		if after == len(items) {
			return nil
		}
		if before != len(items) {
			return fmt.Errorf("%w: %s", ErrDraftVersionConflict, "mixed conversion versions")
		}
		for _, item := range items {
			if item.Kind == "template" {
				if err := templates.SaveDraftDocumentTx(ctx, tx, projectID, item.ID, item.ExpectedVersion, item.Document); err != nil {
					return err
				}
				continue
			}
			row := pages[item.ID]
			now := time.Now().UTC()
			revision := &pagemodel.RevisionEntity{ID: uuid.NewString(), PageID: row.ID, Version: item.ExpectedVersion + 1,
				DraftPath: row.DraftPath, DraftDocument: item.Document, SourceHash: hash(item.Document), CreatedAt: now}
			if err := s.model.SaveDraftWithRevisionTx(ctx, tx, projectID, row.ID, item.ExpectedVersion,
				row.DraftPath, item.Document, revision.Version, now, revision); err != nil {
				return mapPersistenceError(err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}
