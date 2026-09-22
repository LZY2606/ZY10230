// Package domain 实现“化石带议庭”的核心口径：
// 出现/未见/未采样三分、重工候选倒置的多解释、时序事件与分带、锦标冲突链。
package domain

import (
	"fmt"
	"sort"
)

// 产出状态（三分口径，互斥）：
//   StatusPresent   正常产出（或标记为重工候选的产出，仍是“有记录”）
//   StatusAbsent    未见：采样充分度可用的层段内检索而未获 —— 负证据
//   StatusUnsampled 未采样：没有可用采样覆盖 —— 不构成任何证据
const (
	StatusPresent   = "present"
	StatusAbsent    = "absent"
	StatusUnsampled = "unsampled"
)

// Section 为一条剖面，深度向下增大（米）。
type Section struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Top  float64 `json:"top"`  // 最浅深度
	Base float64 `json:"base"` // 最深深度
}

// SampleInterval 为一次采样层段；Adequate=false 表示采样充分度不可用，
// 其内“未见”不得作为负证据。
type SampleInterval struct {
	ID       int64   `json:"id"`
	Section  string  `json:"section"`
	Top      float64 `json:"top"`
	Base     float64 `json:"base"`
	Adequate bool    `json:"adequate"`
	Note     string  `json:"note"`
}

// Covers 判断深度 d 是否落在层段内。
func (s SampleInterval) Covers(d float64) bool {
	return d >= s.Top && d <= s.Base
}

// Taxon 为分类单元名称（原始鉴定名，可参与等义关系）。
type Taxon struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SynonymGroup 为一组等义关系的一个版本。任何修改产生新 revision，
// 旧版本保留，事件记录其计算时使用的 revision。
type SynonymGroup struct {
	ID       int64    `json:"id"`
	Revision int      `json:"revision"`
	Members  []string `json:"members"` // taxon id 列表
	Note     string   `json:"note"`
}

// Occurrence 为一条产出记录：某剖面某深度对某分类单元的观察结果。
// Status 为 present 或 absent；Reworked 标记该产出为重工候选。
// “未采样”不落库，由采样覆盖推导。
type Occurrence struct {
	ID       int64   `json:"id"`
	Section  string  `json:"section"`
	Taxon    string  `json:"taxon"`
	Depth    float64 `json:"depth"`
	Status   string  `json:"status"`
	Reworked bool    `json:"reworked"`
	Note     string  `json:"note"`
}

// Horizon 为剖面内的命名层位（锦标对齐的锚点）。
type Horizon struct {
	Section string  `json:"section"`
	Name    string  `json:"name"`
	Depth   float64 `json:"depth"`
}

// Lock 为一对层位锦标：断言两剖面的两个层位同时。
type Lock struct {
	ID       int64  `json:"id"`
	ASection string `json:"a_section"`
	AHorizon string `json:"a_horizon"`
	BSection string `json:"b_section"`
	BHorizon string `json:"b_horizon"`
	Note     string `json:"note"`
}

// ChainStep 为冲突链上的一步：要么是剖面内层序（below），要么是锦标（lock）。
type ChainStep struct {
	Kind      string `json:"kind"` // "order" | "lock" | "new_lock"
	FromRef   string `json:"from_ref"`
	ToRef     string `json:"to_ref"`
	LockID    int64  `json:"lock_id,omitempty"`
	Detail    string `json:"detail"`
}

func horizonRef(section, name string) string { return section + "." + name }

// ---- 事件 ----

// 事件解释标签：同一分类单元被重工点倒置时，多种解释并存。
const (
	InterpInSitu   = "原位解释（暂忽略重工标记）"
	InterpExclude  = "剔除全部重工候选"
	InterpInverted = "仅倒置点为重工"
)

// Event 为一条时序事件：首现(FAD)、末现(LAD)或共存(coocc)。
type Event struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"` // FAD | LAD | coocc
	Section        string  `json:"section"`
	Concept        string  `json:"concept"` // 等义概念代表名
	Depth          float64 `json:"depth"`
	Interpretation string  `json:"interpretation"`
	Contested      bool    `json:"contested"` // 多解释并存
	Revision       int     `json:"revision"`  // 计算时使用的等义版本
	Generation     int64   `json:"generation"`
	Evidence       string  `json:"evidence"` // 依赖证据描述
}

// conceptKey 返回事件归属键（概念+剖面+种类），用于依赖重算比对。
func (e Event) conceptKey() string { return e.Concept + "|" + e.Section + "|" + e.Kind }

// ---- 事件计算 ----

// Concept 为一个等义概念：代表名 + 成员分类单元。
type Concept struct {
	Name     string
	Members  []string
	Revision int
}

// EvidenceNote 记录一条记录的处置，供“依赖证据”展示。
type EvidenceNote struct {
	OccurrenceID int64  `json:"occurrence_id"`
	Decision     string `json:"decision"`
	Reason       string `json:"reason"`
}

// ConceptSectionResult 为一个概念在一条剖面内的事件计算结果。
type ConceptSectionResult struct {
	Events         []Event
	Notes          []EvidenceNote
	Inverted       bool
	PresentDepths  []float64
	NegativeDepths []float64 // 有效负证据深度
}

// adequateCover 返回覆盖深度 d 的充分采样层段是否存在。
func adequateCover(intervals []SampleInterval, d float64) bool {
	for _, iv := range intervals {
		if iv.Adequate && iv.Covers(d) {
			return true
		}
	}
	return false
}

// anyCover 返回深度 d 是否被任何（含不充分）层段覆盖。
func anyCover(intervals []SampleInterval, d float64) bool {
	for _, iv := range intervals {
		if iv.Covers(d) {
			return true
		}
	}
	return false
}

// ComputeConceptEvents 计算一个概念在一条剖面中的 FAD/LAD 事件。
// 规则：
//   - absent 记录仅当被充分采样层段覆盖时才是负证据；
//     不被任何层段覆盖时按“未采样”剔除，不进入证据链。
//   - 重工候选点导致首现/末现被倒置时，生成多种解释，不按深度最值硬定。
func ComputeConceptEvents(c Concept, section string, occs []Occurrence, intervals []SampleInterval, generation int64) ConceptSectionResult {
	res := ConceptSectionResult{}
	var inSitu, clean []float64 // 原位（全部产出）/ 剔除重工后的产出深度
	var reworkedPts []Occurrence
	for _, o := range occs {
		switch o.Status {
		case StatusPresent:
			inSitu = append(inSitu, o.Depth)
			if o.Reworked {
				reworkedPts = append(reworkedPts, o)
				res.Notes = append(res.Notes, EvidenceNote{o.ID, "产出·重工候选", "保留记录但不直接参与最值"})
			} else {
				clean = append(clean, o.Depth)
				res.Notes = append(res.Notes, EvidenceNote{o.ID, "产出", "参与首现/末现"})
			}
		case StatusAbsent:
			switch {
			case adequateCover(intervals, o.Depth):
				res.NegativeDepths = append(res.NegativeDepths, o.Depth)
				res.Notes = append(res.Notes, EvidenceNote{o.ID, "未见·负证据", "采样充分，约束首现不早于此、末现不晚于此"})
			case anyCover(intervals, o.Depth):
				res.Notes = append(res.Notes, EvidenceNote{o.ID, "未见·降级", "采样充分度不可用，不作负证据"})
			default:
				res.Notes = append(res.Notes, EvidenceNote{o.ID, "未采样", "无采样覆盖，与未见不同，不作证据"})
			}
		}
	}
	res.PresentDepths = inSitu
	if len(inSitu) == 0 {
		return res
	}
	sort.Float64s(inSitu)
	sort.Float64s(clean)

	// 倒置检测：重工点落在非重工产出范围之外（更深的首现或更浅的末现）。
	inverted := false
	if len(clean) > 0 && len(reworkedPts) > 0 {
		lo, hi := clean[0], clean[len(clean)-1]
		for _, rp := range reworkedPts {
			if rp.Depth > hi || rp.Depth < lo {
				inverted = true
			}
		}
	}
	res.Inverted = inverted

	mk := func(kind string, depth float64, interp string, contested bool, evidence string) Event {
		return Event{
			ID:             fmt.Sprintf("%s|%s|%s|%s", c.Name, section, kind, interp),
			Kind:           kind,
			Section:        section,
			Concept:        c.Name,
			Depth:          depth,
			Interpretation: interp,
			Contested:      contested,
			Revision:       c.Revision,
			Generation:     generation,
			Evidence:       evidence,
		}
	}

	if !inverted {
		// 无倒置：单解释。首现取最深产出，末现取最浅产出（深度向下增大）。
		ev := fmt.Sprintf("产出点 %v；负证据 %v", inSitu, res.NegativeDepths)
		res.Events = append(res.Events,
			mk("FAD", inSitu[len(inSitu)-1], InterpInSitu, false, ev),
			mk("LAD", inSitu[0], InterpInSitu, false, ev),
		)
		return res
	}

	// 倒置：多解释并存，绝不按深度最值硬定唯一答案。
	// 解释一：原位解释，全部产出按原位处理。
	res.Events = append(res.Events,
		mk("FAD", inSitu[len(inSitu)-1], InterpInSitu, true,
			fmt.Sprintf("全部产出按原位：%v；含重工候选 %d 个", inSitu, len(reworkedPts))),
		mk("LAD", inSitu[0], InterpInSitu, true,
			fmt.Sprintf("全部产出按原位：%v；含重工候选 %d 个", inSitu, len(reworkedPts))),
	)
	// 解释二：剔除全部重工候选。
	if len(clean) > 0 {
		res.Events = append(res.Events,
			mk("FAD", clean[len(clean)-1], InterpExclude, true,
				fmt.Sprintf("剔除重工候选后产出：%v", clean)),
			mk("LAD", clean[0], InterpExclude, true,
				fmt.Sprintf("剔除重工候选后产出：%v", clean)),
		)
	}
	// 解释三：仅倒置点为重工（非倒置的重工候选仍按原位）。
	lo, hi := clean[0], clean[len(clean)-1]
	partial := append([]float64{}, clean...)
	for _, rp := range reworkedPts {
		if rp.Depth <= hi && rp.Depth >= lo {
			partial = append(partial, rp.Depth)
		}
	}
	sort.Float64s(partial)
	res.Events = append(res.Events,
		mk("FAD", partial[len(partial)-1], InterpInverted, true,
			fmt.Sprintf("仅倒置点判重工，其余原位：%v", partial)),
		mk("LAD", partial[0], InterpInverted, true,
			fmt.Sprintf("仅倒置点判重工，其余原位：%v", partial)),
	)
	return res
}
