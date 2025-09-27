package unit

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	"github.com/fabfab/go-agent/chat"
	"github.com/fabfab/go-agent/embeddings"
	"github.com/fabfab/go-agent/llm"
)

type stubEmbedder struct {
	vectors [][]float32
	err     error
}

func (s *stubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.vectors) == 0 {
		return nil, nil
	}
	return s.vectors, nil
}

var _ embeddings.Embedder = (*stubEmbedder)(nil)

type stubVectorStore struct {
	results []chat.ChunkResult
	err     error
}

func (s *stubVectorStore) SimilarChunks(ctx context.Context, embedding []float32, limit int) ([]chat.ChunkResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

var _ chat.VectorStore = (*stubVectorStore)(nil)

type stubGraphStore struct {
	data map[string]chat.DocumentInsight
	err  error
}

func (s *stubGraphStore) DocumentInsights(ctx context.Context, docIDs []string) (map[string]chat.DocumentInsight, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.data == nil {
		return map[string]chat.DocumentInsight{}, nil
	}
	return s.data, nil
}

var _ chat.GraphStore = (*stubGraphStore)(nil)

type stubLLM struct {
	answer string
	err    error
}

func (s *stubLLM) Generate(ctx context.Context, messages []llm.Message) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if len(messages) == 0 {
		return "", errors.New("no messages provided")
	}
	return s.answer, nil
}

var _ llm.Client = (*stubLLM)(nil)

func TestChatServiceReturnsAnswer(t *testing.T) {
	svc := chat.NewService(
		&stubVectorStore{results: []chat.ChunkResult{
			{
				ChunkID:      "chunk-1",
				DocumentID:   "doc-1",
				Title:        "Doc One",
				Path:         "doc1.md",
				Content:      "This is a relevant paragraph about adoption.",
				Score:        0.9,
				SectionTitle: "Section A",
				SectionOrder: 1,
			},
		}},
		&stubGraphStore{data: map[string]chat.DocumentInsight{
			"doc-1": {
				ChunkCount:       4,
				Folders:          []string{"knowledge"},
				RelatedDocuments: []chat.RelatedDocument{{ID: "doc-2", Title: "Doc Two", Path: "doc2.md", Weight: 2, Reason: "topic"}},
				Sections:         []chat.SectionInfo{{Title: "Section A", Level: 2, Order: 1}},
				Topics:           []string{"Topic A"},
			},
		}},
		&stubEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}},
		&stubLLM{answer: "Here is the response."},
		log.New(io.Discard, "", 0),
	)

	resp, err := svc.Chat(context.Background(), "What is our adoption strategy?", chat.Config{SimilarityLimit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Answer != "Here is the response." {
		t.Fatalf("unexpected answer: %q", resp.Answer)
	}

	if len(resp.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(resp.Sources))
	}

	source := resp.Sources[0]
	if source.Insight.ChunkCount != 4 {
		t.Fatalf("expected chunk count 4, got %d", source.Insight.ChunkCount)
	}
	if len(source.Insight.Folders) != 1 || source.Insight.Folders[0] != "knowledge" {
		t.Fatalf("expected folder 'knowledge', got %#v", source.Insight.Folders)
	}
	if len(source.Insight.RelatedDocuments) != 1 || source.Insight.RelatedDocuments[0].ID != "doc-2" {
		t.Fatalf("expected related doc 'doc-2', got %#v", source.Insight.RelatedDocuments)
	}
	if len(source.Insight.Sections) != 1 || source.Insight.Sections[0].Title != "Section A" {
		t.Fatalf("expected section 'Section A', got %#v", source.Insight.Sections)
	}
	if len(source.Insight.Topics) != 1 || source.Insight.Topics[0] != "Topic A" {
		t.Fatalf("expected topic 'Topic A', got %#v", source.Insight.Topics)
	}
}

func TestChatServiceValidatesQuestion(t *testing.T) {
	svc := chat.NewService(&stubVectorStore{}, &stubGraphStore{}, &stubEmbedder{}, &stubLLM{}, log.New(io.Discard, "", 0))
	if _, err := svc.Chat(context.Background(), "   ", chat.Config{}); err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestChatServiceHandlesNoResults(t *testing.T) {
	svc := chat.NewService(
		&stubVectorStore{results: []chat.ChunkResult{}},
		&stubGraphStore{},
		&stubEmbedder{vectors: [][]float32{{0.1}}},
		&stubLLM{answer: "irrelevant"},
		log.New(io.Discard, "", 0),
	)

	resp, err := svc.Chat(context.Background(), "question", chat.Config{})
	if err != nil {
		t.Fatalf("expected fallback to LLM without context, got %v", err)
	}
	if resp.Answer == "" {
		t.Fatal("expected LLM answer even with empty context")
	}
}

func TestChatServiceSectionFilter(t *testing.T) {
	svc := chat.NewService(
		&stubVectorStore{results: []chat.ChunkResult{{
			ChunkID:      "chunk-1",
			DocumentID:   "doc-1",
			Title:        "Doc One",
			Path:         "doc1.md",
			Content:      "Paragraph",
			Score:        0.9,
			SectionTitle: "Overview",
			SectionOrder: 1,
		}}},
		&stubGraphStore{data: map[string]chat.DocumentInsight{
			"doc-1": {
				ChunkCount: 1,
				Topics:     []string{"Topic"},
			},
		}},
		&stubEmbedder{vectors: [][]float32{{0.1}}},
		&stubLLM{answer: "ok"},
		log.New(io.Discard, "", 0),
	)

	if _, err := svc.Chat(context.Background(), "question", chat.Config{SectionFilters: []string{"overview"}}); err != nil {
		t.Fatalf("expected section filter to pass, got %v", err)
	}

	if _, err := svc.Chat(context.Background(), "question", chat.Config{SectionFilters: []string{"detail"}}); err == nil {
		t.Fatal("expected error when section filter does not match")
	}
}

func TestChatServiceTopicFilter(t *testing.T) {
	svc := chat.NewService(
		&stubVectorStore{results: []chat.ChunkResult{{
			ChunkID:      "chunk-1",
			DocumentID:   "doc-1",
			Title:        "Doc One",
			Path:         "doc1.md",
			Content:      "Paragraph",
			Score:        0.9,
			SectionTitle: "Overview",
			SectionOrder: 1,
		}}},
		&stubGraphStore{data: map[string]chat.DocumentInsight{
			"doc-1": {
				ChunkCount: 1,
				Topics:     []string{"Topic"},
			},
		}},
		&stubEmbedder{vectors: [][]float32{{0.1}}},
		&stubLLM{answer: "ok"},
		log.New(io.Discard, "", 0),
	)

	if _, err := svc.Chat(context.Background(), "question", chat.Config{TopicFilters: []string{"topic"}}); err != nil {
		t.Fatalf("expected topic filter to pass, got %v", err)
	}

	if _, err := svc.Chat(context.Background(), "question", chat.Config{TopicFilters: []string{"other"}}); err == nil {
		t.Fatal("expected error when topic filter does not match")
	}
}
