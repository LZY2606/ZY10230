package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"fossilcourt/internal/store"
)

type reconcileReport struct {
	recomputed int
	preserved  int
	scopedIDs  []string
}

// reconcileEvents 只重算依赖集合内的事件，其余事件原样保留并计入 preserved。
// affectedTaxa：成员变更涉及的分类单元；affectedOcc：重工标记变更涉及的产出 id；
// affectedGroups：显式/单体等义组 id。
func (svc *Service) reconcileEvents(cause, scopeText string, affectedTaxa, affectedOcc map[string]bool, affectedGroups map[string]bool) (reconcileReport, error) {
	s, err := svc.eng.load()
	if err != nil {
		return reconcileReport{}, err
	}
	computed := computeEvents(s)
	existing, err := svc.loadPersistedEvents()
	if err != nil {
		return reconcileReport{}, err
	}
	// 产出 id -> 分类单元
	occTaxon := map[string]string{}
	for _, o := range s.occs {
		occTaxon[o.ID] = o.TaxonID
	}
	inScope := func(ce computedEvent) bool {
		ev := ce.ev
		if affectedGroups[ev.GroupID] || (ev.PartnerGroup != "" && affectedGroups[ev.PartnerGroup]) {
			return true
		}
		for _, oid := range ce.memberOC {
			if affectedOcc[oid] || affectedTaxa[occTaxon[oid]] {
				return true
			}
		}
		return false
	}

	scoped := map[string]bool{}
	now := nowText()
	tx, err := svc.db.Sql.Begin()
	if err != nil {
		return reconcileReport{}, err
	}
	defer tx.Rollback()

	rep := reconcileReport{}
	for _, ce := range computed {
		ev := ce.ev
		if !inScope(ce) {
			// 不在依赖集合：持久行存在则原样保留（不升修订）；理论上不应出现未知新事件。
			if _, ok := existing[ev.ID]; ok {
				rep.preserved++
			} else {
				// 防御：与依赖集合无关但首次出现（一般来自导入后初次重算），按修订 1 落库
				scoped[ev.ID] = true
				rep.recomputed++
			}
			continue
		}
		scoped[ev.ID] = true
		prev, had := existing[ev.ID]
		rev := 1
		if had {
			rev = prev.revision + 1
		}
		membersJSON, _ := json.Marshal(ev.DependsOn)
		_ = membersJSON
		if had {
			if _, err := tx.Exec(`UPDATE events SET revision=?,updated_at=? WHERE id=?`, rev, now, ev.ID); err != nil {
				return reconcileReport{}, err
			}
			if _, err := tx.Exec(`DELETE FROM event_evidence WHERE event_id=?`, ev.ID); err != nil {
				return reconcileReport{}, err
			}
		} else {
			var d interface{}
			if ev.Depth != nil {
				d = *ev.Depth
			}
			if _, err := tx.Exec(`INSERT INTO events(id,revision,interpretation,kind,group_id,section_id,depth,partner_group,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?)`, ev.ID, rev, ev.Interpretation, ev.Kind, ev.GroupID, ev.SectionID, d, nullStr(ev.PartnerGroup), now); err != nil {
				return reconcileReport{}, err
			}
		}
		for _, evd := range ev.Evidence {
			if _, err := tx.Exec(`INSERT INTO event_evidence(event_id,occurrence_id,interval_id,role,note) VALUES(?,?,?,?,?)`,
				ev.ID, nullStr(evd.OccurrenceID), nullStr(evd.IntervalID), evd.Role, evd.Note); err != nil {
				return reconcileReport{}, err
			}
		}
		rep.recomputed++
	}
	// 作用域内、但当前不再产生的事件（例如 split 后组消失）：删除
	for id := range existing {
		if scoped[id] {
			continue
		}
		// 判断该持久事件是否属于依赖集合（旧组 id）
		evInterp, evKind, g1, g2, ok := parseEventID(id)
		if !ok {
			continue
		}
		if affectedGroups[g1] || (g2 != "" && affectedGroups[g2]) {
			if _, err := tx.Exec(`DELETE FROM events WHERE id=?`, id); err != nil {
				return reconcileReport{}, err
			}
			rep.recomputed++
			scoped[id] = true
			_ = evInterp
			_ = evKind
		}
	}
	for id := range scoped {
		rep.scopedIDs = append(rep.scopedIDs, id)
	}
	sort.Strings(rep.scopedIDs)
	detail := ""
	if len(rep.scopedIDs) > 0 {
		detail = joinLimit(rep.scopedIDs, 8)
	}
	if _, err := tx.Exec(`INSERT INTO recompute_log(cause,scope,recomputed,preserved,detail) VALUES(?,?,?,?,?)`,
		cause, scopeText, rep.recomputed, rep.preserved, detail); err != nil {
		return reconcileReport{}, err
	}
	if err := tx.Commit(); err != nil {
		return reconcileReport{}, err
	}
	// 重算后刷新锦标成环标记
	if err := svc.refreshTieConflicts(); err != nil {
		return rep, err
	}
	return rep, nil
}

func parseEventID(id string) (interp, kind, g1, g2 string, ok bool) {
	const prefix = "EV|"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return "", "", "", "", false
	}
	body := id[len(prefix):]
	parts := strings.SplitN(body, "|", 5)
	if len(parts) != 5 {
		return "", "", "", "", false
	}
	interp, kind, g1, sec, partner := parts[0], parts[1], parts[2], parts[3], parts[4]
	// sec 形如 S1 或 gB@S2（COEX 时 partner 含 @）
	if kind == "COEX" {
		sp := strings.SplitN(sec, "@", 2)
		if len(sp) != 2 {
			return "", "", "", "", false
		}
		g2 = sp[0]
	} else if partner != "" {
		return "", "", "", "", false
	}
	_ = partner
	return interp, kind, g1, g2, true
}

func splitN(s, sep string, n int) []string {
	var out []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if len(out) < n-1 && i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			out = append(out, cur)
			cur = ""
			i += len(sep) - 1
			continue
		}
		cur += string(s[i])
	}
	out = append(out, cur)
	return out
}

func joinLimit(xs []string, n int) string {
	out := ""
	for i, x := range xs {
		if i >= n {
			out += fmt.Sprintf(" ...(+%d)", len(xs)-n)
			break
		}
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nowText() string {
	return utcNow()
}

var _ = store.SchemaVersion
