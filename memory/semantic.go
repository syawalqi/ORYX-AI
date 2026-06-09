// Package memory provides semantic (vector) memory for ORYX.
// Uses chromem-go for embedded vector storage with zero external dependencies.
package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/philippgille/chromem-go"
)

// SemanticDB wraps chromem-go for agent memory.
type SemanticDB struct {
	db         *chromem.DB
	collection *chromem.Collection
}

// NewSemanticDB creates an in-memory semantic database.
// If path is non-empty, it persists to disk.
func NewSemanticDB(path string) (*SemanticDB, error) {
	db := chromem.NewDB()

	var collection *chromem.Collection
	var err error

	if path != "" {
		collection, err = db.CreateCollection("oryx_memory", nil, chromem.NewEmbeddingFuncOllama("nomic-embed-text", ""))
		if err != nil {
			return nil, fmt.Errorf("create collection: %w", err)
		}
	} else {
		collection, err = db.CreateCollection("oryx_memory", nil, nil)
		if err != nil {
			return nil, fmt.Errorf("create collection: %w", err)
		}
	}

	return &SemanticDB{
		db:         db,
		collection: collection,
	}, nil
}

// StoreFact saves a fact to semantic memory.
func (s *SemanticDB) StoreFact(ctx context.Context, content string) error {
	if s.collection == nil {
		return nil
	}
	docID := fmt.Sprintf("fact-%d", len(content))
	return s.collection.AddDocument(ctx, chromem.Document{
		ID:      docID,
		Content: content,
	})
}

// Query returns the most relevant memories for the given query.
func (s *SemanticDB) Query(ctx context.Context, query string, limit int) ([]string, error) {
	if s.collection == nil {
		return nil, nil
	}

	results, err := s.collection.Query(ctx, query, limit, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	var texts []string
	for _, r := range results {
		texts = append(texts, r.Content)
	}
	return texts, nil
}

// BuildContext retrieves relevant memories and formats them as context.
func (s *SemanticDB) BuildContext(ctx context.Context, query string, limit int) string {
	if s.collection == nil {
		return ""
	}

	results, err := s.Query(ctx, query, limit)
	if err != nil || len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n## Relevant Context\n")
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
	}
	return b.String()
}
