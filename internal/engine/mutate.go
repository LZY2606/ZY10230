package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func utcNow() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }

// MergeTaxa 建立一个版本化等义组（合并）。
func (svc *Service) MergeTaxa(groupID, displayName string, members []string, note string) error {
	if groupID == "" {
		groupID = "G-" + randHex(3)
	}
	members = uniqueSorted(nonEmpty(members))
	if len(members) < 2 {
		return errors.New("合并等义组至少需要两个分类单元")
	}
	taxa := map[string]bool{}
	for _, m := range members {
		taxa[m] = true
	}
	before, err := svc.eng.loadGroups()
	if err != nil {
		return err
	}
	// 成员不能已在其他活跃组中
	for _, g := range before {
		if g.ID == groupID {
			continue
		}
		for _, m := range g.Members {
			if taxa[m] {
				return fmt.Errorf("分类单元 %s 已属于等义组 %s，请先 split", m, g.ID)
			}
		}
	}
	tx, err := svc.db.Sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO syn_groups(id,display_name) VALUES(?,?)
		ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name`, groupID, displayName); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM syn_members WHERE group_id=?`, groupID); err != nil {
		return err
	}
	for _, m := range members {
		if _, err := tx.Exec(`INSERT INTO syn_members(group_id,taxon_id) VALUES(?,?)`, groupID, m); err != nil {
			return err
		}
	}
	mj, _ := json.Marshal(members)
	if _, err := tx.Exec(`INSERT INTO syn_versions(kind,group_id,display_name,members,note) VALUES('merge',?,?,?,?)`,
		groupID, displayName, string(mj), note); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	after, err := svc.eng.loadGroups()
	if err != nil {
		return err
	}
	aff := affectedGroups(before, after, taxa)
	_, err = svc.reconcileEvents("synonymy:merge",
		fmt.Sprintf("group=%s members=%s", groupID, strings.Join(members, ",")),
		taxa, map[string]bool{}, aff)
	return err
}

// SplitTaxa 解除等义组（拆分），记录版本；组定义保留但成员清空（审计留痕）。
func (svc *Service) SplitTaxa(groupID, note string) error {
	before, err := svc.eng.loadGroups()
	if err != nil {
		return err
	}
	var target *Group
	for i := range before {
		if before[i].ID == groupID {
			target = &before[i]
		}
	}
	if target == nil {
		return fmt.Errorf("等义组不存在: %s", groupID)
	}
	taxa := map[string]bool{}
	for _, m := range target.Members {
		taxa[m] = true
	}
	tx, err := svc.db.Sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	mj, _ := json.Marshal(target.Members)
	if _, err := tx.Exec(`INSERT INTO syn_versions(kind,group_id,display_name,members,note) VALUES('split',?,?,?,?)`,
		groupID, target.DisplayName, string(mj), note); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM syn_members WHERE group_id=?`, groupID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	after, err := svc.eng.loadGroups()
	if err != nil {
		return err
	}
	aff := affectedGroups(before, after, taxa)
	_, err = svc.reconcileEvents("synonymy:split",
		fmt.Sprintf("group=%s released=%s", groupID, strings.Join(target.Members, ",")),
		taxa, map[string]bool{}, aff)
	return err
}

// SetRework 把某个异常产出标记/取消重工候选；只重算依赖该产出的事件。
func (svc *Service) SetRework(occurrenceID string, marked bool, note string) error {
	var sec, tax string
	err := svc.db.Sql.QueryRow(`SELECT section_id,taxon_id FROM occurrences WHERE id=?`, occurrenceID).
		Scan(&sec, &tax)
	if err != nil {
		return fmt.Errorf("产出不存在: %s", occurrenceID)
	}
	if marked {
		if _, err := svc.db.Sql.Exec(`INSERT INTO rework_candidates(occurrence_id,marked,updated_at,note) VALUES(?,1,?,?)
			ON CONFLICT(occurrence_id) DO UPDATE SET marked=1,updated_at=excluded.updated_at,note=excluded.note`,
			occurrenceID, utcNow(), note); err != nil {
			return err
		}
	} else {
		if _, err := svc.db.Sql.Exec(`UPDATE rework_candidates SET marked=0,updated_at=?,note=? WHERE occurrence_id=?`,
			utcNow(), note, occurrenceID); err != nil {
			return err
		}
	}
	affOcc := map[string]bool{occurrenceID: true}
	_, err = svc.reconcileEvents("rework:"+boolText(marked),
		fmt.Sprintf("occurrence=%s", occurrenceID),
		map[string]bool{}, affOcc, map[string]bool{})
	return err
}

type TieInput struct {
	AGroup, ASection, AKind string
	BGroup, BSection, BKind string
	Note                    string
}

// AddTie 锁定一对待时层位锦标。成环时保留锦标并返回具体冲突链（不自动删除）。
func (svc *Service) AddTie(in TieInput) (string, []ChainStep, []string, error) {
	if err := validateHorizonRef(in.AKind); err != nil {
		return "", nil, nil, err
	}
	if err := validateHorizonRef(in.BKind); err != nil {
		return "", nil, nil, err
	}
	id := "TIE-" + randHex(3)
	if _, err := svc.db.Sql.Exec(`INSERT INTO ties(id,a_group,a_section,a_kind,b_group,b_section,b_kind,note)
		VALUES(?,?,?,?,?,?,?,?)`, id, in.AGroup, in.ASection, in.AKind, in.BGroup, in.BSection, in.BKind, in.Note); err != nil {
		return "", nil, nil, err
	}
	if err := svc.refreshTieConflicts(); err != nil {
		return id, nil, nil, err
	}
	st, err := svc.eng.load()
	if err != nil {
		return id, nil, nil, err
	}
	computed := computeEvents(st)
	horizons := computeHorizons(computed)
	g, _ := buildTieGraph(horizons, st.ties, "raw", nil)
	cycle := findCycle(g)
	if cycle == nil {
		g2, _ := buildTieGraph(horizons, st.ties, "in_situ", nil)
		cycle = findCycle(g2)
	}
	if cycle == nil {
		return id, nil, nil, nil
	}
	return id, chainSteps(cycle), tieIDsInChain(cycle), nil
}

func (svc *Service) DeactivateTie(id string) error {
	res, err := svc.db.Sql.Exec(`UPDATE ties SET active=0 WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("锦标不存在: %s", id)
	}
	return svc.refreshTieConflicts()
}

// refreshTieConflicts 重新检测成环：优先 raw 解释，其次 in_situ；
// 参与冲突链的锦标标记 conflicted=1，其余清零。绝不删除锦标。
func (svc *Service) refreshTieConflicts() error {
	st, err := svc.eng.load()
	if err != nil {
		return err
	}
	computed := computeEvents(st)
	horizons := computeHorizons(computed)
	var ids []string
	for _, interp := range []string{"raw", "in_situ"} {
		g, _ := buildTieGraph(horizons, st.ties, interp, nil)
		if cycle := findCycle(g); cycle != nil {
			ids = tieIDsInChain(cycle)
			break
		}
	}
	conf := map[string]bool{}
	for _, id := range ids {
		conf[id] = true
	}
	for _, t := range st.ties {
		v := 0
		if conf[t.ID] {
			v = 1
		}
		if _, err := svc.db.Sql.Exec(`UPDATE ties SET conflicted=? WHERE id=?`, v, t.ID); err != nil {
			return err
		}
	}
	return nil
}

func validateHorizonRef(k string) error {
	if k != "FAD" && k != "LAD" {
		return fmt.Errorf("层位事件必须是 FAD 或 LAD，收到 %q", k)
	}
	return nil
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range in {
		x = strings.TrimSpace(x)
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}
func nonEmpty(in []string) []string { return uniqueSorted(in) }

func boolText(b bool) string {
	if b {
		return "mark"
	}
	return "unmark"
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
