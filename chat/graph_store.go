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
        RETURN d.id AS id, count(c) AS chunkCount
    `, map[string]any{"ids": docIDs})
	if err != nil {
		return nil, fmt.Errorf("run neo4j insights query: %w", err)
	}

	insights := make(map[string]DocumentInsight, len(docIDs))
	for result.Next(ctx) {
		record := result.Record()
		id, _ := record.Get("id")
		count, _ := record.Get("chunkCount")
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
		insights[docID] = DocumentInsight{ChunkCount: int(chunkCount)}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("neo4j insights result error: %w", err)
	}

	return insights, nil
}

var _ GraphStore = (*Neo4jGraphStore)(nil)
