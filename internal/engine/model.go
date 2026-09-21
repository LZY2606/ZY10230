package engine

type Section struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Ord       int    `json:"ord"`
	DepthUnit string `json:"depthUnit"`
}

type Taxon struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Interval struct {
	ID         string  `json:"id"`
	SectionID  string  `json:"id_section"`
	TopDepth   float64 `json:"topDepth"`
	BaseDepth  float64 `json:"baseDepth"`
	Sufficient bool    `json:"sufficient"`
	Note       string  `json:"note"`
}

type Occurrence struct {
	ID         string  `json:"id"`
	SectionID  string  `json:"sectionId"`
	TaxonID    string  `json:"taxonId"`
	Depth      float64 `json:"depth"`
	Status     string  `json:"status"` // present | absent
	IntervalID string  `json:"intervalId"`
	Note       string  `json:"note"`
	Rework     bool    `json:"rework"`
}

type TaxonStatus struct {
	SectionID string `json:"sectionId"`
	TaxonID   string `json:"taxonId"`
	// present 正常产出 | absent 未见(充分采样下的负证据) |
	// insufficient 采样不足(未见但不能作负证据) | unsampled 完全未采样
	State    string   `json:"state"`
	Evidence []string `json:"evidence"`
}

type Group struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Members     []string `json:"members"`
	Version     int      `json:"version"`
}

type SynVersion struct {
	Version     int      `json:"version"`
	CreatedAt   string   `json:"createdAt"`
	Kind        string   `json:"kind"`
	GroupID     string   `json:"groupId"`
	DisplayName string   `json:"displayName"`
	Members     []string `json:"members"`
	Note        string   `json:"note"`
}

type ReworkCandidate struct {
	OccurrenceID string `json:"occurrenceId"`
	Marked       bool   `json:"marked"`
	UpdatedAt    string `json:"updatedAt"`
	Note         string `json:"note"`
}

type Tie struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"createdAt"`
	AGroup     string `json:"aGroup"`
	ASection   string `json:"aSection"`
	AKind      string `json:"aKind"`
	BGroup     string `json:"bGroup"`
	BSection   string `json:"bSection"`
	BKind      string `json:"bKind"`
	Locked     bool   `json:"locked"`
	Active     bool   `json:"active"`
	Conflicted bool   `json:"conflicted"`
	Resolvable bool   `json:"resolvable"`
	Note       string `json:"note"`
}

type Evidence struct {
	OccurrenceID string `json:"occurrenceId,omitempty"`
	IntervalID   string `json:"intervalId,omitempty"`
	Role         string `json:"role"`
	Note         string `json:"note"`
}

type Event struct {
	ID             string     `json:"id"`
	Revision       int        `json:"revision"`
	Interpretation string     `json:"interpretation"` // raw | in_situ
	Kind           string     `json:"kind"`           // FAD | LAD | COEX
	GroupID        string     `json:"groupId"`
	SectionID      string     `json:"sectionId"`
	Depth          *float64   `json:"depth"`
	PartnerGroup   string     `json:"partnerGroup,omitempty"`
	Evidence       []Evidence `json:"evidence"`
	DependsOn      []string   `json:"dependsOn"`
	UpdatedAt      string     `json:"updatedAt"`
}

type Horizon struct {
	GroupID   string   `json:"groupId"`
	SectionID string   `json:"sectionId"`
	Kind      string   `json:"kind"`
	DepthRaw  *float64 `json:"depthRaw"`
	DepthIn   *float64 `json:"depthInSitu"`
}

type Violation struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"` // hard | soft
	Message  string   `json:"message"`
	Refs     []string `json:"refs"`
}

type Zone struct {
	Index   int      `json:"index"`
	Name    string   `json:"name"`
	Top     *float64 `json:"top"`
	Base    *float64 `json:"base"`
	Groups  []string `json:"groups"`
	Section string   `json:"section,omitempty"`
}

type Scheme struct {
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	Interpretation string      `json:"interpretation"`
	Scope          string      `json:"scope"` // per-section | global
	SectionID      string      `json:"sectionId,omitempty"`
	Zones          []Zone      `json:"zones"`
	Violations     []Violation `json:"violations"`
	ConflictChain  []ChainStep `json:"conflictChain,omitempty"`
}

type ChainStep struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Edge   string `json:"edge"` // order | tie
	TieID  string `json:"tieId,omitempty"`
	Detail string `json:"detail"`
}

type RecomputeEntry struct {
	ID         int    `json:"id"`
	At         string `json:"at"`
	Cause      string `json:"cause"`
	Scope      string `json:"scope"`
	Recomputed int    `json:"recomputed"`
	Preserved  int    `json:"preserved"`
	Detail     string `json:"detail"`
}

type State struct {
	SchemaVersion int               `json:"schemaVersion"`
	GeneratedAt   string            `json:"generatedAt"`
	Sections      []Section         `json:"sections"`
	Taxa          []Taxon           `json:"taxa"`
	Intervals     []Interval        `json:"intervals"`
	Occurrences   []Occurrence      `json:"occurrences"`
	Statuses      []TaxonStatus     `json:"statuses"`
	Groups        []Group           `json:"groups"`
	SynVersions   []SynVersion      `json:"synVersions"`
	Rework        []ReworkCandidate `json:"reworkCandidates"`
	Ties          []Tie             `json:"ties"`
	Horizons      []Horizon         `json:"horizons"`
	Events        []Event           `json:"events"`
	Schemes       []Scheme          `json:"schemes"`
	RecomputeLog  []RecomputeEntry  `json:"recomputeLog"`
}
