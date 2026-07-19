package store

import (
	"context"
	"fmt"
	"time"

	"BrainForever/infra/zylog"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/pgvector/pgvector-go"
)

// ============================================================
// PersonalTrait - personal trait (feature) entity
// ============================================================

// PersonalTrait represents a personal trait stored in the traits table.
type PersonalTrait struct {
	ID           int64     `db:"id"`
	Trait        string    `db:"trait"`
	Category     int       `db:"category"`
	Confidence   int       `db:"confidence"`
	HalfLife     int       `db:"half_life"`
	PrivacyLevel int       `db:"privacy_level"`
	ChatID       int64     `db:"chat_id"`
	Score        float64   `db:"-"` // similarity score (search only)
	CreateAt     time.Time `db:"create_at"`
	UpdateAt     time.Time `db:"update_at"`
}

// ============================================================
// TraitKeyword
// ============================================================

type TraitKeyword struct {
	ID       int64     `db:"id"`
	Word     string    `db:"word"`
	Kind     int       `db:"kind"`
	TraitID  int64     `db:"trait_id"`
	CreateAt time.Time `db:"create_at"`
}

// ============================================================
// BrainStore (based on pgvector, stores personal traits)
// ============================================================

// BrainStore manages personal trait storage with vector similarity search
// (based on pgvector) and keyword-based retrieval.
type BrainStore struct {
	logger zylog.Logger
}

// NewBrainStore creates a new BrainStore.
func NewBrainStore(logger zylog.Logger) *BrainStore {
	return &BrainStore{
		logger: zylog.WrapWithSubject(logger, "store-brain"),
	}
}

// db returns the global PostgreSQL connection.
func (s *BrainStore) db() *sqlx.DB {
	return ThePGDB()
}

// TraitInsertion holds all data needed to insert a single trait.
type TraitInsertion struct {
	Trait    PersonalTrait
	Vector   []float32
	Keywords []TraitKeyword
	UserID   int64 // required for data isolation
}

// AddKeyword inserts a keyword associated with a trait.
func (s *BrainStore) AddKeyword(kw *TraitKeyword) (int64, error) {
	sqlStr := "INSERT INTO keywords(word, kind, trait_id) VALUES($1, $2, $3) RETURNING id"
	var id int64
	err := s.db().QueryRow(sqlStr, kw.Word, kw.Kind, kw.TraitID).Scan(&id)
	if err != nil {
		s.logger.Errorf("SQL [%s]:\n%v", sqlStr, err)
		return 0, fmt.Errorf("failed to insert keyword. %w", err)
	}
	return id, nil
}

// SearchByVector performs vector similarity search with optional category filter.
// userID is required for data isolation.
func (s *BrainStore) SearchByVector(userID int64, query []float32, category int, topK int) ([]PersonalTrait, error) {
	pgVec := pgvector.NewVector(query)

	sqlQuery := `SELECT v.trait_id, v.embedding <=> $1 AS distance,
		t.id, t.trait, t.category, t.confidence, t.half_life,
		t.privacy_level, t.chat_id, t.create_at, t.update_at
		FROM trait_vectors v
		JOIN traits t ON t.id = v.trait_id
		WHERE t.user_id = $2`
	args := []interface{}{pgVec, userID}

	if category > 0 {
		sqlQuery += " AND t.category = $3"
		args = append(args, category)
		sqlQuery += " ORDER BY distance LIMIT $4"
	} else {
		sqlQuery += " ORDER BY distance LIMIT $3"
	}
	args = append(args, topK)

	rows, err := s.db().Query(sqlQuery, args...)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlQuery, userID, err)
		return nil, fmt.Errorf("vector search failed. %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var (
			traitID                        int64
			distance                       float64
			traitCat, confidence, halfLife int64
			privacyLevel                   int
			chatID                         int64
			traitText                      string
			createAt, updateAt             time.Time
		)
		if err := rows.Scan(&traitID, &distance, &traitID, &traitText, &traitCat,
			&confidence, &halfLife, &privacyLevel, &chatID, &createAt, &updateAt); err != nil {
			return nil, err
		}

		score := 1.0 - distance

		pt := PersonalTrait{
			ID:           traitID,
			Trait:        traitText,
			Category:     int(traitCat),
			Confidence:   int(confidence),
			HalfLife:     int(halfLife),
			ChatID:       chatID,
			Score:        score,
			PrivacyLevel: privacyLevel,
			CreateAt:     createAt,
			UpdateAt:     updateAt,
		}
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error. %v", err)
		return nil, err
	}

	return results, nil
}

// SearchByKeyword finds traits by matching keywords for a user.
func (s *BrainStore) SearchByKeyword(userID int64, word string, kind int, limit int) ([]PersonalTrait, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []interface{}
	if kind > 0 {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_id, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE t.user_id = $1 AND k.word = $2 AND k.kind = $3
		 ORDER BY t.id DESC
		 LIMIT $4`
		args = []interface{}{userID, word, kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_id, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE t.user_id = $1 AND k.word = $2
		 ORDER BY t.id DESC
		 LIMIT $3`
		args = []interface{}{userID, word, limit}
	}

	rows, err := s.db().Query(query, args...)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d word=%s]:\n%v", query, userID, word, err)
		return nil, fmt.Errorf("keyword search failed. %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatID, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error. %v", err)
		return nil, err
	}

	return results, nil
}

// SearchByKeywordFuzzy finds traits by fuzzy matching keywords using LIKE.
func (s *BrainStore) SearchByKeywordFuzzy(userID int64, word string, kind int, limit int) ([]PersonalTrait, error) {
	if limit <= 0 {
		limit = 20
	}

	var query string
	var args []interface{}
	if kind > 0 {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_id, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE t.user_id = $1 AND k.word LIKE $2 AND k.kind = $3
		 ORDER BY t.id DESC
		 LIMIT $4`
		args = []interface{}{userID, "%" + word + "%", kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_id, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE t.user_id = $1 AND k.word LIKE $2
		 ORDER BY t.id DESC
		 LIMIT $3`
		args = []interface{}{userID, "%" + word + "%", limit}
	}

	rows, err := s.db().Query(query, args...)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d word=%s]:\n%v", query, userID, word, err)
		return nil, fmt.Errorf("fuzzy keyword search failed. %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatID, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error. %v", err)
		return nil, err
	}

	return results, nil
}

// Delete removes a personal trait by ID.
func (s *BrainStore) Delete(id int64) error {
	// CASCADE will handle trait_vectors and keywords
	sqlStr := "DELETE FROM traits WHERE id = $1"
	result, err := s.db().Exec(sqlStr, id)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", sqlStr, id, err)
		return fmt.Errorf("failed to delete trait. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("trait not found (id=%d)", id)
	}
	return nil
}

// DeleteByChatID deletes all traits for a chat ID.
func (s *BrainStore) DeleteByChatID(chatID int64) (int, error) {
	if chatID == 0 {
		return 0, fmt.Errorf("empty chat ID")
	}

	sqlStr := "DELETE FROM traits WHERE chat_id = $1"
	result, err := s.db().Exec(sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return 0, fmt.Errorf("failed to delete traits by chat ID. %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// DeleteTraitsByChatIDs deletes all traits for multiple chat IDs in batch.
func (s *BrainStore) DeleteTraitsByChatIDs(chatIDs []int64) (int, error) {
	total := 0
	for _, id := range chatIDs {
		n, err := s.DeleteByChatID(id)
		if err != nil {
			return total, fmt.Errorf("failed to delete traits for chat_id=%d. %w", id, err)
		}
		total += n
	}
	return total, nil
}

// ListTraitsByChat returns all traits for a given chat ID.
func (s *BrainStore) ListTraitsByChat(chatID int64) ([]PersonalTrait, error) {
	sqlStr := `SELECT id, trait, category, confidence, half_life, privacy_level, chat_id, create_at, update_at
		 FROM traits
		 WHERE chat_id = $1
		 ORDER BY create_at DESC`
	rows, err := s.db().Query(sqlStr, chatID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatID=%d]:\n%v", sqlStr, chatID, err)
		return nil, fmt.Errorf("failed to list traits by chat. %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatID, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error. %v", err)
		return nil, err
	}

	return results, nil
}

// ListAllTraitsByCreateTime returns all personal traits for a user ordered by create_at desc.
func (s *BrainStore) ListAllTraitsByCreateTime(userID int64) ([]PersonalTrait, error) {
	sqlStr := `SELECT id, trait, category, confidence, half_life, privacy_level, chat_id, create_at, update_at
		 FROM traits
		 WHERE user_id = $1
		 ORDER BY create_at DESC`
	rows, err := s.db().Query(sqlStr, userID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to list all traits. %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatID, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error. %v", err)
		return nil, err
	}

	return results, nil
}

// Close is a no-op because BrainStore no longer owns a connection.
func (s *BrainStore) Close() error {
	return nil
}

// ============================================================
// AddTraits — insert traits and mark extraction progress atomically
// ============================================================

// AddTraits atomically performs the three write operations of a trait
// extraction cycle in a single database transaction:
//
//	A. Insert traits, vectors and keywords into brain tables.
//	B. Mark chat_messages as extracted (extracted = true).
//	C. Update chat_sessions.extracted_at and extracted_count.
//
// If insertions is empty, only B and C are executed (marks as processed).
// The LLM API call and embedding computation must happen before this function.
//
// Returns the number of traits actually stored.
func (s *BrainStore) AddTraits(ctx context.Context, chatID int64, upToMsgID int64, insertions []TraitInsertion) (int, error) {
	tx, err := s.db().Beginx()
	if err != nil {
		return 0, fmt.Errorf("BEGIN transaction failed. %w", err)
	}
	defer tx.Rollback()

	// ---- A: Insert traits ----
	storedCount := 0
	if len(insertions) > 0 {
		sqlInsertTrait := `INSERT INTO traits(user_id, trait, category, confidence, half_life, privacy_level, chat_id)
			 VALUES($1, $2, $3, $4, $5, $6, $7)
			 RETURNING id`
		sqlInsertVec := "INSERT INTO trait_vectors(trait_id, embedding) VALUES($1, $2)"
		sqlInsertKw := "INSERT INTO keywords(word, kind, trait_id) VALUES($1, $2, $3)"

		for _, ins := range insertions {
			var traitID int64
			err := tx.QueryRow(sqlInsertTrait,
				ins.UserID, ins.Trait.Trait, ins.Trait.Category, ins.Trait.Confidence,
				ins.Trait.HalfLife, ins.Trait.PrivacyLevel, ins.Trait.ChatID,
			).Scan(&traitID)
			if err != nil {
				return 0, fmt.Errorf("failed to insert trait. %w", err)
			}

			pgVec := pgvector.NewVector(ins.Vector)
			_, err = tx.Exec(sqlInsertVec, traitID, pgVec)
			if err != nil {
				return 0, fmt.Errorf("failed to insert trait vector. %w", err)
			}

			for _, kw := range ins.Keywords {
				_, err := tx.Exec(sqlInsertKw, kw.Word, kw.Kind, traitID)
				if err != nil {
					return 0, fmt.Errorf("failed to insert keyword. %w", err)
				}
			}
		}
		storedCount = len(insertions)
	}

	// ---- B: Mark messages as extracted ----
	sqlMarkMsg := `UPDATE chat_messages
		 SET extracted = TRUE
		 WHERE chat_id = $1 AND id <= $2 AND extracted = FALSE`
	_, err = tx.Exec(sqlMarkMsg, chatID, upToMsgID)
	if err != nil {
		return 0, fmt.Errorf("failed to mark messages as extracted. %w", err)
	}

	// ---- C: Update session extraction progress ----
	sqlUpdateSession := `UPDATE chat_sessions
		 SET extracted_at = NOW(),
		     extracted_count = extracted_count + $1
		 WHERE id = $2`
	result, err := tx.Exec(sqlUpdateSession, storedCount, chatID)
	if err != nil {
		return 0, fmt.Errorf("failed to update extraction progress. %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return 0, fmt.Errorf("session not found (id=%d)", chatID)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("COMMIT transaction failed. %w", err)
	}
	return storedCount, nil
}
