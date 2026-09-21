package engine

import (
	"encoding/json"

	"fossilcourt/internal/fixture"
)

// IsEmpty 判断业务表是否为空（用于首次启动自动落 fixture）。
func (svc *Service) IsEmpty() (bool, error) {
	var n int
	err := svc.db.Sql.QueryRow(`SELECT COUNT(*) FROM sections`).Scan(&n)
	return n == 0, err
}

// Seed 清空业务表并写入固定 fixture，随后全量计算事件（revision=1）。
func (svc *Service) Seed() error {
	return svc.SeedData(fixture.Fixed())
}

func (svc *Service) SeedData(d fixture.Data) error {
	tx, err := svc.db.Sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{
		"event_evidence", "events", "ties", "rework_candidates", "syn_members",
		"syn_versions", "syn_groups", "occurrences", "sample_intervals", "taxa",
		"sections", "recompute_log", "runs",
	} {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return err
		}
	}
	for _, s := range d.Sections {
		if _, err := tx.Exec(`INSERT INTO sections(id,name,ord,depth_unit) VALUES(?,?,?,?)`, s.ID, s.Name, s.Ord, s.Unit); err != nil {
			return err
		}
	}
	for _, t := range d.Taxa {
		if _, err := tx.Exec(`INSERT INTO taxa(id,name) VALUES(?,?)`, t.ID, t.Name); err != nil {
			return err
		}
	}
	for _, iv := range d.Intervals {
		suff := 0
		if iv.Sufficient {
			suff = 1
		}
		if _, err := tx.Exec(`INSERT INTO sample_intervals(id,section_id,top_depth,base_depth,sufficient,note) VALUES(?,?,?,?,?,?)`,
			iv.ID, iv.SectionID, iv.Top, iv.Base, suff, iv.Note); err != nil {
			return err
		}
	}
	for _, o := range d.Occurrences {
		var ivl interface{}
		if o.IntervalID != "" {
			ivl = o.IntervalID
		}
		if _, err := tx.Exec(`INSERT INTO occurrences(id,section_id,taxon_id,depth,status,interval_id,note) VALUES(?,?,?,?,?,?,?)`,
			o.ID, o.SectionID, o.TaxonID, o.Depth, o.Status, ivl, o.Note); err != nil {
			return err
		}
		if o.Rework {
			if _, err := tx.Exec(`INSERT INTO rework_candidates(occurrence_id,marked,note) VALUES(?,1,?)`, o.ID, o.Note); err != nil {
				return err
			}
		}
	}
	for _, g := range d.Groups {
		if _, err := tx.Exec(`INSERT INTO syn_groups(id,display_name) VALUES(?,?)`, g.ID, g.Name); err != nil {
			return err
		}
		for _, m := range g.Members {
			if _, err := tx.Exec(`INSERT INTO syn_members(group_id,taxon_id) VALUES(?,?)`, g.ID, m); err != nil {
				return err
			}
		}
		mj, _ := json.Marshal(g.Members)
		if _, err := tx.Exec(`INSERT INTO syn_versions(kind,group_id,display_name,members,note) VALUES('merge',?,?,?,?)`,
			g.ID, g.Name, string(mj), g.Note); err != nil {
			return err
		}
	}
	for _, t := range d.Ties {
		if _, err := tx.Exec(`INSERT INTO ties(id,a_group,a_section,a_kind,b_group,b_section,b_kind,note)
			VALUES(?,?,?,?,?,?,?,?)`,
			t.ID, t.AGroup, t.ASection, t.AKind, t.BGroup, t.BSection, t.BKind, t.Note); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO recompute_log(cause,scope,recomputed,preserved,detail) VALUES
		('seed','fixture',0,0,'固定 fixture 导入完成')`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// 全量计算并持久化初始事件
	return svc.persistAllEvents()
}

// persistAllEvents 全量（重新）持久化事件，保留既有 revision；用于 seed 与 import 后校验。
func (svc *Service) persistAllEvents() error {
	s, err := svc.eng.load()
	if err != nil {
		return err
	}
	computed := computeEvents(s)
	existing, err := svc.loadPersistedEvents()
	if err != nil {
		return err
	}
	tx, err := svc.db.Sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM event_evidence`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM events`); err != nil {
		return err
	}
	for _, ce := range computed {
		ev := ce.ev
		rev := 1
		if pe, ok := existing[ev.ID]; ok {
			rev = pe.revision
		}
		var d interface{}
		if ev.Depth != nil {
			d = *ev.Depth
		}
		if _, err := tx.Exec(`INSERT INTO events(id,revision,interpretation,kind,group_id,section_id,depth,partner_group,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, ev.ID, rev, ev.Interpretation, ev.Kind, ev.GroupID, ev.SectionID,
			d, nullStr(ev.PartnerGroup), utcNow()); err != nil {
			return err
		}
		for _, evd := range ev.Evidence {
			if _, err := tx.Exec(`INSERT INTO event_evidence(event_id,occurrence_id,interval_id,role,note) VALUES(?,?,?,?,?)`,
				ev.ID, nullStr(evd.OccurrenceID), nullStr(evd.IntervalID), evd.Role, evd.Note); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
