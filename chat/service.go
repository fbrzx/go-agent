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
	SectionFilters  []string
	TopicFilters    []string
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
	resp, _, err := s.chat(ctx, question, cfg, nil, nil)
	return resp, err
}

// ChatStream runs the chat workflow while optionally streaming the LLM output.
// The provided history slice contains prior conversation turns (excluding the
// system prompt) and will be extended with the latest user/assistant messages on
// success. When the LLM implementation does not support streaming, the callback
// receives the full answer once.
func (s *Service) ChatStream(
	ctx context.Context,
	question string,
	cfg Config,
	history []llm.Message,
	streamFn func(string) error,
) (Response, []llm.Message, error) {
	return s.chat(ctx, question, cfg, history, streamFn)
}

func (s *Service) chat(
	ctx context.Context,
	question string,
	cfg Config,
	history []llm.Message,
	streamFn func(string) error,
) (Response, []llm.Message, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return Response{}, nil, fmt.Errorf("question cannot be empty")
	}
	if s.embedder == nil {
		return Response{}, nil, fmt.Errorf("embedder is not configured")
	}
	if s.vectors == nil {
		return Response{}, nil, fmt.Errorf("vector store is not configured")
	}
	if s.llm == nil {
		return Response{}, nil, fmt.Errorf("llm client is not configured")
	}

	limit := cfg.SimilarityLimit
	if limit <= 0 {
		limit = defaultSimilarityLimit
	}

	embeddings, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return Response{}, nil, fmt.Errorf("embed question: %w", err)
	}
	if len(embeddings) == 0 {
		return Response{}, nil, fmt.Errorf("embedder returned no vectors")
	}

	chunks, err := s.vectors.SimilarChunks(ctx, embeddings[0], limit)
	if err != nil {
		return Response{}, nil, fmt.Errorf("vector search: %w", err)
	}

	ctxEmpty := len(chunks) == 0

	if ctxEmpty {
		s.logger.Printf("no context available for question, falling back to LLM-only response")
	}

	if len(cfg.SectionFilters) > 0 && !ctxEmpty {
		filtered := filterChunksBySections(chunks, cfg.SectionFilters)
		if len(filtered) == 0 {
			return Response{}, nil, fmt.Errorf("no chunks matched the requested sections")
		}
		chunks = filtered
	}

	docIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		docIDs = append(docIDs, chunk.DocumentID)
	}

	insights := map[string]DocumentInsight{}
	if s.graph != nil && len(docIDs) > 0 {
		insightMap, insightErr := s.graph.DocumentInsights(ctx, unique(docIDs))
		if insightErr != nil {
			s.logger.Printf("graph insights error: %v", insightErr)
		} else {
			insights = insightMap
		}
	}

	sources := mergeSources(chunks, insights)
	if len(cfg.TopicFilters) > 0 && len(sources) > 0 {
		filteredSources := filterSourcesByTopics(sources, cfg.TopicFilters)
		if len(filteredSources) == 0 {
			return Response{}, nil, fmt.Errorf("no documents matched the requested topics")
		}
		sources = filteredSources
	}

	contextPrompt := ""
	if len(sources) > 0 {
		contextPrompt = buildContextPrompt(sources)
	}

	messages := make([]llm.Message, 0, len(history)+2)
	messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: systemPrompt()})
	if len(history) > 0 {
		messages = append(messages, history...)
	}
	userMessage := llm.Message{Role: llm.RoleUser, Content: formatUserPrompt(question, contextPrompt)}
	messages = append(messages, userMessage)

	var answer string
	if streamFn != nil {
		if streamClient, ok := s.llm.(llm.StreamClient); ok {
			var builder strings.Builder
			streamErr := streamClient.GenerateStream(ctx, messages, func(chunk string) error {
				if chunk == "" {
					return nil
				}
				builder.WriteString(chunk)
				return streamFn(chunk)
			})
			if streamErr != nil {
				return Response{}, nil, fmt.Errorf("llm stream generate: %w", streamErr)
			}
			answer = builder.String()
		} else {
			generated, genErr := s.llm.Generate(ctx, messages)
			if genErr != nil {
				return Response{}, nil, fmt.Errorf("llm generate: %w", genErr)
			}
			answer = generated
			if err := streamFn(answer); err != nil {
				return Response{}, nil, err
			}
		}
	} else {
		generated, genErr := s.llm.Generate(ctx, messages)
		if genErr != nil {
			return Response{}, nil, fmt.Errorf("llm generate: %w", genErr)
		}
		answer = generated
	}

	answer = strings.TrimSpace(answer)
	assistantMessage := llm.Message{Role: llm.RoleAssistant, Content: answer}

	updatedHistory := make([]llm.Message, 0, len(history)+2)
	if len(history) > 0 {
		updatedHistory = append(updatedHistory, history...)
	}
	updatedHistory = append(updatedHistory, userMessage, assistantMessage)

	return Response{Answer: answer, Sources: sources}, updatedHistory, nil
}

func mergeSources(chunks []ChunkResult, insights map[string]DocumentInsight) []Source {
	grouped := make(map[string]*Source, len(chunks))
	for i := range chunks {
		chunk := chunks[i]
		source, ok := grouped[chunk.DocumentID]
		if !ok {
			source = &Source{
				DocumentID: chunk.DocumentID,
				Title:      chunk.Title,
				Path:       chunk.Path,
				Score:      chunk.Score,
			}
			grouped[chunk.DocumentID] = source
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
	}

	sources := make([]Source, 0, len(grouped))
	for _, src := range grouped {
		sources = append(sources, *src)
	}

	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Score > sources[j].Score
	})

	return sources
}

func buildContextPrompt(sources []Source) string {
	var sb strings.Builder
	for idx := range sources {
		source := &sources[idx]
		sb.WriteString(fmt.Sprintf("Source %d: %s (%s)\n", idx+1, source.Title, source.Path))
		if source.Insight.ChunkCount > 0 {
			sb.WriteString(fmt.Sprintf("Chunks indexed: %d\n", source.Insight.ChunkCount))
		}
		if len(source.Insight.Sections) > 0 {
			var parts []string
			for i := range source.Insight.Sections {
				section := source.Insight.Sections[i]
				if section.Title == "" {
					continue
				}
				parts = append(parts, fmt.Sprintf("%s (level %d)", section.Title, section.Level))
			}
			if len(parts) > 0 {
				sb.WriteString("Sections: " + strings.Join(parts, "; ") + "\n")
			}
		}
		if len(source.Insight.Topics) > 0 {
			sb.WriteString("Topics: " + strings.Join(source.Insight.Topics, ", ") + "\n")
		}
		if len(source.Insight.Folders) > 0 {
			sb.WriteString("Folders: " + strings.Join(source.Insight.Folders, ", ") + "\n")
		}
		if len(source.Insight.RelatedDocuments) > 0 {
			sb.WriteString("Related documents:\n")
			for i := range source.Insight.RelatedDocuments {
				related := source.Insight.RelatedDocuments[i]
				weightInfo := ""
				if related.Weight > 0 {
					weightInfo = fmt.Sprintf(" weight %.2f", related.Weight)
				}
				reasonInfo := ""
				if related.Reason != "" {
					reasonInfo = fmt.Sprintf(" via %s", related.Reason)
				}
				sb.WriteString(fmt.Sprintf("- %s (%s)%s%s\n", related.Title, related.Path, weightInfo, reasonInfo))
			}
		}
		sb.WriteString(source.Snippet)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func systemPrompt() string {
	return "You are a helpful assistant. Use the supplied context to enrich and support your response, citing Source numbers in brackets (e.g., [Source 1]) when you draw from it. If the context is missing or not useful, rely on your general knowledge, note any uncertainty, and still deliver the best possible answer. Always answer the question first, then optionally add brief context notes."
}

func formatUserPrompt(question, context string) string {
	var sb strings.Builder
	sb.WriteString("Question:\n")
	sb.WriteString(question)
	if strings.TrimSpace(context) != "" {
		sb.WriteString("\nContext (optional, may be incomplete):\n")
		sb.WriteString(context)
	}
	sb.WriteString("\nProvide your answer in markdown. Begin with the direct answer. If you reference the context, cite the relevant Source numbers. Conclude with a short 'Context Notes' section only when you actually used the context.")
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

func filterChunksBySections(chunks []ChunkResult, filters []string) []ChunkResult {
	normalized := normalizeFilters(filters)
	if len(normalized) == 0 {
		return chunks
	}

	filtered := make([]ChunkResult, 0, len(chunks))
	for i := range chunks {
		chunk := chunks[i]
		sectionTitle := strings.ToLower(strings.TrimSpace(chunk.SectionTitle))
		if sectionTitle == "" {
			sectionTitle = "introduction"
		}
		order := fmt.Sprintf("%d", chunk.SectionOrder)
		for _, filter := range normalized {
			if strings.Contains(sectionTitle, filter) || (filter != "" && order == filter) {
				filtered = append(filtered, chunk)
				break
			}
		}
	}
	return filtered
}

func filterSourcesByTopics(sources []Source, filters []string) []Source {
	normalized := normalizeFilters(filters)
	if len(normalized) == 0 {
		return sources
	}

	filtered := make([]Source, 0, len(sources))
	for i := range sources {
		source := &sources[i]
		topics := make([]string, len(source.Insight.Topics))
		for j, topic := range source.Insight.Topics {
			topics[j] = strings.ToLower(strings.TrimSpace(topic))
		}
		if containsAny(topics, normalized) {
			filtered = append(filtered, *source)
		}
	}
	return filtered
}

func normalizeFilters(filters []string) []string {
	result := make([]string, 0, len(filters))
	for _, filter := range filters {
		trimmed := strings.ToLower(strings.TrimSpace(filter))
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func containsAny(values, filters []string) bool {
	valueSet := make(map[string]struct{}, len(values))
	for _, v := range values {
		valueSet[v] = struct{}{}
	}
	for _, filter := range filters {
		if _, ok := valueSet[filter]; ok {
			return true
		}
		for value := range valueSet {
			if strings.Contains(value, filter) {
				return true
			}
		}
	}
	return false
}
