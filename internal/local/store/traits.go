package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"BrainForever/infra/zylog"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// ============================================================
// PersonalTrait -personal trait (feature) entity
// ============================================================

// PersonalTrait represents a single personal trait (特征条目) stored in the traits table.
// Score is only populated during search results and not persisted to the database.
type PersonalTrait struct {
	ID         int64     `db:"id"`
	Trait      string    `db:"trait"`      // 特征条目文本（原 item，建议改为trait）
	Category   int       `db:"category"`   // 特征分类 1~14
	Confidence int       `db:"confidence"` // 置信度（整数）
	HalfLife   int       `db:"half_life"`  // 半衰期 1-短期,2-中期,3-长期,4-永久
	Score      float64   `db:"-"`          // 相似度分数（仅搜索结果使用，不持久化）
	CreateAt   time.Time `db:"create_at"`
	UpdateAt   time.Time `db:"update_at"`
}

// ============================================================
// TraitKeyword -keyword extracted from a personal trait
// ============================================================

// TraitKeyword represents a keyword extracted from a specific trait.
// Kind corresponds to A-F letter categories mapped as 1-6.
type TraitKeyword struct {
	ID       int64     `db:"id"`
	Word     string    `db:"word"`     // 关键词文期
	Kind     int       `db:"kind"`     // 字母分类: 1=A,2=B,3=C,4=D,5=E,6=F（原 category，建议改为kind）
	TraitID  int64     `db:"trait_id"` // 关联 traits.id（无外键约束）
	CreateAt time.Time `db:"create_at"`
	UpdateAt time.Time `db:"update_at"`
}

// ============================================================
// VectorStore (based on sqlite-vec)
// ============================================================

// VectorStore manages personal trait storage with vector similarity search
// (based on sqlite-vec) and keyword-based retrieval.
type VectorStore struct {
	db        *sqlx.DB
	dimension int
	logger    zylog.Logger
}

// NewVectorStore creates a new VectorStore.
// dimension specifies the vector dimension used for the HNSW index,
// which must match the embedding model's output dimension.
func NewVectorStore(dbPath string, dimension int, logger zylog.Logger) (*VectorStore, error) {
	// Enable sqlite-vec (global effect)
	sqlite_vec.Auto()

	db, err := sqlx.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database. %w", err)
	}

	store := &VectorStore{db: db, dimension: dimension, logger: logger}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

// initSchema initializes the traits, keywords, and vector index tables.
func (s *VectorStore) initSchema() error {
	// Verify sqlite-vec is loaded
	var vecVersion string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&vecVersion); err != nil {
		return fmt.Errorf("sqlite-vec not loaded correctly. %w", err)
	}
	s.logger.Infof("✓sqlite-vec version: %s", vecVersion)

	schema := fmt.Sprintf(`
		-- vec0 virtual table: HNSW vector index for traits
		CREATE VIRTUAL TABLE IF NOT EXISTS trait_vectors
		USING vec0(
			embedding float[%d] distance_metric=cosine
		);

		-- Personal traits table
		CREATE TABLE IF NOT EXISTS traits (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			trait      TEXT    NOT NULL,
			category   INTEGER NOT NULL,
			confidence INTEGER NOT NULL,
			half_life  INTEGER NOT NULL,
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		-- Keywords extracted from traits
		CREATE TABLE IF NOT EXISTS keywords (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			word      TEXT    NOT NULL,
			kind      INTEGER NOT NULL,
			trait_id  INTEGER NOT NULL REFERENCES traits(id) ON DELETE CASCADE,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		-- Indexes for traits table
		CREATE INDEX IF NOT EXISTS idx_traits_category   ON traits(category);
		CREATE INDEX IF NOT EXISTS idx_traits_half_life  ON traits(half_life);
		CREATE INDEX IF NOT EXISTS idx_traits_create_at  ON traits(create_at);

		-- Indexes for keywords table
		CREATE INDEX IF NOT EXISTS idx_keywords_trait_id ON keywords(trait_id);
		CREATE INDEX IF NOT EXISTS idx_keywords_word     ON keywords(word);
		CREATE INDEX IF NOT EXISTS idx_keywords_kind     ON keywords(kind);
		CREATE INDEX IF NOT EXISTS idx_keywords_trait_kind ON keywords(trait_id, kind);

		-- Trigger: auto-update update_at on traits
		CREATE TRIGGER IF NOT EXISTS trg_traits_update_at
			BEFORE UPDATE ON traits
			FOR EACH ROW
		BEGIN
			UPDATE traits SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;

		-- Trigger: auto-update update_at on keywords
		CREATE TRIGGER IF NOT EXISTS trg_keywords_update_at
			BEFORE UPDATE ON keywords
			FOR EACH ROW
		BEGIN
			UPDATE keywords SET update_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;
	`, s.dimension)

	_, err := s.db.Exec(schema)
	return err
}

// ============================================================
// AddTrait -insert a trait and its vector embedding
// ============================================================

// AddTrait inserts a personal trait into the traits table and its vector embedding
// into the vec0 virtual table. The embedding must be pre-computed externally.
// Returns the new trait ID.
func (s *VectorStore) AddTrait(ctx context.Context, trait *PersonalTrait, embedding []float32) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Insert trait
	result, err := tx.Exec(
		"INSERT INTO traits(trait, category, confidence, half_life) VALUES(?, ?, ?, ?)",
		trait.Trait, trait.Category, trait.Confidence, trait.HalfLife,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert trait. %w", err)
	}

	traitID, _ := result.LastInsertId()

	// Serialize vector as JSON (sqlite-vec accepts JSON format vectors)
	vecJSON, _ := json.Marshal(embedding)

	// Insert vector into trait_vectors virtual table
	_, err = tx.Exec(
		"INSERT INTO trait_vectors(rowid, embedding) VALUES(?, ?)",
		traitID, string(vecJSON),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert trait vector. %w", err)
	}

	return traitID, tx.Commit()
}

// ============================================================
// AddKeyword -insert a keyword for a trait
// ============================================================

// AddKeyword inserts a keyword associated with a trait.
func (s *VectorStore) AddKeyword(kw *TraitKeyword) (int64, error) {
	result, err := s.db.Exec(
		"INSERT INTO keywords(word, kind, trait_id) VALUES(?, ?, ?)",
		kw.Word, kw.Kind, kw.TraitID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert keyword. %w", err)
	}
	return result.LastInsertId()
}

// ============================================================
// Search -vector similarity search with category filter
// ============================================================

// Search performs vector similarity search (using sqlite-vec's HNSW index)
// with an optional category filter directly in SQL. If category is 0, no filtering is applied.
// Returns traits ordered by similarity (highest first), each with a Score field populated.
func (s *VectorStore) Search(query []float32, category int, topK int) ([]PersonalTrait, error) {
	// Serialize query vector
	queryJSON, _ := json.Marshal(query)

	// Build query -category filter is pushed into SQL WHERE for efficiency
	sqlQuery := "SELECT v.rowid, v.distance, " +
		"t.id, t.trait, t.category, t.confidence, t.half_life, " +
		"t.create_at, t.update_at\n" +
		"FROM trait_vectors v " +
		"JOIN traits t ON t.id = v.rowid " +
		"WHERE v.embedding MATCH ? AND k=?"
	args := []interface{}{string(queryJSON), topK}

	if category > 0 {
		sqlQuery += " AND t.category = ?"
		args = append(args, category)
	}

	sqlQuery += " ORDER BY v.distance"

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search failed. %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var (
			rowid       int64
			distance    float64
			traitID     int64
			traitText   string
			traitCat    int64
			confidence  int64
			halfLife    int64
			createAtStr string
			updateAtStr string
		)
		if err := rows.Scan(&rowid, &distance, &traitID, &traitText, &traitCat, &confidence, &halfLife, &createAtStr, &updateAtStr); err != nil {
			return nil, err
		}

		// Convert cosine distance to similarity (distance = 1 - similarity)
		score := 1.0 - distance

		pt := PersonalTrait{
			ID:         traitID,
			Trait:      traitText,
			Category:   int(traitCat),
			Confidence: int(confidence),
			HalfLife:   int(halfLife),
			Score:      score,
		}

		// Parse timestamps
		pt.CreateAt, _ = time.Parse("2006-01-02 15:04:05", createAtStr)
		pt.UpdateAt, _ = time.Parse("2006-01-02 15:04:05", updateAtStr)

		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// ============================================================
// SearchByKeyword -keyword-based trait search (no vector matching)
// ============================================================

// SearchByKeyword finds traits by matching keywords.
// word uses LIKE fuzzy matching; kind uses exact match.
// Both word and kind are required to be non-zero (guaranteed by caller).
// Returns distinct traits matching the criteria, ordered by trait ID descending.
func (s *VectorStore) SearchByKeyword(word string, kind int, limit int) ([]PersonalTrait, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(
		`SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word LIKE ? AND k.kind = ?
		 ORDER BY t.id DESC
		 LIMIT ?`,
		"%"+word+"%", kind, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("keyword search failed. %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAtStr, updateAtStr string
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &createAtStr, &updateAtStr); err != nil {
			return nil, err
		}
		pt.CreateAt, _ = time.Parse("2006-01-02 15:04:05", createAtStr)
		pt.UpdateAt, _ = time.Parse("2006-01-02 15:04:05", updateAtStr)
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// ============================================================
// Delete -delete a trait, its vector embedding, and all keywords
// ============================================================

// Delete removes a personal trait by ID, along with its vector embedding
// (from trait_vectors) and all associated keywords.
// If the trait has a foreign key constraint (ON DELETE CASCADE) on keywords,
// keywords are deleted automatically; otherwise they are deleted explicitly.
// All operations run in a single transaction.
func (s *VectorStore) Delete(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete from vec0 virtual table (no FK support, must be explicit)
	if _, err := tx.Exec("DELETE FROM trait_vectors WHERE rowid = ?", id); err != nil {
		return fmt.Errorf("failed to delete trait vector. %w", err)
	}

	// Delete keywords (explicit; ON DELETE CASCADE also handles it if FK is enabled)
	if _, err := tx.Exec("DELETE FROM keywords WHERE trait_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete keywords. %w", err)
	}

	// Delete the trait itself
	result, err := tx.Exec("DELETE FROM traits WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete trait. %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("trait not found: id=%d", id)
	}

	return tx.Commit()
}

// ============================================================
// Close
// ============================================================

// Close closes the database connection.
func (s *VectorStore) Close() error {
	return s.db.Close()
}
