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
	ChatSN       string    `db:"chat_sn"`
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

// AddTrait inserts a personal trait and its vector embedding.
func (s *BrainStore) AddTrait(ctx context.Context, userID int64, trait *PersonalTrait, embedding []float32) (int64, error) {
	tx, err := s.db().Begin()
	if err != nil {
		s.logger.Errorf("BEGIN transaction failed: %v", err)
		return 0, err
	}
	defer tx.Rollback()

	sqlInsertTrait := `INSERT INTO traits(user_id, trait, category, confidence, half_life, privacy_level, chat_sn)
		 VALUES($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`
	var traitID int64
	err = tx.QueryRow(sqlInsertTrait,
		userID, trait.Trait, trait.Category, trait.Confidence, trait.HalfLife, trait.PrivacyLevel, trait.ChatSN,
	).Scan(&traitID)
	if err != nil {
		s.logger.Errorf("SQL [%s]:\n%v", sqlInsertTrait, err)
		return 0, fmt.Errorf("failed to insert trait: %w", err)
	}

	pgVec := pgvector.NewVector(embedding)
	sqlInsertVec := "INSERT INTO trait_vectors(trait_id, embedding) VALUES($1, $2)"
	_, err = tx.Exec(sqlInsertVec, traitID, pgVec)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[traitID=%d]:\n%v", sqlInsertVec, traitID, err)
		return 0, fmt.Errorf("failed to insert trait vector: %w", err)
	}

	return traitID, tx.Commit()
}

// TraitInsertion holds all data needed to insert a single trait.
type TraitInsertion struct {
	Trait    PersonalTrait
	Vector   []float32
	Keywords []TraitKeyword
	UserID   int64 // required for data isolation
}

// AddTraits inserts multiple traits atomically.
func (s *BrainStore) AddTraits(ctx context.Context, insertions []TraitInsertion) (int, error) {
	tx, err := s.db().Begin()
	if err != nil {
		s.logger.Errorf("BEGIN transaction failed: %v", err)
		return 0, err
	}
	defer tx.Rollback()

	sqlInsertTrait := `INSERT INTO traits(user_id, trait, category, confidence, half_life, privacy_level, chat_sn)
			 VALUES($1, $2, $3, $4, $5, $6, $7)
			 RETURNING id`
	sqlInsertVec := "INSERT INTO trait_vectors(trait_id, embedding) VALUES($1, $2)"
	sqlInsertKw := "INSERT INTO keywords(word, kind, trait_id) VALUES($1, $2, $3)"

	for _, ins := range insertions {
		var traitID int64
		err = tx.QueryRow(sqlInsertTrait,
			ins.UserID, ins.Trait.Trait, ins.Trait.Category, ins.Trait.Confidence,
			ins.Trait.HalfLife, ins.Trait.PrivacyLevel, ins.Trait.ChatSN,
		).Scan(&traitID)
		if err != nil {
			s.logger.Errorf("SQL [%s]:\n%v", sqlInsertTrait, err)
			return 0, fmt.Errorf("failed to insert trait: %w", err)
		}

		pgVec := pgvector.NewVector(ins.Vector)
		_, err = tx.Exec(sqlInsertVec, traitID, pgVec)
		if err != nil {
			s.logger.Errorf("SQL [%s] args=[traitID=%d]:\n%v", sqlInsertVec, traitID, err)
			return 0, fmt.Errorf("failed to insert trait vector: %w", err)
		}

		for _, kw := range ins.Keywords {
			_, err := tx.Exec(sqlInsertKw, kw.Word, kw.Kind, traitID)
			if err != nil {
				s.logger.Errorf("SQL [%s] args=[traitID=%d word=%s kind=%d]:\n%v", sqlInsertKw, traitID, kw.Word, kw.Kind, err)
				return 0, fmt.Errorf("failed to insert keyword: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		s.logger.Errorf("COMMIT transaction failed: %v", err)
		return 0, fmt.Errorf("failed to commit traits transaction: %w", err)
	}
	return len(insertions), nil
}

// AddKeyword inserts a keyword associated with a trait.
func (s *BrainStore) AddKeyword(kw *TraitKeyword) (int64, error) {
	sqlStr := "INSERT INTO keywords(word, kind, trait_id) VALUES($1, $2, $3) RETURNING id"
	var id int64
	err := s.db().QueryRow(sqlStr, kw.Word, kw.Kind, kw.TraitID).Scan(&id)
	if err != nil {
		s.logger.Errorf("SQL [%s]:\n%v", sqlStr, err)
		return 0, fmt.Errorf("failed to insert keyword: %w", err)
	}
	return id, nil
}

// SearchByVector performs vector similarity search with optional category filter.
// userID is required for data isolation.
func (s *BrainStore) SearchByVector(userID int64, query []float32, category int, topK int) ([]PersonalTrait, error) {
	pgVec := pgvector.NewVector(query)

	sqlQuery := `SELECT v.trait_id, v.embedding <=> $1 AS distance,
		t.id, t.trait, t.category, t.confidence, t.half_life,
		t.privacy_level, t.chat_sn, t.create_at, t.update_at
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
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var (
			traitID                        int64
			distance                       float64
			traitCat, confidence, halfLife int64
			privacyLevel                   int
			traitText, chatSN              string
			createAt, updateAt             time.Time
		)
		if err := rows.Scan(&traitID, &distance, &traitID, &traitText, &traitCat,
			&confidence, &halfLife, &privacyLevel, &chatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}

		score := 1.0 - distance

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
		s.logger.Errorf("rows iteration error: %v", err)
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
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE t.user_id = $1 AND k.word = $2 AND k.kind = $3
		 ORDER BY t.id DESC
		 LIMIT $4`
		args = []interface{}{userID, word, kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
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
		return nil, fmt.Errorf("keyword search failed: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error: %v", err)
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
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
		 FROM traits t
		 INNER JOIN keywords k ON k.trait_id = t.id
		 WHERE t.user_id = $1 AND k.word LIKE $2 AND k.kind = $3
		 ORDER BY t.id DESC
		 LIMIT $4`
		args = []interface{}{userID, "%" + word + "%", kind, limit}
	} else {
		query = `SELECT DISTINCT t.id, t.trait, t.category, t.confidence, t.half_life,
		                t.privacy_level, t.chat_sn, t.create_at, t.update_at
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
		return nil, fmt.Errorf("fuzzy keyword search failed: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error: %v", err)
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
		return fmt.Errorf("failed to delete trait: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("trait not found (id=%d)", id)
	}
	return nil
}

// DeleteByChatSN deletes all traits for a chat SN.
func (s *BrainStore) DeleteByChatSN(chatSN string) (int, error) {
	if chatSN == "" {
		return 0, fmt.Errorf("empty chat SN")
	}

	sqlStr := "DELETE FROM traits WHERE chat_sn = $1"
	result, err := s.db().Exec(sqlStr, chatSN)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatSN=%s]:\n%v", sqlStr, chatSN, err)
		return 0, fmt.Errorf("failed to delete traits by chat SN: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
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

// ListTraitsByChat returns all traits for a given chat SN.
func (s *BrainStore) ListTraitsByChat(chatSN string) ([]PersonalTrait, error) {
	sqlStr := `SELECT id, trait, category, confidence, half_life, privacy_level, chat_sn, create_at, update_at
		 FROM traits
		 WHERE chat_sn = $1
		 ORDER BY create_at DESC`
	rows, err := s.db().Query(sqlStr, chatSN)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[chatSN=%s]:\n%v", sqlStr, chatSN, err)
		return nil, fmt.Errorf("failed to list traits by chat: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error: %v", err)
		return nil, err
	}

	return results, nil
}

// ListAllTraitsByCreateTime returns all personal traits for a user ordered by create_at desc.
func (s *BrainStore) ListAllTraitsByCreateTime(userID int64) ([]PersonalTrait, error) {
	sqlStr := `SELECT id, trait, category, confidence, half_life, privacy_level, chat_sn, create_at, update_at
		 FROM traits
		 WHERE user_id = $1
		 ORDER BY create_at DESC`
	rows, err := s.db().Query(sqlStr, userID)
	if err != nil {
		s.logger.Errorf("SQL [%s] args=[userID=%d]:\n%v", sqlStr, userID, err)
		return nil, fmt.Errorf("failed to list all traits: %w", err)
	}
	defer rows.Close()

	var results []PersonalTrait
	for rows.Next() {
		var pt PersonalTrait
		var createAt, updateAt time.Time
		if err := rows.Scan(&pt.ID, &pt.Trait, &pt.Category, &pt.Confidence, &pt.HalfLife,
			&pt.PrivacyLevel, &pt.ChatSN, &createAt, &updateAt); err != nil {
			return nil, err
		}
		pt.CreateAt = createAt
		pt.UpdateAt = updateAt
		results = append(results, pt)
	}
	if err := rows.Err(); err != nil {
		s.logger.Errorf("rows iteration error: %v", err)
		return nil, err
	}

	return results, nil
}

// Close is a no-op because BrainStore no longer owns a connection.
func (s *BrainStore) Close() error {
	return nil
}
