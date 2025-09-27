package unit

import (
	"context"
	"testing"

	"github.com/fabfab/go-agent/database"
)

func TestEnsureRAGSchemaRejectsInvalidDimension(t *testing.T) {
	err := database.EnsureRAGSchema(context.Background(), nil, 0)
	if err == nil {
		t.Fatal("expected error when dimension is not positive")
	}
}
