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

// PersonalTrait represents a personal trait stored in the traits table.
// Score is only populated during search results, not persisted to DB.
type PersonalTrait struct {
	ID           int64     `db:"id"`
	Trait        string    `db:"trait"`         // trait text
	Category     int       `db:"category"`      // category 1~14
	Confidence   int       `db:"confidence"`    // confidence (integer)
	HalfLife     int       `db:"half_life"`     // half-life: 1=short,2=medium,3=long,4=permanent
	PrivacyLevel int       `db:"privacy_level"` // 0=private, 1=protected, 2=public
	ChatSN       string    `db:"chat_sn"`       // source chat SN, globally unique
	Score        float64   `db:"-"`             // similarity score (search only, not persisted)
	CreateAt     time.Time `db:"create_at"`
	UpdateAt     time.Time `db:"update_at"`
}

// ============================================================
// TraitKeyword -keyword extracted from a personal trait
// ============================================================

// TraitKeyword represents a keyword extracted from a specific trait.
// Kind corresponds to A-F letter categories mapped as 1-6.
type TraitKeyword struct {
	ID       int64     `db:"id"`
	Word     string    `db:"word"`     // keyword text
	Kind     int       `db:"kind"`     // letter category: 1=A,2=B,3=C,4=D,5=E,6=F
	TraitID  int64     `db:"trait_id"` // references traits.id (no FK)
	CreateAt time.Time `db:"create_at"`
}

// ============================================================
// BrainStore (based on sqlite-vec, stores personal traits)
// ============================================================

// BrainStore manages personal trait storage with vector similarity search
// (based on sqlite-vec) and keyword-based retrieval.
type BrainStore struct {
	db        *sqlx.DB
	dimension int
	logger    zylog.Logger
}

// OpenBrainStore opens an existing brain database WITHOUT running DDL/migrations.
// Use this for on-demand open/close patterns where the schema is already created.
// sqlite_vec.Auto() is called once globally; the caller should ensure it is called
// at process startup before using any BrainStore.
func OpenBrainStore(dbPath string, dimension int, logger zylog.Logger) (*BrainStore, error) {
	db, err := sqlx.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("failed to open vector database: %w", err)
	}
	return &BrainStore{db: db, dimension: dimension, logger: logger}, nil
}

// EnsureSchema ensures the brain store schema exists (idempotent).
func (s *BrainStore) EnsureSchema() error {
	return s.initSchema()
}

// NewBrainStore creates a new BrainStore with full schema initialization.
// dimension specifies the vector dimension used for the HNSW index,
// which must match the embedding model's output dimension.
func NewBrainStore(dbPath string, dimension int, logger zylog.Logger) (*BrainStore, error) {
	sqlite_vec.Auto()

	store, err := OpenBrainStore(dbPath, dimension, logger)
	if err != nil {
		return nil, err
	}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

// initSchema initializes the traits, keywords, and vector index tables.
func (s *BrainStore) initSchema() error {
	var vecVersion string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&vecVersion); err != nil {
		return fmt.Errorf("sqlite-vec not loaded correctly: %w", err)
	}
	s.logger.Infof("sqlite-vec version: %s", vecVersion)

	schema := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS trait_vectors
		USING vec0(
			embedding float[%d] distance_metric=cosine
		);

		CREATE TABLE IF NOT EXISTS traits (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			trait          TEXT    NOT NULL,
			category       INTEGER NOT NULL,
			confidence     INTEGER NOT NULL,
			half_life      INTEGER NOT NULL,
			privacy_level  INTEGER NOT NULL DEFAULT 0,
			chat_sn        TEXT    NOT NULL DEFAULT '',
			create_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			update_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS keywords (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			word      TEXT    NOT NULL,
			kind      INTEGER NOT NULL,
			trait_id  INTEGER NOT NULL REFERENCES traits(id) ON DELETE CASCADE,
			create_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_traits_category   ON traits(category);
		CREATE INDEX IF NOT EXISTS idx_traits_half_life  ON traits(half_life);
		CREATE INDEX IF NOT EXISTS idx_traits_create_at  ON traits(create_at);
		CREATE INDEX IF NOT EXISTS idx_traits_chat_sn    ON traits(chat_sn);

		CREATE INDEX IF NOT EXISTS idx_keywords_trait_id ON keywords(trait_id);
		CREATE INDEX IF NOT EXISTS idx_keywords_word     ON keywords(word);
		CREATE INDEX IF NOT EXISTS idx_keywords_kind     ON keywords(kind);
		CREATE INDEX IF NOT EXISTS idx_keywords_trait_kind ON keywords(trait_id, kind);

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

	s.db.Exec("ALTER TABLE traits ADD COLUMN chat_sn TEXT NOT NULL DEFAULT ''")
	s.db.Exec("DROP TRIGGER IF EXISTS trg_keywords_update_at")
	s.db.Exec("ALTER TABLE keywords DROP COLUMN update_at")

	return nil
}

// AddTrait inserts a personal trait into the traits table and its vector embedding
// into the vec0 virtual table. The embedding must be pre-computed externally.
// Returns the new trait ID.
func (s *BrainStore) AddTrait(ctx context.Context, trait *PersonalTrait, embedding []float32) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO traits(trait, category, confidence, half_life, privacy_level, chat_sn) VALUES(?, ?, ?, ?, ?, ?)",
		trait.Trait, trait.Category, trait.Confidence, trait.HalfLife, trait.PrivacyLevel, trait.ChatSN,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert trait: %w", err)
	}

	traitID, _ := result.LastInsertId()

	vecJSON, _ := json.Marshal(embedding)

	_, err = tx.Exec(
		"INSERT INTO trait_vectors(rowid, embedding) VALUES(?, ?)",
		traitID, string(vecJSON),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert trait vector: %w", err)
	}

	return traitID, tx.Commit()
}

// AddKeyword inserts a keyword associated with a trait.
func (s *BrainStore) AddKeyword(kw *TraitKeyword) (int64, error) {
	result, err := s.db.Exec(
		"INSERT INTO keywords(word, kind, trait_id) VALUES(?, ?, ?)",
		kw.Word, kw.Kind, kw.TraitID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert keyword: %w", err)
	}
	return result.LastInsertId()
}

// SearchByVector performs vector similarity search with an optional category filter.
func (s *BrainStore) SearchByVector(query []float32, category int, topK int) ([]PersonalTrait, error) {
	queryJSON, _ := json.Marshal(query)

	sqlQuery := "SELECT v.rowid, v.distance, " +
		"t.id, t.trait, t.category, t.confidence, t.half_life, " +
		"t.privacy_level, t.chat_sn, t.create_at, t.update_at\n" +
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
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var (
			rowid, traitID                           int64
			distance, traitCat, confidence, halfLife int64
			privacyLevel                             int
			traitText, chatSN                        string
			createAt, updateAt                       time.Time
		)
		if err := rows.Scan(&rowid, &distance, &traitID, &traitText, &traitCat, &confidence, &halfLife, &privacyLevel, &chatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}

		score := 1.0 - float64(distance)

		pt := PersonalTrait{
			ID:           traitID,
			Trait:        traitText,
			Category:     int(traitCat),
			Confidence:   int(confidence),
			HalfLife:     int(halfLife),
			ChatSN:       chatSN,
			Score:        score,
			PrivacyLevel: privacyLevel,
			CreateAt:     createAt,
			UpdateAt:     updateAt,
		}
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// SearchByKeyword finds traits by matching keywords.
func (s *BrainStore) SearchByKeyword(word string, kind int, limit int) ([]PersonalTrait, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []interface{}
	if kind > 0 {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word = ? AND k.kind = ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{word, kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word = ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{word, limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("keyword search failed: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
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

// SearchByKeywordFuzzy finds traits by fuzzy matching keywords using LIKE.
func (s *BrainStore) SearchByKeywordFuzzy(word string, kind int, limit int) ([]PersonalTrait, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []interface{}
	if kind > 0 {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word LIKE ? AND k.kind = ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{"%" + word + "%", kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE k.word LIKE ?
		 ORDER BY t.id DESC
		 LIMIT ?`
		args = []interface{}{"%" + word + "%", limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("fuzzy keyword search failed: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
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

// Delete removes a personal trait by ID, along with its vector embedding and keywords.
func (s *BrainStore) Delete(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM trait_vectors WHERE rowid = ?", id); err != nil {
		return fmt.Errorf("failed to delete trait vector: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM keywords WHERE trait_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete keywords: %w", err)
	}

	result, err := tx.Exec("DELETE FROM traits WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete trait: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("trait not found (id=%d)", id)
	}

	return tx.Commit()
}

// DeleteByChatSN deletes all traits for a chat SN, along with vectors and keywords.
func (s *BrainStore) DeleteByChatSN(chatSN string) (int, error) {
	if chatSN == "" {
		return 0, fmt.Errorf("empty chat SN")
	}

	var traitIDs []int64
	if err := s.db.Select(&traitIDs, "SELECT id FROM traits WHERE chat_sn = ?", chatSN); err != nil {
		return 0, fmt.Errorf("failed to list traits by chat: %w", err)
	}

	if len(traitIDs) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for _, id := range traitIDs {
		if _, err := tx.Exec("DELETE FROM trait_vectors WHERE rowid = ?", id); err != nil {
			return 0, fmt.Errorf("failed to delete trait vector (rowid=%d): %w", id, err)
		}
	}

	if _, err := tx.Exec("DELETE FROM keywords WHERE trait_id IN (SELECT id FROM traits WHERE chat_sn = ?)", chatSN); err != nil {
		return 0, fmt.Errorf("failed to delete keywords: %w", err)
	}

	result, err := tx.Exec("DELETE FROM traits WHERE chat_sn = ?", chatSN)
	if err != nil {
		return 0, fmt.Errorf("failed to delete trait: %w", err)
	}

	_ = result

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return len(traitIDs), nil
}

// DeleteTraitsByChatSNs deletes all traits for multiple chat SNs in batch.
func (s *BrainStore) DeleteTraitsByChatSNs(chatSNs []string) (int, error) {
	total := 0
	for _, sn := range chatSNs {
		n, err := s.DeleteByChatSN(sn)
		if err != nil {
			return total, fmt.Errorf("failed to delete traits for chat_sn=%s: %w", sn, err)
		}
		total += n
	}
	return total, nil
}

// ListTraitsByChat returns all traits for a given chat SN, ordered by create_at desc.
func (s *BrainStore) ListTraitsByChat(chatSN string) ([]PersonalTrait, error) {
	rows, err := s.db.Query(
		`SELECT id, trait, category, confidence, half_life, privacy_level, chat_sn, create_at, update_at
		 FROM traits
		 WHERE chat_sn = ?
		 ORDER BY create_at DESC`,
		chatSN,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list traits by chat: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
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

// ListAllTraitsByCreateTime returns all personal traits ordered by create_at desc.
func (s *BrainStore) ListAllTraitsByCreateTime() ([]PersonalTrait, error) {
	rows, err := s.db.Query(
		`SELECT id, trait, category, confidence, half_life, privacy_level, chat_sn, create_at, update_at
		 FROM traits
		 ORDER BY create_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list all traits: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife, &pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
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
func (s *BrainStore) Close() error {
	return s.db.Close()
}
