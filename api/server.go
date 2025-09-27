package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/fabfab/go-agent/chat"
	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/database"
	"github.com/fabfab/go-agent/embeddings"
	"github.com/fabfab/go-agent/ingestion"
	"github.com/fabfab/go-agent/llm"
)

const defaultChatLimit = 5

// Server exposes HTTP handlers for the core go-agent workflows.
type Server struct {
	cfg     config.Config
	logger  *log.Logger
	handler http.Handler
}

type messageResponse struct {
	Message string `json:"message"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type ingestRequest struct {
	Dir string `json:"dir"`
}

type clearRequest struct {
	Confirm bool `json:"confirm"`
}

type chatRequest struct {
	Question string   `json:"question"`
	Limit    int      `json:"limit"`
	Sections []string `json:"sections"`
	Topics   []string `json:"topics"`
}

type chatResponse struct {
	Answer  string       `json:"answer"`
	Sources []chatSource `json:"sources"`
}

type chatSource struct {
	DocumentID string              `json:"documentId"`
	Title      string              `json:"title"`
	Path       string              `json:"path"`
	Snippet    string              `json:"snippet"`
	Score      float64             `json:"score"`
	Insight    chatDocumentInsight `json:"insight"`
}

type chatDocumentInsight struct {
	ChunkCount       int                   `json:"chunkCount"`
	Folders          []string              `json:"folders"`
	Sections         []chatSectionInfo     `json:"sections"`
	Topics           []string              `json:"topics"`
	RelatedDocuments []chatRelatedDocument `json:"relatedDocuments"`
}

type chatSectionInfo struct {
	Title string `json:"title"`
	Level int    `json:"level"`
	Order int    `json:"order"`
}

type chatRelatedDocument struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	Weight     float64 `json:"weight"`
	Similarity float64 `json:"similarity"`
	Reason     string  `json:"reason"`
}

// New constructs a Server that serves the HTTP API using the provided configuration.
func New(cfg config.Config, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}

	s := &Server{cfg: cfg, logger: logger}
	s.handler = s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("/v1/ingest", s.handleIngest)
	mux.HandleFunc("/v1/chat", s.handleChat)
	mux.HandleFunc("/v1/clear", s.handleClear)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	s.writeJSON(w, http.StatusOK, messageResponse{Message: "ok"})
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"openapi.yaml\"")
	_, _ = w.Write(openAPISpecYAML)
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	var req ingestRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		dir = s.cfg.DataDir
	}

	ctx := r.Context()

	pgPool, err := database.NewPostgresPool(ctx, s.cfg.PostgresDSN)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("postgres connection: %w", err))
		return
	}
	defer pgPool.Close()

	neo4jDriver, err := database.NewNeo4jDriver(ctx, s.cfg.Neo4jURI, s.cfg.Neo4jUser, s.cfg.Neo4jPass)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("neo4j connection: %w", err))
		return
	}
	defer neo4jDriver.Close(ctx)

	embedder, err := embeddings.NewEmbedder(s.cfg)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("embedder setup: %w", err))
		return
	}

	svc := ingestion.NewService(pgPool, neo4jDriver, embedder, s.logger, s.cfg.Embeddings.Dimension)
	s.logger.Printf("ingesting markdown from %s using %s/%s embeddings", dir, strings.ToUpper(s.cfg.Embeddings.Provider), s.cfg.Embeddings.Model)

	if err := svc.IngestDirectory(ctx, dir); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("ingestion failed: %w", err))
		return
	}

	s.writeJSON(w, http.StatusOK, messageResponse{Message: "ingestion complete"})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	var req chatRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("question is required"))
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultChatLimit
	}

	ctx := r.Context()

	pgPool, err := database.NewPostgresPool(ctx, s.cfg.PostgresDSN)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("postgres connection: %w", err))
		return
	}
	defer pgPool.Close()

	neo4jDriver, err := database.NewNeo4jDriver(ctx, s.cfg.Neo4jURI, s.cfg.Neo4jUser, s.cfg.Neo4jPass)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("neo4j connection: %w", err))
		return
	}
	defer neo4jDriver.Close(ctx)

	embedder, err := embeddings.NewEmbedder(s.cfg)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("embedder setup: %w", err))
		return
	}

	llmClient, err := llm.NewClient(s.cfg)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("llm setup: %w", err))
		return
	}

	vectorStore := chat.NewPostgresVectorStore(pgPool)
	graphStore := chat.NewNeo4jGraphStore(neo4jDriver)
	svc := chat.NewService(vectorStore, graphStore, embedder, llmClient, s.logger)

	resp, err := svc.Chat(ctx, req.Question, chat.Config{
		SimilarityLimit: limit,
		SectionFilters:  req.Sections,
		TopicFilters:    req.Topics,
	})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("chat failed: %w", err))
		return
	}

	s.writeJSON(w, http.StatusOK, transformChatResponse(&resp))
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, http.MethodPost)
		return
	}

	var req clearRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	if !req.Confirm {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("confirm must be true to clear data"))
		return
	}

	ctx := r.Context()

	pgPool, err := database.NewPostgresPool(ctx, s.cfg.PostgresDSN)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("postgres connection: %w", err))
		return
	}
	defer pgPool.Close()

	if _, err := pgPool.Exec(ctx, "TRUNCATE rag_chunks, rag_documents"); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("truncate postgres tables: %w", err))
		return
	}
	s.logger.Println("cleared Postgres rag_documents and rag_chunks")

	neo4jDriver, err := database.NewNeo4jDriver(ctx, s.cfg.Neo4jURI, s.cfg.Neo4jUser, s.cfg.Neo4jPass)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("neo4j connection: %w", err))
		return
	}
	defer neo4jDriver.Close(ctx)

	session := neo4jDriver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	if err := purgeNeo4j(ctx, session); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("clear neo4j: %w", err))
		return
	}

	s.logger.Println("Neo4j documents and chunks cleared")
	s.logger.Println("RAG data removed")

	s.writeJSON(w, http.StatusOK, messageResponse{Message: "rag data cleared"})
}

func (s *Server) methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	s.writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed, use %s", allowed))
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Printf("encode response: %v", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	s.logger.Printf("api error (%d): %v", status, err)
	s.writeJSON(w, status, errorResponse{Error: err.Error()})
}

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}

	if dec.More() {
		return fmt.Errorf("request body must contain a single JSON object")
	}

	return nil
}

func transformChatResponse(resp *chat.Response) chatResponse {
	if resp == nil {
		return chatResponse{}
	}

	converted := chatResponse{Answer: resp.Answer}
	if len(resp.Sources) == 0 {
		return converted
	}

	sources := make([]chatSource, len(resp.Sources))
	for i, src := range resp.Sources {
		sources[i] = chatSource{
			DocumentID: src.DocumentID,
			Title:      src.Title,
			Path:       src.Path,
			Snippet:    src.Snippet,
			Score:      src.Score,
			Insight:    transformInsight(src.Insight),
		}
	}
	converted.Sources = sources
	return converted
}

func transformInsight(insight chat.DocumentInsight) chatDocumentInsight {
	sections := make([]chatSectionInfo, len(insight.Sections))
	for i, section := range insight.Sections {
		sections[i] = chatSectionInfo{
			Title: section.Title,
			Level: section.Level,
			Order: section.Order,
		}
	}

	related := make([]chatRelatedDocument, len(insight.RelatedDocuments))
	for i, doc := range insight.RelatedDocuments {
		related[i] = chatRelatedDocument{
			ID:         doc.ID,
			Title:      doc.Title,
			Path:       doc.Path,
			Weight:     doc.Weight,
			Similarity: doc.Similarity,
			Reason:     doc.Reason,
		}
	}

	return chatDocumentInsight{
		ChunkCount:       insight.ChunkCount,
		Folders:          append([]string(nil), insight.Folders...),
		Sections:         sections,
		Topics:           append([]string(nil), insight.Topics...),
		RelatedDocuments: related,
	}
}

func purgeNeo4j(ctx context.Context, session neo4j.SessionWithContext) error {
	queries := []string{
		"MATCH (d:Document) DETACH DELETE d",
		"MATCH (c:Chunk) DETACH DELETE c",
		"MATCH (f:Folder) DETACH DELETE f",
	}

	for _, query := range queries {
		result, err := session.Run(ctx, query, nil)
		if err != nil {
			return err
		}
		if _, err := result.Consume(ctx); err != nil {
			return err
		}
	}

	return nil
}
