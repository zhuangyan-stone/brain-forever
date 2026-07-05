package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"BrainForever/infra/i18n"
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
	ChatSN     string    `db:"chat_sn"`    // 来源 chat SN (chat_sessions.sn)，全局唯一
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
	Kind     int       `db:"kind"`     // 字母分类: 1=A,2=B,3=C,4=D,5=E,6=F
	TraitID  int64     `db:"trait_id"` // 关联 traits.id（无外键约束）
	CreateAt time.Time `db:"create_at"`
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

	db, err := sqlx.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_open_vector_db_failed"), err)
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
		return fmt.Errorf("%s: %w", i18n.T("db_vec_not_loaded"), err)
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
			chat_sn    TEXT    NOT NULL DEFAULT '',
			create_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		-- Keywords extracted from traits
		CREATE TABLE IF NOT EXISTS keywords (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			word      TEXT    NOT NULL,
			kind      INTEGER NOT NULL,
			trait_id  INTEGER NOT NULL REFERENCES traits(id) ON DELETE CASCADE,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		-- Indexes for traits table
		CREATE INDEX IF NOT EXISTS idx_traits_category   ON traits(category);
		CREATE INDEX IF NOT EXISTS idx_traits_half_life  ON traits(half_life);
		CREATE INDEX IF NOT EXISTS idx_traits_create_at  ON traits(create_at);
		CREATE INDEX IF NOT EXISTS idx_traits_chat_sn    ON traits(chat_sn);

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
	`, s.dimension)

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Migration: add chat_sn column for existing databases (replacing old chat_id)
	s.db.Exec("ALTER TABLE traits ADD COLUMN chat_sn TEXT NOT NULL DEFAULT ''")

	// Migration: remove update_at from keywords table (keywords are immutable once created)
	s.db.Exec("DROP TRIGGER IF EXISTS trg_keywords_update_at")
	s.db.Exec("ALTER TABLE keywords DROP COLUMN update_at")

	return nil
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

	// Insert trait (including chat_sn)
	result, err := tx.Exec(
		"INSERT INTO traits(trait, category, confidence, half_life, chat_sn) VALUES(?, ?, ?, ?, ?)",
		trait.Trait, trait.Category, trait.Confidence, trait.HalfLife, trait.ChatSN,
	)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", i18n.T("db_insert_trait_failed"), err)
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
		return 0, fmt.Errorf("%s: %w", i18n.T("db_insert_trait_vector_failed"), err)
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
		return 0, fmt.Errorf("%s: %w", i18n.T("db_insert_keyword_failed"), err)
	}
	return result.LastInsertId()
}

// ============================================================
// SearchByVector -vector similarity search with category filter
// ============================================================

// SearchByVector performs vector similarity search (using sqlite-vec's HNSW index)
// with an optional category filter directly in SQL. If category is 0, no filtering is applied.
// Returns traits ordered by similarity (highest first), each with a Score field populated.
func (s *VectorStore) SearchByVector(query []float32, category int, topK int) ([]PersonalTrait, error) {
	// Serialize query vector
	queryJSON, _ := json.Marshal(query)

	// Build query -category filter is pushed into SQL WHERE for efficiency
	sqlQuery := "SELECT v.rowid, v.distance, " +
		"t.id, t.trait, t.category, t.confidence, t.half_life, " +
		"t.chat_sn, t.create_at, t.update_at\n" +
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
		return nil, fmt.Errorf("%s: %w", i18n.T("db_vector_search_failed"), err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var (
			rowid      int64
			distance   float64
			traitID    int64
			traitText  string
			traitCat   int64
			confidence int64
			halfLife   int64
			chatSN     string
			createAt   time.Time
			updateAt   time.Time
		)
		if err := rows.Scan(&rowid, &distance, &traitID, &traitText, &traitCat, &confidence, &halfLife, &chatSN, &createAt, &updateAt); err != nil {
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
			ChatSN:     chatSN,
			Score:      score,
			CreateAt:   createAt,
			UpdateAt:   updateAt,
		}

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
// word uses exact match; kind uses exact match.
// If kind is 0, no kind filtering is applied (match keyword only).
// Uses idx_keywords_word + idx_keywords_kind indexes for fast lookup.
// Returns distinct traits matching the criteria, ordered by trait ID descending.
func (s *VectorStore) SearchByKeyword(word string, kind int, limit int) ([]PersonalTrait, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []interface{}
	if kind > 0 {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word = ? AND k.kind = ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{word, kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word = ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{word, limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_keyword_search_failed"), err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// ============================================================
// SearchByKeywordFuzzy -fuzzy keyword-based trait search (LIKE %word%)
// ============================================================

// SearchByKeywordFuzzy finds traits by fuzzy matching keywords using LIKE '%word%'.
// kind uses exact match. This is a fallback when exact SearchByKeyword returns no results.
// If kind is 0, no kind filtering is applied (match keyword only).
// Uses idx_keywords_word + idx_keywords_kind indexes for fast lookup.
// Returns distinct traits matching the criteria, ordered by trait ID descending.
func (s *VectorStore) SearchByKeywordFuzzy(word string, kind int, limit int) ([]PersonalTrait, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []interface{}
	if kind > 0 {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word LIKE ? AND k.kind = ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{"%" + word + "%", kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word LIKE ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{"%" + word + "%", limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_fuzzy_keyword_search_failed"), err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
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
		return fmt.Errorf("%s: %w", i18n.T("db_delete_trait_vector_failed"), err)
	}

	// Delete keywords (explicit; ON DELETE CASCADE also handles it if FK is enabled)
	if _, err := tx.Exec("DELETE FROM keywords WHERE trait_id = ?", id); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_keywords_failed"), err)
	}

	// Delete the trait itself
	result, err := tx.Exec("DELETE FROM traits WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("db_delete_trait_failed"), err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("%s (id=%d)", i18n.T("db_trait_not_found"), id)
	}

	return tx.Commit()
}

// ============================================================
// Close
// ============================================================

// ============================================================
// ListTraitsByChat -list all traits for a specific chat
// ============================================================

// ListTraitsByChat returns all traits that belong to the given chat SN,
// ordered by create_at descending (newest first).
func (s *VectorStore) ListTraitsByChat(chatSN string) ([]PersonalTrait, error) {
	rows, err := s.db.Query(
		`SELECT id, trait, category, confidence, half_life, chat_sn, create_at, update_at
		 FROM traits
		 WHERE chat_sn = ?
		 ORDER BY create_at DESC`,
		chatSN,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_traits_by_chat_failed"), err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// ============================================================
// ListAllTraitsByCreateTime -list all traits ordered by create_at
// ============================================================

// ListAllTraitsByCreateTime returns all personal traits for the current user,
// ordered by create_at descending (newest first).
// This is used for portrait generation where all traits need to be
// sent to the LLM for higher-level abstraction.
func (s *VectorStore) ListAllTraitsByCreateTime() ([]PersonalTrait, error) {
	rows, err := s.db.Query(
		`SELECT id, trait, category, confidence, half_life, chat_sn, create_at, update_at
		 FROM traits
		 ORDER BY create_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("db_list_all_traits_failed"), err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// Close closes the database connection.
func (s *VectorStore) Close() error {
	return s.db.Close()
}
