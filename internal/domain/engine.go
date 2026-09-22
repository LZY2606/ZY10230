package domain

import (
	"sort"
)

// BuildConcepts 由分类单元与等义关系的当前版本构造概念。
// 同一分类单元被多个版本引用时，最新版本（revision 最大）胜出；
// 未入组的分类单元各自成为独立概念。
func BuildConcepts(taxa []Taxon, groups []SynonymGroup) []Concept {
	nameOf := map[string]string{}
	for _, t := range taxa {
		nameOf[t.ID] = t.Name
	}
	owner := map[string]SynonymGroup{} // taxon id → 其当前组
	sorted := append([]SynonymGroup{}, groups...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Revision < sorted[j].Revision })
	used := map[int64]bool{}
	for _, g := range sorted {
		for _, m := range g.Members {
			owner[m] = g
		}
		used[g.ID] = true
	}
	groupSeen := map[int64]bool{}
	var concepts []Concept
	for _, t := range taxa {
		g, in := owner[t.ID]
		if !in {
			concepts = append(concepts, Concept{Name: t.Name, Members: []string{t.ID}, Revision: 0})
			continue
		}
		if groupSeen[g.ID] {
			continue
		}
		groupSeen[g.ID] = true
		members := append([]string{}, g.Members...)
		sort.Strings(members)
		var names []string
		for _, m := range members {
			if n, ok := nameOf[m]; ok {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		if len(names) > 0 {
			names[0] = "≡" + names[0]
		}
		concepts = append(concepts, Concept{
			Name:     joinNames(names),
			Members:  members,
			Revision: g.Revision,
		})
	}
	sort.Slice(concepts, func(i, j int) bool { return concepts.Name < concepts[j].Name })
	return concepts
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "/"
		}
		out += n
	}
	return out
}

// ComputeAll 计算全部概念在全部剖面上的 FAD/LAD 事件，并收集负证据深度。
func ComputeAll(concepts []Concept, sections []Section, intervals []SampleInterval, occs []Occurrence, generation int64) ([]Event, map[string]map[string][]float64) {
	neg := map[string]map[string][]float64{}
	var events []Event
	for _, c := range concepts {
		neg[c.Name] = map[string][]float64{}
		for _, sec := range sections {
			var related []Occurrence
			for _, o := range occs {
				if o.Section == sec.ID && contains(c.Members, o.Taxon) {
					related = append(related, o)
				}
			}
			r := ComputeConceptEvents(c, sec.ID, related, intervalsFor(intervals, sec.ID), generation)
			events = append(events, r.Events...)
			if len(r.NegativeDepths) > 0 {
				neg[c.Name][sec.ID] = r.NegativeDepths
			}
		}
	}
	return events, neg
}

// CooccEvents 由分带结果生成共存事件（同一剖面、同一带内延程重叠的概念对）。
func CooccEvents(zones []Zone, generation int64) []Event {
	var out []Event
	for _, z := range zones {
		for i := 0; i < len(z.Taxa); i++ {
			for j := i + 1; j < len(z.Taxa); j++ {
				if z.Taxa[i] == z.Taxa[j] {
					continue
				}
				out = append(out, Event{
					ID:         z.Taxa[i] + "+" + z.Taxa[j] + "|" + z.Section + "|z" + itoa(z.Index),
					Kind:       "coocc",
					Section:    z.Section,
					Concept:    z.Taxa[i] + "+" + z.Taxa[j],
					Depth:      (z.Top + z.Base) / 2,
					Generation: generation,
					Evidence:   "同带共存（第" + itoa(z.Index) + "带）",
				})
			}
		}
	}
	return out
}

func intervalsFor(all []SampleInterval, section string) []SampleInterval {
	var out []SampleInterval
	for _, iv := range all {
		if iv.Section == section {
			out = append(out, iv)
		}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// AffectedConceptNames 计算等义关系变更后需要重算的概念名集合。
// changedMembers 为被新版本覆盖的分类单元；oldConcepts/newConcepts 为重算前后的概念集。
func AffectedConceptNames(oldConcepts, newConcepts []Concept, changedTaxa []string) map[string]bool {
	hit := map[string]bool{}
	for _, id := range changedTaxa {
		for _, c := range oldConcepts {
			if contains(c.Members, id) {
				hit[c.Name] = true
			}
		}
		for _, c := range newConcepts {
			if contains(c.Members, id) {
				hit[c.Name] = true
			}
		}
	}
	return hit
}
