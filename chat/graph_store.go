package chat

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type GraphStore interface {
	DocumentInsights(ctx context.Context, docIDs []string) (map[string]DocumentInsight, error)
}

type Neo4jGraphStore struct {
	driver neo4j.DriverWithContext
}

func NewNeo4jGraphStore(driver neo4j.DriverWithContext) *Neo4jGraphStore {
	return &Neo4jGraphStore{driver: driver}
}

func (s *Neo4jGraphStore) DocumentInsights(ctx context.Context, docIDs []string) (map[string]DocumentInsight, error) {
	if s.driver == nil {
		return nil, fmt.Errorf("neo4j driver is nil")
	}
	if len(docIDs) == 0 {
		return map[string]DocumentInsight{}, nil
	}

	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, `
        MATCH (d:Document)
        WHERE d.id IN $ids
        OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c:Chunk)
        OPTIONAL MATCH (d)-[:IN_FOLDER]->(folder:Folder)
        OPTIONAL MATCH (folder)<-[:IN_FOLDER]-(related:Document)
        WITH d,
             count(DISTINCT c) AS chunkCount,
             collect(DISTINCT folder.name) AS folders,
             collect(DISTINCT related) AS relatedNodes
        WITH d,
             chunkCount,
             [f IN folders WHERE f IS NOT NULL] AS folderNames,
             [r IN relatedNodes WHERE r IS NOT NULL AND r.id <> d.id | {id: r.id, title: r.title, path: r.path}] AS relatedDocs
        RETURN d.id AS id,
               chunkCount,
               folderNames AS folders,
               relatedDocs AS relatedDocuments
    `, map[string]any{"ids": docIDs})
	if err != nil {
		return nil, fmt.Errorf("run neo4j insights query: %w", err)
	}

	insights := make(map[string]DocumentInsight, len(docIDs))
	for result.Next(ctx) {
		record := result.Record()
		id, _ := record.Get("id")
		count, _ := record.Get("chunkCount")
		foldersVal, _ := record.Get("folders")
		relatedVal, _ := record.Get("relatedDocuments")
		docID, ok := id.(string)
		if !ok {
			continue
		}
		var chunkCount int64
		switch v := count.(type) {
		case int64:
			chunkCount = v
		case int32:
			chunkCount = int64(v)
		}
		folders := convertStringSlice(foldersVal)
		relatedDocs, err := convertRelated(relatedVal)
		if err != nil {
			return nil, fmt.Errorf("parse related documents: %w", err)
		}

		insights[docID] = DocumentInsight{
			ChunkCount:       int(chunkCount),
			Folders:          folders,
			RelatedDocuments: relatedDocs,
		}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("neo4j insights result error: %w", err)
	}

	return insights, nil
}

var _ GraphStore = (*Neo4jGraphStore)(nil)

func convertStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if v, ok := value.([]string); ok {
			return v
		}
		return nil
	}

	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

func convertRelated(value any) ([]RelatedDocument, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, nil
	}

	related := make([]RelatedDocument, 0, len(raw))
	for _, item := range raw {
		data, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := data["id"].(string)
		title, _ := data["title"].(string)
		path, _ := data["path"].(string)
		if id == "" {
			continue
		}
		related = append(related, RelatedDocument{ID: id, Title: title, Path: path})
	}

	return related, nil
}
