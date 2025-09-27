package chat

type ChunkResult struct {
	ChunkID      string
	DocumentID   string
	Title        string
	Path         string
	Content      string
	Score        float64
	SectionTitle string
	SectionLevel int
	SectionOrder int
}

type DocumentInsight struct {
	ChunkCount       int
	Folders          []string
	RelatedDocuments []RelatedDocument
	Sections         []SectionInfo
	Topics           []string
}

type RelatedDocument struct {
	ID     string
	Title  string
	Path   string
	Weight float64
	Reason string
}

type SectionInfo struct {
	Title string
	Level int
	Order int
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
