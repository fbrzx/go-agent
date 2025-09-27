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
				ChunkID:    "chunk-1",
				DocumentID: "doc-1",
				Title:      "Doc One",
				Path:       "doc1.md",
				Content:    "This is a relevant paragraph about adoption.",
				Score:      0.9,
			},
		}},
		&stubGraphStore{data: map[string]chat.DocumentInsight{
			"doc-1": {ChunkCount: 4},
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

	if resp.Sources[0].Insight.ChunkCount != 4 {
		t.Fatalf("expected chunk count 4, got %d", resp.Sources[0].Insight.ChunkCount)
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

	if _, err := svc.Chat(context.Background(), "question", chat.Config{}); err == nil {
		t.Fatal("expected error when no context is found")
	}
}
