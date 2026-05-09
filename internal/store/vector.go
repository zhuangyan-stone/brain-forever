package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"BrainOnline/infra/embedder"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// ============================================================
// Document struct
// ============================================================

// Document represents a document
type Document struct {
	ID        int64  `db:"id"`
	Title     string `db:"title"`
	Content   string `db:"content"`
	CreatedAt string `db:"created_at"`
}

// ============================================================
// VectorStore (based on sqlite-vec)
// ============================================================

// VectorStore manages vector storage (based on sqlite-vec)
type VectorStore struct {
	db        *sqlx.DB
	dimension int
	embedder  embedder.Embedder
}

// NewVectorStore creates a new VectorStore
// dimension is obtained from embedder.Dimension() to ensure consistency with Embedder output
func NewVectorStore(dbPath string, e embedder.Embedder) (*VectorStore, error) {
	// Enable sqlite-vec (global effect)
	sqlite_vec.Auto()

	db, err := sqlx.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database. %w", err)
	}

	dimension := e.Dimension()
	store := &VectorStore{db: db, dimension: dimension, embedder: e}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

// initSchema initializes the table schema
func (s *VectorStore) initSchema() error {
	// Verify sqlite-vec is loaded
	var vecVersion string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&vecVersion); err != nil {
		return fmt.Errorf("sqlite-vec not loaded correctly. %w", err)
	}
	fmt.Printf("✓ sqlite-vec version: %s\n", vecVersion)

	schema := fmt.Sprintf(`
		-- vec0 virtual table: HNSW vector index
		CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_vectors
		USING vec0(
			embedding float[%d] distance_metric=cosine
		);

		-- Document metadata table
		CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`, s.dimension)

	_, err := s.db.Exec(schema)
	return err
}

// AddDocument adds a document (automatically calls Embedder to generate vector)
func (s *VectorStore) AddDocument(ctx context.Context, title, content string) (int64, error) {
	// Concatenate title + content and generate embedding
	embedding, err := s.embedder.Embed(ctx, title+" "+content)
	if err != nil {
		return 0, fmt.Errorf("failed to generate embedding. %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Insert document
	result, err := tx.Exec(
		"INSERT INTO documents(title, content) VALUES(?, ?)",
		title, content,
	)
	if err != nil {
		return 0, err
	}

	docID, _ := result.LastInsertId()

	// Serialize vector as JSON (sqlite-vec accepts JSON format vectors)
	vecJSON, _ := json.Marshal(embedding)

	// Insert vector into vec0 virtual table
	_, err = tx.Exec(
		"INSERT INTO knowledge_vectors(rowid, embedding) VALUES(?, ?)",
		docID, string(vecJSON),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert vector. %w", err)
	}

	return docID, tx.Commit()
}

// SearchResult represents a search result
type SearchResult struct {
	Document Document
	Score    float64
}

// Search performs vector similarity search (using sqlite-vec's HNSW index)
func (s *VectorStore) Search(query []float32, topK int) ([]SearchResult, error) {
	// Serialize query vector
	queryJSON, _ := json.Marshal(query)

	// Use sqlite-vec's KNN search (note the k=? syntax)
	rows, err := s.db.Query("SELECT "+
		"v.rowid, "+
		"v.distance, d.id, "+
		"d.title, "+
		"d.content, "+
		"d.created_at\n"+
		"FROM knowledge_vectors v "+
		"LEFT JOIN documents d ON d.id = v.rowid "+
		"WHERE v.embedding MATCH ? AND k=? "+
		"ORDER BY v.distance", string(queryJSON), topK)
	if err != nil {
		return nil, fmt.Errorf("vector search failed. %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var (
			rowid     int64
			distance  float64
			docID     sql.NullInt64
			title     sql.NullString
			content   sql.NullString
			createdAt sql.NullString
		)
		if err := rows.Scan(&rowid, &distance, &docID, &title, &content, &createdAt); err != nil {
			return nil, err
		}

		// Convert cosine distance to similarity (distance = 1 - similarity)
		score := 1.0 - distance

		results = append(results, SearchResult{
			Document: Document{
				ID:        docID.Int64,
				Title:     title.String,
				Content:   content.String,
				CreatedAt: createdAt.String,
			},
			Score: score,
		})
	}

	return results, rows.Err()
}

// SearchByText performs text semantic search: inputs a string, auto-embeds then retrieves
func (s *VectorStore) SearchByText(ctx context.Context, queryText string, topK int) ([]SearchResult, error) {
	queryVec, err := s.embedder.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query text. %w", err)
	}
	return s.Search(queryVec, topK)
}

// Close closes the database
func (s *VectorStore) Close() error {
	return s.db.Close()
}
