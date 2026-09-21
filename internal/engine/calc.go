package engine

import (
	"sort"
)

// effectiveGroups 返回显式等义组，并为未入组的分类单元补充稳定的单体组（id 以 "~" 前缀）。
func effectiveGroups(s *snapshot) []Group {
	indexed := map[string]bool{}
	var out []Group
	for _, g := range s.groups {
		out = append(out, g)
		for _, m := range g.Members {
			indexed[m] = true
		}
	}
	for _, tx := range s.taxa {
		if !indexed[tx.ID] {
			out = append(out, Group{
				ID:          "~" + tx.ID,
				DisplayName: tx.Name,
				Members:     []string{tx.ID},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func memberOccs(s *snapshot, group Group, sectionID string) []Occurrence {
	member := map[string]bool{}
	for _, m := range group.Members {
		member[m] = true
	}
	var out []Occurrence
	for _, o := range s.occs {
		if o.SectionID == sectionID && member[o.TaxonID] && o.Status == "present" {
			out = append(out, o)
		}
	}
	return out
}

type computedEvent struct {
	ev       Event
	memberOC []string
}

type rangeInfo struct {
	fad, lad *float64
	fadOcc   string
	ladOcc   string
	occs     []string
}

func groupRange(s *snapshot, g Group, sectionID, interp string) rangeInfo {
	occs := memberOccs(s, g, sectionID)
	var ri rangeInfo
	for _, o := range occs {
		if interp == "in_situ" && o.Rework {
			continue
		}
		d := o.Depth
		ri.occs = append(ri.occs, o.ID)
		if ri.fad == nil || d < *ri.fad {
			ri.fad = &d
			ri.fadOcc = o.ID
		}
		if ri.lad == nil || d > *ri.lad {
			ri.lad = &d
			ri.ladOcc = o.ID
		}
	}
	return ri
}

func eventKey(interp, kind, groupID, sectionID, partner string) string {
	return interp + "|" + kind + "|" + groupID + "|" + sectionID + "|" + partner
}

// computeEvents 按两种解释（raw 全部产出 / in_situ 剔除重工候选）计算时序事件。
func computeEvents(s *snapshot) []computedEvent {
	groups := effectiveGroups(s)
	var out []computedEvent
	for _, interp := range []string{"raw", "in_situ"} {
		type gr struct {
			g Group
			r rangeInfo
		}
		ranges := map[string]map[string]rangeInfo{}
		for _, g := range groups {
			ranges[g.ID] = map[string]rangeInfo{}
			for _, sec := range s.sections {
				ri := groupRange(s, g, sec.ID, interp)
				ranges[g.ID][sec.ID] = ri
				if ri.fad == nil {
					continue
				}
				// FAD
				out = append(out, computedEvent{
					ev: Event{
						ID:             "EV|" + eventKey(interp, "FAD", g.ID, sec.ID, ""),
						Interpretation: interp, Kind: "FAD",
						GroupID: g.ID, SectionID: sec.ID, Depth: ri.fad,
						Evidence: []Evidence{{
							OccurrenceID: ri.fadOcc, Role: "fad_point",
							Note: "最浅产出点（深度最小值）决定首现",
						}},
						DependsOn: append([]string{}, ri.occs...),
					},
					memberOC: append([]string{}, ri.occs...),
				})
				// LAD：最深产出点；负证据只能来自充分采样层段中的“未见”。
				ev := Evidence{OccurrenceID: ri.ladOcc, Role: "lad_point",
					Note: "最深产出点（深度最大值）决定末现"}
				ladEv := Event{
					ID:             "EV|" + eventKey(interp, "LAD", g.ID, sec.ID, ""),
					Interpretation: interp, Kind: "LAD",
					GroupID: g.ID, SectionID: sec.ID, Depth: ri.lad,
					Evidence:  []Evidence{ev},
					DependsOn: append([]string{}, ri.occs...),
				}
				if neg := deepestNegativeEvidence(s, g, sec.ID, *ri.lad); neg != nil {
					ladEv.Evidence = append(ladEv.Evidence, *neg)
				}
				out = append(out, computedEvent{ev: ladEv, memberOC: append([]string{}, ri.occs...)})
			}
		}
		// COEX 共存事件：两组产出深度区间重叠。
		for _, sec := range s.sections {
			for i := 0; i < len(groups); i++ {
				for j := i + 1; j < len(groups); j++ {
					a, b := ranges[groups[i].ID][sec.ID], ranges[groups[j].ID][sec.ID]
					if a.fad == nil || b.fad == nil {
						continue
					}
					overlapTop := maxF(*a.fad, *b.fad)
					overlapBase := minF(*a.lad, *b.lad)
					if overlapTop > overlapBase {
						continue
					}
					deps := append(append([]string{}, a.occs...), b.occs...)
					mid := overlapTop
					out = append(out, computedEvent{
						ev: Event{
							ID:             "EV|" + eventKey(interp, "COEX", groups[i].ID, sec.ID, groups[j].ID),
							Interpretation: interp, Kind: "COEX",
							GroupID: groups[i].ID, PartnerGroup: groups[j].ID,
							SectionID: sec.ID, Depth: &mid,
							Evidence: []Evidence{
								{OccurrenceID: a.fadOcc, Role: "coex_a", Note: "A 组首现点"},
								{OccurrenceID: b.fadOcc, Role: "coex_b", Note: "B 组首现点"},
								{Role: "overlap", Note: "重叠区间[" + f2s(overlapTop) + "," + f2s(overlapBase) + "]"},
							},
							DependsOn: deps,
						},
						memberOC: deps,
					})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ev.ID < out[j].ev.ID })
	return out
}

// deepestNegativeEvidence 在 LAD 之下寻找充分采样层段中的“未见”记录；
// 采样不足的“未见”与完全未采样都不作为负证据。
func deepestNegativeEvidence(s *snapshot, g Group, sectionID string, ladDepth float64) *Evidence {
	member := map[string]bool{}
	for _, m := range g.Members {
		member[m] = true
	}
	var best *Occurrence
	for i := range s.occs {
		o := &s.occs[i]
		if o.SectionID != sectionID || o.TaxonID == "" || !member[o.TaxonID] || o.Status != "absent" || o.IntervalID == "" {
			continue
		}
		iv := s.ivlByID[o.IntervalID]
		if !iv.Sufficient {
			continue
		}
		if iv.TopDepth >= ladDepth-1e-9 {
			if best == nil || o.Depth < best.Depth {
				best = o
			}
		}
	}
	if best == nil {
		return nil
	}
	return &Evidence{OccurrenceID: best.ID, IntervalID: best.IntervalID, Role: "lad_negative",
		Note: "其下充分采样层段未见，构成末现负证据；采样不足/未采样不计"}
}

// computeHorizons 汇总两个解释下的首末现层位，供锦标解析与成环判定。
func computeHorizons(events []computedEvent) []Horizon {
	type key struct{ g, sec, kind string }
	hm := map[key]*Horizon{}
	for _, ce := range events {
		ev := ce.ev
		if ev.Kind != "FAD" && ev.Kind != "LAD" {
			continue
		}
		k := key{ev.GroupID, ev.SectionID, ev.Kind}
		h := hm[k]
		if h == nil {
			h = &Horizon{GroupID: ev.GroupID, SectionID: ev.SectionID, Kind: ev.Kind}
			hm[k] = h
		}
		if ev.Interpretation == "raw" {
			h.DepthRaw = ev.Depth
		} else {
			h.DepthIn = ev.Depth
		}
	}
	var out []Horizon
	for _, h := range hm {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GroupID != out[j].GroupID {
			return out[i].GroupID < out[j].GroupID
		}
		if out[i].SectionID != out[j].SectionID {
			return out[i].SectionID < out[j].SectionID
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func f2s(v float64) string {
	return strconvFormat(v)
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
