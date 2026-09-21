package engine

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"fossilcourt/internal/store"
)

type Engine struct {
	db *store.DB
}

func New(db *store.DB) *Engine { return &Engine{db: db} }

type snapshot struct {
	sections  []Section
	secByID   map[string]Section
	taxa      []Taxon
	taxonByID map[string]Taxon
	intervals []Interval
	ivlByID   map[string]Interval
	occs      []Occurrence
	groups    []Group
	rework    map[string]ReworkCandidate
	ties      []Tie
}

func (e *Engine) load() (*snapshot, error) {
	s := &snapshot{
		secByID:   map[string]Section{},
		taxonByID: map[string]Taxon{},
		ivlByID:   map[string]Interval{},
		rework:    map[string]ReworkCandidate{},
	}
	rows, err := e.db.Sql.Query(`SELECT id,name,ord,depth_unit FROM sections ORDER BY ord,id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x Section
		if err := rows.Scan(&x.ID, &x.Name, &x.Ord, &x.DepthUnit); err != nil {
			rows.Close()
			return nil, err
		}
		s.sections = append(s.sections, x)
		s.secByID[x.ID] = x
	}
	rows.Close()

	rows, err = e.db.Sql.Query(`SELECT id,name FROM taxa ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x Taxon
		if err := rows.Scan(&x.ID, &x.Name); err != nil {
			rows.Close()
			return nil, err
		}
		s.taxa = append(s.taxa, x)
		s.taxonByID[x.ID] = x
	}
	rows.Close()

	rows, err = e.db.Sql.Query(`SELECT id,section_id,top_depth,base_depth,sufficient,note FROM sample_intervals ORDER BY section_id,base_depth`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x Interval
		var suff int
		if err := rows.Scan(&x.ID, &x.SectionID, &x.TopDepth, &x.BaseDepth, &suff, &x.Note); err != nil {
			rows.Close()
			return nil, err
		}
		x.Sufficient = suff == 1
		s.intervals = append(s.intervals, x)
		s.ivlByID[x.ID] = x
	}
	rows.Close()

	rows, err = e.db.Sql.Query(`SELECT o.id,o.section_id,o.taxon_id,o.depth,o.status,o.interval_id,o.note,
		COALESCE(r.marked,0)
		FROM occurrences o LEFT JOIN rework_candidates r ON r.occurrence_id=o.id
		ORDER BY o.section_id,o.depth,o.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x Occurrence
		var marked int
		var ivl sql.NullString
		if err := rows.Scan(&x.ID, &x.SectionID, &x.TaxonID, &x.Depth, &x.Status, &ivl, &x.Note, &marked); err != nil {
			rows.Close()
			return nil, err
		}
		x.IntervalID = ivl.String
		x.Rework = marked == 1
		s.occs = append(s.occs, x)
	}
	rows.Close()

	groups, err := e.loadGroups()
	if err != nil {
		return nil, err
	}
	s.groups = groups

	rows, err = e.db.Sql.Query(`SELECT occurrence_id,marked,updated_at,note FROM rework_candidates`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r ReworkCandidate
		var marked int
		if err := rows.Scan(&r.OccurrenceID, &marked, &r.UpdatedAt, &r.Note); err != nil {
			rows.Close()
			return nil, err
		}
		r.Marked = marked == 1
		s.rework[r.OccurrenceID] = r
	}
	rows.Close()

	rows, err = e.db.Sql.Query(`SELECT id,created_at,a_group,a_section,a_kind,b_group,b_section,b_kind,locked,active,conflicted,note FROM ties ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t Tie
		var locked, active, conflicted int
		if err := rows.Scan(&t.ID, &t.CreatedAt, &t.AGroup, &t.ASection, &t.AKind, &t.BGroup, &t.BSection, &t.BKind,
			&locked, &active, &conflicted, &t.Note); err != nil {
			rows.Close()
			return nil, err
		}
		t.Locked, t.Active, t.Conflicted = locked == 1, active == 1, conflicted == 1
		s.ties = append(s.ties, t)
	}
	rows.Close()
	return s, nil
}

func (e *Engine) loadGroups() ([]Group, error) {
	var out []Group
	rows, err := e.db.Sql.Query(`
		SELECT g.id,g.display_name,(SELECT MAX(version) FROM syn_versions v WHERE v.group_id=g.id)
		FROM syn_groups g ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g Group
		var ver sql.NullInt64
		if err := rows.Scan(&g.ID, &g.DisplayName, &ver); err != nil {
			rows.Close()
			return nil, err
		}
		g.Version = int(ver.Int64)
		out = append(out, g)
	}
	rows.Close()
	for i := range out {
		mrows, err := e.db.Sql.Query(`SELECT taxon_id FROM syn_members WHERE group_id=? ORDER BY taxon_id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			var m string
			if err := mrows.Scan(&m); err != nil {
				mrows.Close()
				return nil, err
			}
			out[i].Members = append(out[i].Members, m)
		}
		mrows.Close()
	}
	return out, nil
}

// deriveStatuses 在每个剖面 × 分类单元上区分四种状态：
// present 正常产出；absent 充分采样下未见（可作负证据）；
// insufficient 采样不足（未见但不能作负证据）；unsampled 完全未采样。
func deriveStatuses(s *snapshot) []TaxonStatus {
	var out []TaxonStatus
	ivls := map[string][]Interval{}
	for _, iv := range s.intervals {
		ivls[iv.SectionID] = append(ivls[iv.SectionID], iv)
	}
	occs := map[string][]Occurrence{}
	for _, o := range s.occs {
		occs[o.SectionID+"|"+o.TaxonID] = append(occs[o.SectionID+"|"+o.TaxonID], o)
	}
	for _, sec := range s.sections {
		for _, tx := range s.taxa {
			list := occs[sec.ID+"|"+tx.ID]
			st := TaxonStatus{SectionID: sec.ID, TaxonID: tx.ID}
			hasPresent := false
			for _, o := range list {
				if o.Status == "present" {
					hasPresent = true
					st.Evidence = append(st.Evidence, o.ID)
				}
			}
			switch {
			case hasPresent:
				st.State = "present"
			default:
				suffAbsent, insufCoverage := false, false
				for _, o := range list {
					if o.Status != "absent" || o.IntervalID == "" {
						continue
					}
					iv := s.ivlByID[o.IntervalID]
					if iv.Sufficient {
						suffAbsent = true
						st.Evidence = append(st.Evidence, o.ID)
					} else {
						insufCoverage = true
						st.Evidence = append(st.Evidence, o.ID)
					}
				}
				switch {
				case suffAbsent:
					st.State = "absent"
				case insufCoverage:
					st.State = "insufficient"
				default:
					st.State = "unsampled"
				}
			}
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SectionID != out[j].SectionID {
			return out[i].SectionID < out[j].SectionID
		}
		return out[i].TaxonID < out[j].TaxonID
	})
	return out
}

// intervalContaining 返回覆盖某深度的采样层段（优先充分采样层段）。
func intervalContaining(s *snapshot, sectionID string, depth float64) *Interval {
	var best *Interval
	for i := range s.intervals {
		iv := &s.intervals[i]
		if iv.SectionID != sectionID {
			continue
		}
		if depth >= iv.TopDepth && depth <= iv.BaseDepth {
			if best == nil || (iv.Sufficient && !best.Sufficient) {
				best = iv
			}
		}
	}
	return best
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
