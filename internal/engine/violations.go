package engine

import (
	"fmt"
	"sort"
)

// sectionViolations 计算单剖面分带方案违反的软约束。
// 口径原则：充分采样下的“未见”才是负证据；采样不足与完全未采样均不约束末现。
func sectionViolations(s *snapshot, sectionID, interp string, events []computedEvent, horizons []Horizon) []Violation {
	var out []Violation

	// 1) range_gap：末现点之下仍有充分采样层段，但未登记“未见”作为负证据。
	depthOf := map[string]map[string]float64{} // group -> kind -> depth
	for _, h := range horizons {
		if h.SectionID != sectionID {
			continue
		}
		var d *float64
		if interp == "raw" {
			d = h.DepthRaw
		} else {
			d = h.DepthIn
		}
		if d == nil {
			continue
		}
		if depthOf[h.GroupID] == nil {
			depthOf[h.GroupID] = map[string]float64{}
		}
		depthOf[h.GroupID][h.Kind] = *d
	}
	memberMap := map[string]map[string]bool{}
	for _, g := range effectiveGroups(s) {
		memberMap[g.ID] = map[string]bool{}
		for _, m := range g.Members {
			memberMap[g.ID][m] = true
		}
	}
	groupIDs := make([]string, 0, len(depthOf))
	for g := range depthOf {
		groupIDs = append(groupIDs, g)
	}
	sort.Strings(groupIDs)
	for _, g := range groupIDs {
		lad, hasLAD := depthOf[g]["LAD"]
		if !hasLAD {
			continue
		}
		hasNegative := false
		for _, o := range s.occs {
			if o.SectionID != sectionID || !memberMap[g][o.TaxonID] || o.Status != "absent" || o.IntervalID == "" {
				continue
			}
			iv := s.ivlByID[o.IntervalID]
			if iv.Sufficient && iv.TopDepth >= lad-1e-9 {
				hasNegative = true
			}
		}
		if !hasNegative {
			// 末现之下是否至少存在充分采样层段？
			sampledBelow := false
			for _, iv := range s.intervals {
				if iv.SectionID == sectionID && iv.Sufficient && iv.TopDepth >= lad-1e-9 {
					sampledBelow = true
				}
			}
			if sampledBelow {
				out = append(out, Violation{
					Code: "range_gap", Severity: "soft",
					Message: fmt.Sprintf("剖面 %s 组 %s 末现(%s)之下有充分采样层段，但缺少充分采样“未见”记录，末现缺负证据支撑",
						s.secByID[sectionID].Name, groupLabel(s, g), f2s(lad)),
					Refs: []string{"group:" + g, "section:" + sectionID},
				})
			}
		}
	}

	// 2) unconstrained_fad / unconstrained_lad：
	// 首现之上/末现之下完全没有采样（既无产出也无充分未见），边界开放。
	for _, g := range groupIDs {
		fad, hasFAD := depthOf[g]["FAD"]
		lad, hasLAD := depthOf[g]["LAD"]
		if hasFAD && !anyCoverage(s, sectionID, memberMap[g], func(iv Interval) bool { return iv.TopDepth <= fad+1e-9 }) {
			out = append(out, Violation{
				Code: "unconstrained_fad", Severity: "soft",
				Message: fmt.Sprintf("剖面 %s 组 %s 首现(%s)之上无采样覆盖，顶界开放，不能据最值断言真实首现",
					s.secByID[sectionID].Name, groupLabel(s, g), f2s(fad)),
				Refs: []string{"group:" + g, "section:" + sectionID},
			})
		}
		if hasLAD && !anyCoverage(s, sectionID, memberMap[g], func(iv Interval) bool { return iv.TopDepth >= lad-1e-9 }) {
			out = append(out, Violation{
				Code: "unconstrained_lad", Severity: "soft",
				Message: fmt.Sprintf("剖面 %s 组 %s 末现(%s)之下无采样覆盖，底界开放；未采样不等于未见",
					s.secByID[sectionID].Name, groupLabel(s, g), f2s(lad)),
				Refs: []string{"group:" + g, "section:" + sectionID},
			})
		}
	}

	// 3) event_order_inversion：重工点导致两事件的先后在 raw 与 in_situ 之间倒置。
	if interp == "raw" {
		inDepth := map[string]map[string]float64{}
		for _, h := range horizons {
			if h.SectionID != sectionID || h.DepthIn == nil {
				continue
			}
			if inDepth[h.GroupID] == nil {
				inDepth[h.GroupID] = map[string]float64{}
			}
			inDepth[h.GroupID][h.Kind] = *h.DepthIn
		}
		var gs []string
		for g := range depthOf {
			if _, ok := inDepth[g]; ok {
				gs = append(gs, g)
			}
		}
		sort.Strings(gs)
		for i := 0; i < len(gs); i++ {
			for j := i + 1; j < len(gs); j++ {
				for _, kind := range []string{"FAD", "LAD"} {
					di, okI := depthOf[gs[i]][kind], false
					dj, okJ := depthOf[gs[j]][kind], false
					_, okI = depthOf[gs[i]][kind]
					_, okJ = depthOf[gs[j]][kind]
					ei, okEI := inDepth[gs[i]][kind]
					ej, okEJ := inDepth[gs[j]][kind]
					if !okI || !okJ || !okEI || !okEJ {
						continue
					}
					if (di-dj)*(ei-ej) < 0 {
						out = append(out, Violation{
							Code: "event_order_inversion", Severity: "soft",
							Message: fmt.Sprintf("剖面 %s 的 %s 事件顺序在两种解释间倒置：%s(%s) vs %s(%s)；保留多解释，不按深度最值硬定",
								s.secByID[sectionID].Name, kind,
								groupLabel(s, gs[i]), f2s(di), groupLabel(s, gs[j]), f2s(dj)),
							Refs: []string{"group:" + gs[i], "group:" + gs[j], "section:" + sectionID},
						})
					}
				}
			}
		}
	}

	// 4) rework_ambiguity：原始口径使用了被标重工候选的点来定位首/末现。
	for _, ce := range events {
		ev := ce.ev
		if ev.SectionID != sectionID || ev.Interpretation != interp || ev.Kind == "COEX" || ev.Depth == nil {
			continue
		}
		for _, o := range s.occs {
			if o.SectionID != sectionID || !memberMap[ev.GroupID][o.TaxonID] || !o.Rework {
				continue
			}
			if (ev.Kind == "FAD" && o.Depth <= *ev.Depth+1e-9) || (ev.Kind == "LAD" && o.Depth >= *ev.Depth-1e-9) {
				out = append(out, Violation{
					Code: "rework_ambiguity", Severity: "soft",
					Message: fmt.Sprintf("剖面 %s 组 %s 的 %s(%s) 依赖重工候选点 %s；raw/in_situ 两种解释均保留",
						s.secByID[sectionID].Name, groupLabel(s, ev.GroupID), ev.Kind, f2s(*ev.Depth), o.ID),
					Refs: []string{"occurrence:" + o.ID, "group:" + ev.GroupID},
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Message < out[j].Message
	})
	return dedupViolations(out)
}

func anyCoverage(s *snapshot, sectionID string, members map[string]bool, match func(Interval) bool) bool {
	// 采样覆盖：充分采样层段，或该组分类单元在层段中有产出/未见记录。
	for _, iv := range s.intervals {
		if iv.SectionID == sectionID && iv.Sufficient && match(iv) {
			return true
		}
	}
	for _, o := range s.occs {
		if o.SectionID != sectionID || !members[o.TaxonID] || o.IntervalID == "" {
			continue
		}
		iv := s.ivlByID[o.IntervalID]
		if iv.Sufficient && match(iv) {
			return true
		}
	}
	return false
}

func groupLabel(s *snapshot, id string) string {
	if len(id) > 0 && id[0] == '~' {
		if tx, ok := s.taxonByID[id[1:]]; ok {
			return tx.Name + "(" + id[1:] + ")"
		}
	}
	for _, g := range s.groups {
		if g.ID == id {
			return g.DisplayName + "(" + id + ")"
		}
	}
	return id
}

func dedupViolations(vs []Violation) []Violation {
	seen := map[string]bool{}
	var out []Violation
	for _, v := range vs {
		key := v.Code + "|" + v.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}
