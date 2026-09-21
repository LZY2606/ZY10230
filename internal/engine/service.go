package engine

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"fossilcourt/internal/store"
)

type Service struct {
	eng *Engine
	db  *store.DB
}

func NewService(db *store.DB) *Service {
	return &Service{eng: New(db), db: db}
}

func (svc *Service) State() (*State, error) {
	s, err := svc.eng.load()
	if err != nil {
		return nil, err
	}
	computed := computeEvents(s)
	horizons := computeHorizons(computed)

	// 从持久层读取事件修订与证据
	persisted, err := svc.loadPersistedEvents()
	if err != nil {
		return nil, err
	}
	var events []Event
	for _, ce := range computed {
		ev := ce.ev
		if pe, ok := persisted[ev.ID]; ok {
			ev.Revision = pe.revision
			ev.UpdatedAt = pe.updatedAt
		} else {
			ev.Revision = 1
			ev.UpdatedAt = ""
		}
		events = append(events, ev)
	}

	schemes := perSectionSchemes(s, computed, horizons)
	var conflictTies []string
	for _, interp := range []string{"raw", "in_situ"} {
		gs, ids := globalScheme(s, horizons, interp, nil)
		schemes = append(schemes, gs)
		if len(ids) > 0 {
			for _, id := range ids {
				conflictTies = append(conflictTies, interp+":"+id)
			}
		}
	}

	var groups []Group
	for _, g := range s.groups {
		groups = append(groups, g)
	}
	var synv []SynVersion
	rows, err := svc.db.Sql.Query(`SELECT version,created_at,kind,group_id,display_name,members,note FROM syn_versions ORDER BY version`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v SynVersion
		var members string
		if err := rows.Scan(&v.Version, &v.CreatedAt, &v.Kind, &v.GroupID, &v.DisplayName, &members, &v.Note); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(members), &v.Members)
		synv = append(synv, v)
	}
	rows.Close()

	var rw []ReworkCandidate
	for _, r := range s.rework {
		rw = append(rw, r)
	}
	sort.Slice(rw, func(i, j int) bool { return rw[i].OccurrenceID < rw[j].OccurrenceID })

	tiesOut := make([]Tie, len(s.ties))
	horizonSet := map[string]bool{}
	for _, h := range horizons {
		if h.DepthRaw != nil {
			horizonSet[horizonID(h.GroupID, h.Kind, h.SectionID)] = true
		}
	}
	for i, t := range s.ties {
		a := horizonID(t.AGroup, t.AKind, t.ASection)
		b := horizonID(t.BGroup, t.BKind, t.BSection)
		t.Resolvable = horizonSet[a] && horizonSet[b]
		tiesOut[i] = t
	}

	var logs []RecomputeEntry
	lrows, err := svc.db.Sql.Query(`SELECT id,at,cause,scope,recomputed,preserved,detail FROM recompute_log ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	for lrows.Next() {
		var l RecomputeEntry
		if err := lrows.Scan(&l.ID, &l.At, &l.Cause, &l.Scope, &l.Recomputed, &l.Preserved, &l.Detail); err != nil {
			lrows.Close()
			return nil, err
		}
		logs = append(logs, l)
	}
	lrows.Close()

	return &State{
		SchemaVersion: store.SchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Sections:      s.sections,
		Taxa:          s.taxa,
		Intervals:     s.intervals,
		Occurrences:   s.occs,
		Statuses:      deriveStatuses(s),
		Groups:        groups,
		SynVersions:   synv,
		Rework:        rw,
		Ties:          tiesOut,
		Horizons:      horizons,
		Events:        events,
		Schemes:       schemes,
		RecomputeLog:  logs,
	}, nil
}

type persistedEvent struct {
	revision  int
	updatedAt string
}

func (svc *Service) loadPersistedEvents() (map[string]persistedEvent, error) {
	out := map[string]persistedEvent{}
	rows, err := svc.db.Sql.Query(`SELECT id,revision,updated_at FROM events`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, at string
		var rev int
		if err := rows.Scan(&id, &rev, &at); err != nil {
			return nil, err
		}
		out[id] = persistedEvent{revision: rev, updatedAt: at}
	}
	return out, nil
}

// affectedGroups 根据变更前后的成员集合，计算受影响的等义组（显式组或单体~组）。
func affectedGroups(before, after []Group, taxonSet map[string]bool) map[string]bool {
	aff := map[string]bool{}
	consider := func(groups []Group) {
		for _, g := range groups {
			for _, m := range g.Members {
				if taxonSet[m] {
					aff[g.ID] = true
				}
			}
		}
	}
	consider(before)
	consider(after)
	return aff
}

func currentGroups(db *store.DB) ([]Group, error) {
	tmp := New(db)
	return tmp.loadGroups()
}

// inScope 判断某计算事件是否依赖受影响分类单元的产出，或其组在受影响组集合内。
func eventInScope(ce computedEvent, taxa map[string]bool, groups map[string]bool) bool {
	if groups[ce.ev.GroupID] {
		return true
	}
	if ce.ev.PartnerGroup != "" && groups[ce.ev.PartnerGroup] {
		return true
	}
	for _, oid := range ce.memberOC {
		// oid 形如 O-..，由 occurrence id 直接查 taxa 集合（调用方已转为 occ->taxon）
		if taxa[oid] {
			return true
		}
	}
	return false
}

var _ = sql.NullString{}
var _ = fmt.Sprintf
var _ = strings.TrimSpace
