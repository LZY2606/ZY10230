package domain

import (
	"fmt"
	"sort"
)

// Scheme 为一个分带方案：按某一解释集合切分各剖面，并用锦标对齐。
type Scheme struct {
	Name       string       `json:"name"`
	Interp     string       `json:"interp"` // 采用的解释集合
	Zones      []Zone       `json:"zones"`
	Violations []Violation  `json:"violations"`
}

// Zone 为某剖面内相邻事件界定的带。
type Zone struct {
	Section  string   `json:"section"`
	Index    int      `json:"index"`
	Top      float64  `json:"top"`
	Base     float64  `json:"base"`
	Taxa     []string `json:"taxa"` // 延程覆盖本带的概念
}

// Violation 为一条软约束违反，附依赖证据。
type Violation struct {
	Rule     string `json:"rule"`
	Section  string `json:"section,omitempty"`
	Detail   string `json:"detail"`
	Evidence string `json:"evidence"`
}

// BuildSchemes 由事件集合构建多个分带方案。
// 无倒置概念在所有解释下事件一致；倒置概念按解释集合分别成案。
func BuildSchemes(events []Event, sections []Section, locks []Lock, horizons []Horizon, negEvidence map[string]map[string][]float64) []Scheme {
	interps := []string{InterpExclude, InterpInSitu, InterpInverted}
	names := []string{"方案A·剔除重工候选", "方案B·全部原位", "方案C·仅倒置点重工"}
	var schemes []Scheme
	for i, interp := range interps {
		sel := selectEvents(events, interp)
		schemes = append(schemes, buildScheme(names[i], interp, sel, sections, locks, horizons, negEvidence))
	}
	return schemes
}

// selectEvents 选取某解释集合下的事件：优先所选解释，缺失时回退到无争议事件。
func selectEvents(events []Event, interp string) []Event {
	type key struct{ concept, section, kind string }
	byInterp := map[key]Event{}
	fallback := map[key]Event{}
	for _, e := range events {
		k := key{e.Concept, e.Section, e.Kind}
		if !e.Contested {
			fallback[k] = e
		}
		if e.Interpretation == interp {
			byInterp[k] = e
		}
	}
	var out []Event
	for k, e := range byInterp {
		out = append(out, e)
		_ = k
	}
	for k, e := range fallback {
		if _, ok := byInterp[k]; !ok {
			out = append(out, e)
		}
	}
	return out
}

func buildScheme(name, interp string, events []Event, sections []Section, locks []Lock, horizons []Horizon, neg map[string]map[string][]float64) Scheme {
	sc := Scheme{Name: name, Interp: interp}
	// 按剖面聚合事件深度。
	bySection := map[string][]Event{}
	for _, e := range events {
		if e.Kind == "FAD" || e.Kind == "LAD" {
			bySection[e.Section] = append(bySection[e.Section], e)
		}
	}
	for _, sec := range sections {
		evs := bySection[sec.ID]
		var cuts []float64
		for _, e := range evs {
			cuts = append(cuts, e.Depth)
		}
		sort.Float64s(cuts)
		cuts = uniqFloats(cuts)
		bounds := append([]float64{sec.Top}, cuts...)
		bounds = append(bounds, sec.Base)
		for i := 0; i+1 < len(bounds); i++ {
			z := Zone{Section: sec.ID, Index: i + 1, Top: bounds[i], Base: bounds[i+1]}
			mid := (z.Top + z.Base) / 2
			for _, e := range evs {
				// 概念延程覆盖：mid 落在该概念 [LAD, FAD] 深度区间内。
				if rangeCovers(evs, e.Concept, mid) {
					if !contains(z.Taxa, e.Concept) {
						z.Taxa = append(z.Taxa, e.Concept)
					}
				}
			}
			sort.Strings(z.Taxa)
			sc.Zones = append(sc.Zones, z)
		}
	}
	sc.Violations = checkSoftConstraints(sc, events, locks, horizons, neg)
	return sc
}

// rangeCovers 判断深度 d 是否在某概念于该剖面的 [LAD, FAD] 延程内。
func rangeCovers(evs []Event, concept string, d float64) bool {
	var fad, lad *float64
	for _, e := range evs {
		if e.Concept != concept {
			continue
		}
		if e.Kind == "FAD" {
			v := e.Depth
			fad = &v
		}
		if e.Kind == "LAD" {
			v := e.Depth
			lad = &v
		}
	}
	if fad == nil || lad == nil {
		return false
	}
	return d >= *lad && d <= *fad
}

// checkSoftConstraints 检查软约束并返回违反项（不阻断方案生成）。
func checkSoftConstraints(sc Scheme, events []Event, locks []Lock, horizons []Horizon, neg map[string]map[string][]float64) []Violation {
	var out []Violation
	// 软约束1：同一概念同一剖面 FAD 不得浅于 LAD。
	type ck struct{ concept, section string }
	fad := map[ck]float64{}
	lad := map[ck]float64{}
	for _, e := range events {
		k := ck{e.Concept, e.Section}
		if e.Kind == "FAD" {
			fad[k] = e.Depth
		}
		if e.Kind == "LAD" {
			lad[k] = e.Depth
		}
	}
	for k, f := range fad {
		if l, ok := lad[k]; ok && f < l {
			out = append(out, Violation{
				Rule: "首现不浅于末现", Section: k.section,
				Detail:   fmt.Sprintf("%s 首现 %.1fm 浅于末现 %.1fm", k.concept, f, l),
				Evidence: fmt.Sprintf("事件 %s/FAD 与 %s/LAD（解释：%s）", k.concept, k.concept, sc.Interp),
			})
		}
	}
	// 软约束2：末现之上的充分采样“未见”构成负证据，带界不应把该概念延入其上覆带。
	for _, z := range sc.Zones {
		for _, concept := range z.Taxa {
			for _, nd := range neg[concept][z.Section] {
				if nd < z.Top {
					out = append(out, Violation{
						Rule: "负证据约束", Section: z.Section,
						Detail:   fmt.Sprintf("%s 在 %.1fm 有充分采样未见，却仍延入第%d带", concept, nd, z.Index),
						Evidence: fmt.Sprintf("负证据深度 %.1fm（采样充分）", nd),
					})
				}
			}
		}
	}
	// 软约束3：锦标锁定的层位应落入对齐的带（同序带）。
	hzone := map[node]int{}
	for _, z := range sc.Zones {
		for _, h := range horizons {
			if h.Section == z.Section && h.Depth >= z.Top && h.Depth <= z.Base {
				hzone[node{h.Section, h.Name}] = z.Index
			}
		}
	}
	for _, l := range locks {
		za, oka := hzone[node{l.ASection, l.AHorizon}]
		zb, okb := hzone[node{l.BSection, l.BHorizon}]
		if oka && okb && za != zb {
			out = append(out, Violation{
				Rule: "锦标对齐",
				Detail: fmt.Sprintf("锦标#%d 锁定 %s.%s≡%s.%s，但分入第%d带与第%d带",
					l.ID, l.ASection, l.AHorizon, l.BSection, l.BHorizon, za, zb),
				Evidence: fmt.Sprintf("锦标#%d", l.ID),
			})
		}
	}
	return out
}

func uniqFloats(in []float64) []float64 {
	var out []float64
	for i, v := range in {
		if i == 0 || v != in[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
