package chat

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/fabfab/go-agent/embeddings"
	"github.com/fabfab/go-agent/llm"
)

const (
	defaultSimilarityLimit = 5
)

type Service struct {
	vectors  VectorStore
	graph    GraphStore
	embedder embeddings.Embedder
	llm      llm.Client
	logger   *log.Logger
}

type Config struct {
	SimilarityLimit int
}

func NewService(vectors VectorStore, graph GraphStore, embedder embeddings.Embedder, llmClient llm.Client, logger *log.Logger) *Service {
	if logger == nil {
		logger = log.Default()
	}

	return &Service{
		vectors:  vectors,
		graph:    graph,
		embedder: embedder,
		llm:      llmClient,
		logger:   logger,
	}
}

func (s *Service) Chat(ctx context.Context, question string, cfg Config) (Response, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return Response{}, fmt.Errorf("question cannot be empty")
	}
	if s.embedder == nil {
		return Response{}, fmt.Errorf("embedder is not configured")
	}
	if s.vectors == nil {
		return Response{}, fmt.Errorf("vector store is not configured")
	}
	if s.llm == nil {
		return Response{}, fmt.Errorf("llm client is not configured")
	}

	limit := cfg.SimilarityLimit
	if limit <= 0 {
		limit = defaultSimilarityLimit
	}

	embeddings, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return Response{}, fmt.Errorf("embed question: %w", err)
	}
	if len(embeddings) == 0 {
		return Response{}, fmt.Errorf("embedder returned no vectors")
	}

	chunks, err := s.vectors.SimilarChunks(ctx, embeddings[0], limit)
	if err != nil {
		return Response{}, fmt.Errorf("vector search: %w", err)
	}

	if len(chunks) == 0 {
		return Response{}, fmt.Errorf("no relevant context found for the question")
	}

	docIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		docIDs = append(docIDs, chunk.DocumentID)
	}

	insights := map[string]DocumentInsight{}
	if s.graph != nil {
		insightMap, insightErr := s.graph.DocumentInsights(ctx, unique(docIDs))
		if insightErr != nil {
			s.logger.Printf("graph insights error: %v", insightErr)
		} else {
			insights = insightMap
		}
	}

	sources := mergeSources(chunks, insights)
	contextPrompt := buildContextPrompt(sources)

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt()},
		{Role: llm.RoleUser, Content: formatUserPrompt(question, contextPrompt)},
	}

	answer, err := s.llm.Generate(ctx, messages)
	if err != nil {
		return Response{}, fmt.Errorf("llm generate: %w", err)
	}

	return Response{Answer: strings.TrimSpace(answer), Sources: sources}, nil
}

func mergeSources(chunks []ChunkResult, insights map[string]DocumentInsight) []Source {
	grouped := make(map[string]Source)
	for _, chunk := range chunks {
		source := grouped[chunk.DocumentID]
		if source.DocumentID == "" {
			source.DocumentID = chunk.DocumentID
			source.Title = chunk.Title
			source.Path = chunk.Path
			source.Score = chunk.Score
		} else if chunk.Score > source.Score {
			source.Score = chunk.Score
		}

		snippet := strings.TrimSpace(chunk.Content)
		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
		}
		if source.Snippet == "" {
			source.Snippet = snippet
		} else if !strings.Contains(source.Snippet, snippet) {
			source.Snippet += "\n---\n" + snippet
		}

		if insight, ok := insights[chunk.DocumentID]; ok {
			source.Insight = insight
		}

		grouped[chunk.DocumentID] = source
	}

	sources := make([]Source, 0, len(grouped))
	for _, src := range grouped {
		sources = append(sources, src)
	}

	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Score > sources[j].Score
	})

	return sources
}

func buildContextPrompt(sources []Source) string {
	var sb strings.Builder
	for idx, source := range sources {
		sb.WriteString(fmt.Sprintf("Source %d: %s (%s)\n", idx+1, source.Title, source.Path))
		if source.Insight.ChunkCount > 0 {
			sb.WriteString(fmt.Sprintf("Chunks indexed: %d\n", source.Insight.ChunkCount))
		}
		if len(source.Insight.Folders) > 0 {
			sb.WriteString("Folders: " + strings.Join(source.Insight.Folders, ", ") + "\n")
		}
		if len(source.Insight.RelatedDocuments) > 0 {
			sb.WriteString("Related documents:\n")
			for _, related := range source.Insight.RelatedDocuments {
				sb.WriteString(fmt.Sprintf("- %s (%s)\n", related.Title, related.Path))
			}
		}
		sb.WriteString(source.Snippet)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func systemPrompt() string {
	return "You are a helpful assistant that answers questions using the provided context. If the context does not contain the answer, say that you do not know."
}

func formatUserPrompt(question, context string) string {
	var sb strings.Builder
	sb.WriteString("Context:\n")
	sb.WriteString(context)
	sb.WriteString("Question:\n")
	sb.WriteString(question)
	sb.WriteString("\nAnswer concisely using markdown.")
	return sb.String()
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}
