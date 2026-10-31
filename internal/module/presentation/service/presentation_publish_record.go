package presentationservice

import (
	"context"
	"strings"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
)

func (s *Service) PublishRecords(ctx context.Context, projectID, entityType string, entityIDs []string) ([]presentationcontract.PublishRecordRow, error) {
	projectID, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		return []presentationcontract.PublishRecordRow{}, nil
	}
	instances, err := s.currentInstances(ctx, projectID, entityType, entityIDs)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return []presentationcontract.PublishRecordRow{}, nil
	}

	ids := make([]string, 0, len(instances))
	for _, inst := range instances {
		ids = append(ids, inst.ID)
	}
	pubs, err := s.m.ListPublicationsByInstances(ctx, ids)
	if err != nil {
		return nil, err
	}
	byInstance := make(map[string][]presentationmodel.PublicationEntity, len(ids))
	artifactIDs := make([]string, 0, len(pubs))
	for _, p := range pubs {
		byInstance[p.PresentationID] = append(byInstance[p.PresentationID], p)
		if p.ArtifactID != nil {
			artifactIDs = append(artifactIDs, *p.ArtifactID)
		}
	}
	versions, err := s.m.ListArtifactVersionFacts(ctx, artifactIDs)
	if err != nil {
		return nil, err
	}
	versionOf := make(map[string]presentationmodel.ArtifactVersionFact, len(versions))
	for _, v := range versions {
		versionOf[v.ID] = v
	}

	rows := make([]presentationcontract.PublishRecordRow, 0, len(instances))
	for _, inst := range instances {
		langs := byInstance[inst.ID]
		if len(langs) == 0 {
			rows = append(rows, instanceRow(inst, presentationmodel.PublicationEntity{}, presentationmodel.ArtifactVersionFact{}))
			continue
		}
		for _, pub := range langs {
			fact := presentationmodel.ArtifactVersionFact{}
			if pub.ArtifactID != nil {
				fact = versionOf[*pub.ArtifactID]

				if fact.ArtifactHash == "" {
					fact.ArtifactHash = pub.ArtifactHash
				}
			}
			rows = append(rows, instanceRow(inst, pub, fact))
		}
	}
	return rows, nil
}

func instanceRow(inst *presentationmodel.InstanceEntity, pub presentationmodel.PublicationEntity,
	fact presentationmodel.ArtifactVersionFact) presentationcontract.PublishRecordRow {
	row := presentationcontract.PublishRecordRow{
		InstanceID:      inst.ID,
		EntityType:      inst.EntityType,
		EntityID:        inst.EntityID,
		InstanceRole:    inst.InstanceRole,
		URLPath:         inst.URLPath,
		Lang:            pub.Lang,
		ActivePath:      pub.ActivePath,
		ArtifactVersion: fact.Version,
		ArtifactHash:    fact.ArtifactHash,
		PublishedAt:     pub.PublishedAt,
		Stale:           inst.Stale,
		Active:          inst.ActiveArtifactID != nil && *inst.ActiveArtifactID != "",
	}
	return row
}

func (s *Service) currentInstances(ctx context.Context, projectID, entityType string,
	entityIDs []string) ([]*presentationmodel.InstanceEntity, error) {
	ids := normalizeEntityIDs(entityIDs)
	if len(ids) == 0 {
		return s.m.ListInstances(ctx, projectID, entityType)
	}
	return s.m.ListInstanceFactsByEntities(ctx, projectID, entityType, ids)
}

func normalizeEntityIDs(entityIDs []string) []string {
	out := make([]string, 0, len(entityIDs))
	seen := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
