package buildmodel

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameBuildJobSeq = "build_job_seq"

func (m *Model) NextMenuStaleVersionTx(tx *gorm.DB, projectID, kind string) (version int64, err error) {
	if err := requireTx(tx); err != nil {
		return 0, err
	}

	got := seqRow{ProjectID: projectID, Kind: kind}
	res := tx.Table(tableNameBuildJobSeq).
		Omit("NextVersion").
		Clauses(
			clause.OnConflict{
				Columns: []clause.Column{{Name: "project_id"}, {Name: "kind"}},
				DoUpdates: clause.Assignments(map[string]any{

					"next_version": gorm.Expr(tableNameBuildJobSeq + ".next_version + 1"),
					"update_time":  gorm.Expr("now()"),
				}),
			},

			clause.Returning{Columns: []clause.Column{{Name: "next_version"}}},
		).
		Create(&got)
	if res.Error != nil {
		return 0, res.Error
	}
	if got.NextVersion <= 0 {

		return 0, gorm.ErrRecordNotFound
	}
	return got.NextVersion, nil
}

type seqRow struct {
	ProjectID   string `gorm:"column:project_id;primaryKey"`
	Kind        string `gorm:"column:kind;primaryKey"`
	NextVersion int64  `gorm:"column:next_version"`
}
