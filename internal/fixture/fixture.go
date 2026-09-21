// Package fixture 提供“化石带议庭”的固定演示数据（固定 ID，便于重放与复核）。
package fixture

type Interval struct {
	ID, SectionID string
	Top, Base     float64
	Sufficient    bool
	Note          string
}

type Occurrence struct {
	ID, SectionID, TaxonID string
	Depth                  float64
	Status                 string // present | absent
	IntervalID             string
	Rework                 bool
	Note                   string
}

type Section struct {
	ID, Name, Unit string
	Ord            int
}
type Taxon struct{ ID, Name string }

type Group struct {
	ID, Name string
	Members  []string
	Note     string
}

type Tie struct {
	ID                                               string
	AGroup, ASection, AKind, BGroup, BSection, BKind string
	Note                                             string
}

type Data struct {
	Sections    []Section
	Taxa        []Taxon
	Intervals   []Interval
	Occurrences []Occurrence
	Groups      []Group
	Ties        []Tie
}

// Fixed 返回固定 fixture：
//
//	S1 龙脊坡：三叶虫 G-T(TA+TB) 正常产出，与 U 共存、与 Q 错层；
//	S2 青石崖：TB 在 60m 有异常高位点（重工候选），raw/in_situ 两种解释致 FAD 顺序倒置；
//	S3 白塔坪：G-T 完全未采样（无任何记录）；W 在充分采样层段中“未见”。
//
// 深度单位 m，数值向下增大（深度越大越老）。
func Fixed() Data {
	return Data{
		Sections: []Section{
			{"S1", "龙脊坡剖面", "m", 1},
			{"S2", "青石崖剖面", "m", 2},
			{"S3", "白塔坪剖面", "m", 3},
		},
		Taxa: []Taxon{
			{"TA", "Trilobites alpha"},
			{"TB", "Trilobites beta"},
			{"U", "Indexofusus uniformis"},
			{"Q", "Brachiopoda quieta"},
			{"W", "Brachiopoda witnessi"},
		},
		Intervals: []Interval{
			{"I-S1-1", "S1", 40, 110, true, "J1 采样充分"},
			{"I-S1-2", "S1", 170, 250, true, "J2 采样充分"},
			{"I-S1-3", "S1", 300, 320, true, "J3 采样充分"},
			{"I-S1-4", "S1", 330, 360, false, "J4 风化严重，采样不足"},
			{"I-S2-1", "S2", 40, 120, true, "J5 采样充分"},
			{"I-S2-2", "S2", 130, 230, true, "J6 采样充分"},
			{"I-S2-3", "S2", 240, 280, false, "J7 覆盖差，采样不足"},
			{"I-S3-1", "S3", 100, 160, true, "J8 采样充分"},
			{"I-S3-2", "S3", 170, 230, true, "J9 采样充分"},
			{"I-S3-3", "S3", 240, 280, false, "J10 采样不足"},
		},
		Occurrences: []Occurrence{
			// S1 龙脊坡：G-T 正常产出于 80-200
			{"O-S1-1", "S1", "TA", 80, "present", "I-S1-1", false, ""},
			{"O-S1-2", "S1", "TA", 130, "present", "I-S1-1", false, ""},
			{"O-S1-3", "S1", "TA", 180, "present", "I-S1-2", false, ""},
			{"O-S1-4", "S1", "TB", 95, "present", "I-S1-1", false, ""},
			{"O-S1-5", "S1", "TB", 150, "present", "I-S1-1", false, ""},
			{"O-S1-6", "S1", "TB", 200, "present", "I-S1-2", false, ""},
			{"O-S1-7", "S1", "Q", 110, "present", "I-S1-1", false, ""},
			{"O-S1-8", "S1", "Q", 170, "present", "I-S1-2", false, ""},
			{"O-S1-9", "S1", "U", 220, "present", "I-S1-2", false, ""},
			{"O-S1-10", "S1", "U", 260, "present", "I-S1-2", false, ""},
			{"O-S1-11", "S1", "U", 310, "present", "I-S1-3", false, ""},
			{"O-S1-12", "S1", "U", 40, "absent", "I-S1-1", false, "U 在 J1 充分采样中未见"},
			{"O-S1-13", "S1", "W", 40, "absent", "I-S1-1", false, "W 在 J1 充分采样中未见"},
			// S2 青石崖：TB@60 异常高位，标重工候选
			{"O-S2-1", "S2", "TB", 60, "present", "I-S2-1", true, "异常高位产出，疑似再搬运"},
			{"O-S2-2", "S2", "TB", 150, "present", "I-S2-2", false, ""},
			{"O-S2-3", "S2", "TB", 210, "present", "I-S2-2", false, ""},
			{"O-S2-4", "S2", "U", 100, "present", "I-S2-1", false, ""},
			{"O-S2-5", "S2", "U", 140, "present", "I-S2-2", false, ""},
			{"O-S2-6", "S2", "U", 190, "present", "I-S2-2", false, ""},
			{"O-S2-7", "S2", "Q", 130, "present", "I-S2-2", false, ""},
			{"O-S2-8", "S2", "Q", 200, "present", "I-S2-2", false, ""},
			{"O-S2-9", "S2", "W", 110, "absent", "I-S2-1", false, "W 在 J5 充分采样中未见"},
			// S3 白塔坪：G-T 无任何记录（完全未采样）；W 充分采样下未见
			{"O-S3-1", "S3", "U", 120, "present", "I-S3-1", false, ""},
			{"O-S3-2", "S3", "U", 180, "present", "I-S3-2", false, ""},
			{"O-S3-3", "S3", "Q", 200, "present", "I-S3-2", false, ""},
			{"O-S3-4", "S3", "W", 150, "absent", "I-S3-1", false, "W 在 J8 充分采样中未见"},
		},
		Groups: []Group{
			{"G-T", "三叶虫等义组", []string{"TA", "TB"}, "fixture 预置：TA/TB 视为同属"},
		},
		Ties: []Tie{
			// 前两条锦标在三种层位上不闭合；加入第三条闭合锦标后成环。
			{"TIE-SEED1", "~U", "S1", "LAD", "~U", "S2", "FAD", "跨区标志层对齐 a"},
			{"TIE-SEED2", "~U", "S2", "LAD", "~U", "S3", "FAD", "跨区标志层对齐 b"},
		},
	}
}
