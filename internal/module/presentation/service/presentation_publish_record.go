package presentationservice

// presentation_publish_record.go — 发布记录页的读模型（B9）。
//
// 一屏一个方法：按实体批量回答「现在发布成什么样了」。数据来自三张表 ——
//   presentation_instances    实例身份 + active 指针 + stale（带 FORCE 策略，必须工程作用域）
//   presentation_publications 每语言的激活路径 / 产物 / 发布时间（唯一真源）
//   presentation_artifacts    激活产物的版本号（账本只存 id 与 hash，版本号要单独取）
//
// 三张表的读法只在这一处：调用方（商品页）拿到的是拼好的行。让页面自己拿实例再逐行
// 查产物，等于把「哪一列才算已发布」这条判据复制到别的模块。
//
// **兼容边界（报告里也写了）**：库里没有「每次发布尝试成功/失败」的历史明细表，
// 回执 publication_receipts 只覆盖访问面切换那一段且完成后只留终态。所以这一屏回答的是
// **当前**发布状态，不是发布历史；不为了「像 dy_erp」而伪造历史行。

import (
	"context"
	"strings"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
)

// PublishRecords 按实体批量读当前发布登记（见 presentationcontract.PublishRecordPort）。
//
// **从未发布过的实例也给一行**：Lang / ActivePath / PublishedAt 空、Status=draft。
// 反过来（只回有语言账本的行）会让「建了详情页实例但从没发成功」的实体从页面消失，
// 而那正是最需要被看见的一档。
func (s *Service) PublishRecords(ctx context.Context, projectID, entityType string, entityIDs []string) ([]presentationcontract.PublishRecordRow, error) {
	projectID, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		return []presentationcontract.PublishRecordRow{}, nil
	}
	all, err := s.m.ListInstances(ctx, projectID, entityType)
	if err != nil {
		return nil, err
	}
	instances := filterInstancesByEntity(all, entityIDs)
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
				// 账本指向的产物行可能已被 GC 删掉：版本号留 0、hash 照账本给 ——
				// 这是「记录还在、产物没了」的真实状态，不该被吞成「没发布」。
				if fact.ArtifactHash == "" {
					fact.ArtifactHash = pub.ArtifactHash
				}
			}
			rows = append(rows, instanceRow(inst, pub, fact))
		}
	}
	return rows, nil
}

// instanceRow 一条当前登记行。
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

// filterInstancesByEntity 只留指定实体（entityIDs 为空 = 全部），保持入参顺序。
func filterInstancesByEntity(all []*presentationmodel.InstanceEntity, entityIDs []string) []*presentationmodel.InstanceEntity {
	if len(entityIDs) == 0 {
		return all
	}
	wanted := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	if len(wanted) == 0 {
		return all
	}
	out := make([]*presentationmodel.InstanceEntity, 0, len(wanted))
	for _, inst := range all {
		if wanted[inst.EntityID] {
			out = append(out, inst)
		}
	}
	return out
}
