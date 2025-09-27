package chat

type ChunkResult struct {
	ChunkID    string
	DocumentID string
	Title      string
	Path       string
	Content    string
	Score      float64
}

type DocumentInsight struct {
	ChunkCount int
}

type Source struct {
	DocumentID string
	Title      string
	Path       string
	Snippet    string
	Score      float64
	Insight    DocumentInsight
}

type Response struct {
	Answer  string
	Sources []Source
}
