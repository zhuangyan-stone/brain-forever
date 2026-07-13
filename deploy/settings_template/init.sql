-- ============================================================
-- BrainForever database initialization script
-- ============================================================

-- traits table: stores personal trait (feature) entities
CREATE TABLE IF NOT EXISTS traits (
	id             BIGSERIAL PRIMARY KEY,
	user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	trait          TEXT         NOT NULL,
	category       INTEGER      NOT NULL,
	confidence     INTEGER      NOT NULL,
	half_life      INTEGER      NOT NULL,
	privacy_level  INTEGER      NOT NULL DEFAULT 0,
	chat_sn        TEXT         NOT NULL DEFAULT '',
	create_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	update_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- trait_vectors table: stores vector embeddings for each trait
-- {dimension} is replaced at runtime with the actual vector dimension
CREATE TABLE IF NOT EXISTS trait_vectors (
	trait_id  BIGINT PRIMARY KEY REFERENCES traits(id) ON DELETE CASCADE,
	embedding VECTOR({dimension})
);

-- keywords table: stores keywords associated with each trait
CREATE TABLE IF NOT EXISTS keywords (
	id        BIGSERIAL PRIMARY KEY,
	word      TEXT         NOT NULL,
	kind      INTEGER      NOT NULL,
	trait_id  BIGINT       NOT NULL REFERENCES traits(id) ON DELETE CASCADE,
	create_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- indexes for traits table
CREATE INDEX IF NOT EXISTS idx_traits_user_id    ON traits(user_id);
CREATE INDEX IF NOT EXISTS idx_traits_category   ON traits(category);
CREATE INDEX IF NOT EXISTS idx_traits_half_life  ON traits(half_life);
CREATE INDEX IF NOT EXISTS idx_traits_create_at  ON traits(create_at);
CREATE INDEX IF NOT EXISTS idx_traits_chat_sn    ON traits(chat_sn);

-- indexes for keywords table
CREATE INDEX IF NOT EXISTS idx_keywords_trait_id      ON keywords(trait_id);
CREATE INDEX IF NOT EXISTS idx_keywords_word          ON keywords(word);
CREATE INDEX IF NOT EXISTS idx_keywords_kind          ON keywords(kind);
CREATE INDEX IF NOT EXISTS idx_keywords_trait_kind    ON keywords(trait_id, kind);

-- HNSW index for vector similarity search (requires pgvector >= 0.5.0)
CREATE INDEX IF NOT EXISTS idx_trait_vectors_hnsw
	ON trait_vectors USING hnsw (embedding vector_cosine_ops)
	WITH (m = 16, ef_construction = 64);
