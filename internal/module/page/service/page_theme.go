package pageservice

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/builder"
	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/internal/pipeline"
)

func (s *Service) mergeActiveTheme(ctx context.Context, projectID string, doc json.RawMessage) (json.RawMessage, error) {
	return pipeline.MergeActiveThemeIntoDocument(ctx, s.project, projectID, doc)
}

func (s *Service) ActiveThemeID(ctx context.Context, projectID string) string {
	theme, err := s.project.GetActiveTheme(ctx, projectID)
	if err != nil || theme == nil {
		return ""
	}
	return theme.ID
}

func (s *Service) RefreshThemeForTheme(ctx context.Context, themeID string, theme json.RawMessage) error {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		rows, err := s.model.ListThemePageSnapshots(ctx, projectID, themeID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			snapshot := json.RawMessage(`{}`)
			if merged := builder.MergeThemeRawJSON(theme, row.Override); len(merged) > 0 {
				snapshot = merged
			}

			snapshot = builder.InjectThemeID(snapshot, themeID)
			if err := s.model.UpdateThemeSnapshot(ctx, projectID, row.ID, snapshot); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) RefreshStructureForTheme(ctx context.Context, themeID string, structure json.RawMessage) error {
	var themeBindings builder.StructureBindings
	if len(structure) > 0 {
		if err := json.Unmarshal(structure, &themeBindings); err != nil {
			return err
		}
	}
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		rows, err := s.model.ListThemePageStructureSnapshots(ctx, projectID, themeID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			pageBindings := builder.StructureBindings{}
			if len(row.Structure) > 0 {
				if err := json.Unmarshal(row.Structure, &pageBindings); err != nil {
					return err
				}
			}
			merged := pipeline.MergeStructureBindings(pageBindings, themeBindings)
			structureJSON, err := json.Marshal(merged)
			if err != nil {
				return err
			}
			if err := s.model.UpdateStructureSnapshot(ctx, projectID, row.ID, structureJSON); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) MarkStaleForTheme(ctx context.Context, themeID string) (ids []string, err error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, merr := s.model.MarkStaleForTheme(ctx, projectID, themeID)
		if merr != nil {
			return nil, merr
		}
		hit.add(ids)
	}
	affected := hit.list()
	s.logStaleImpact(ctx, "theme:"+themeID, affected)
	return affected, nil
}

func (s *Service) MarkStaleForBlock(ctx context.Context, blockID string) (ids []string, err error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, merr := s.model.MarkStaleForBlock(ctx, projectID, blockID)
		if merr != nil {
			return nil, merr
		}
		hit.add(ids)
	}
	affected := hit.list()
	s.logStaleImpact(ctx, "block:"+blockID, affected)
	return affected, nil
}

func (s *Service) CountBlockReference(ctx context.Context, blockID string) (int64, error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		n, cerr := s.model.CountBlockReference(ctx, projectID, blockID)
		if cerr != nil {
			return 0, cerr
		}
		total += n
	}
	return total, nil
}

func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]blockcontract.BlockUsage, 0, 2)
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.model.ListBlockSourceRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range rows {
			row := rows[i]
			label := strings.TrimSpace(row.Path)
			if label == "" {
				label = row.ID
			}

			if row.InDocument {
				out = append(out, blockcontract.BlockUsage{
					Kind: blockcontract.UsageKindPageDocument, ProjectID: projectID, EntityID: row.ID, Label: label,
				})
			}
			if row.InStructure {
				out = append(out, blockcontract.BlockUsage{
					Kind: blockcontract.UsageKindPageStructure, ProjectID: projectID, EntityID: row.ID,
					Label: label, Detail: "header/footer/slots",
				})
			}
		}

		revs, rerr := s.model.ListBlockRevisionRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range revs {
			label := strings.TrimSpace(revs[i].Path)
			if label == "" {
				label = revs[i].PageID
			}
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindPageRevision, ProjectID: projectID, EntityID: revs[i].PageID,
				Label: label, Detail: fmt.Sprintf("revision v%d", revs[i].Version),
			})
		}
	}
	return out, nil
}

func (s *Service) AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) error {
	return s.model.AttachThemeToUnassigned(ctx, projectID, themeID)
}

func (s *Service) ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) error {
	return s.model.ReattachProjectPagesToTheme(ctx, projectID, themeID)
}

func (s *Service) ReskinProjectForTheme(ctx context.Context, projectID, themeID string, theme, structure json.RawMessage) error {
	var themeBindings builder.StructureBindings
	if len(structure) > 0 {
		if err := json.Unmarshal(structure, &themeBindings); err != nil {
			return err
		}
	}
	return s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.model.ReattachProjectPagesToThemeTx(ctx, tx, projectID, themeID); err != nil {
			return err
		}
		rows, err := s.model.ListThemePageSnapshotsTx(ctx, tx, projectID, themeID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, row := range rows {
			snapshot := json.RawMessage(`{}`)
			if merged := builder.MergeThemeRawJSON(theme, row.Override); len(merged) > 0 {
				snapshot = merged
			}

			snapshot = builder.InjectThemeID(snapshot, themeID)
			if err := s.model.UpdateThemeSnapshotTx(ctx, tx, projectID, row.ID, snapshot, now); err != nil {
				return err
			}
		}
		structRows, err := s.model.ListThemePageStructureSnapshotsTx(ctx, tx, projectID, themeID)
		if err != nil {
			return err
		}

		structureJSON, err := json.Marshal(themeBindings)
		if err != nil {
			return err
		}
		for _, row := range structRows {
			if err := s.model.UpdateStructureSnapshotTx(ctx, tx, projectID, row.ID, structureJSON, now); err != nil {
				return err
			}
		}
		return s.model.MarkStaleForThemeTx(ctx, tx, projectID, themeID, now)
	})
}

func (s *Service) pageContextOf(ctx context.Context, pageID, lang string) (projectID, logicalPath string) {
	return s.pageContextOfWithDefault(ctx, pageID, lang, "")
}

func (s *Service) pageContextOfWithDefault(ctx context.Context, pageID, lang, defaultLang string) (projectID, logicalPath string) {
	if strings.TrimSpace(pageID) == "" {
		return "", ""
	}

	page, err := s.locatePageInProjects(ctx, pageID)
	if err != nil || page == nil {
		return "", ""
	}
	logicalPath = page.DraftPath
	if pub, perr := s.model.GetPublication(ctx, pageID, buildLang(lang)); perr == nil && pub != nil && pub.ActivePath != "" {
		rule := s.langURLRuleOf(ctx, page.ProjectID)
		if dl := strings.TrimSpace(defaultLang); dl != "" {
			rule = pipeline.LangURLRuleForProjectWithDefault(ctx, s.project, page.ProjectID, dl)
		}
		logicalPath = rule.Strip(buildLang(lang), pub.ActivePath)
	}
	return page.ProjectID, logicalPath
}
