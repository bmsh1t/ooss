package rerank

type Document struct {
	ID       string
	Text     string
	Metadata map[string]string
}

type Request struct {
	Query         string
	Documents     []Document
	TopN          int
	MaxCandidates int
	MinScore      float64
	ModelOverride string
}

type Result struct {
	ID       string            `json:"id"`
	Index    int               `json:"index"`
	Score    float64           `json:"score"`
	Text     string            `json:"text"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Response struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Results  []Result `json:"results"`
}
