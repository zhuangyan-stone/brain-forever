-- ============================================================
-- BrainForever database initialization script
-- ============================================================

-- ============================================================
-- users table: stores user accounts
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
	id            BIGSERIAL PRIMARY KEY,
	sn            VARCHAR(48)  NOT NULL UNIQUE,
	no            VARCHAR(6)   NOT NULL UNIQUE,
	tel           VARCHAR(18)  NOT NULL DEFAULT '',
	nickname      VARCHAR(38)  NOT NULL,
	password      VARCHAR(32)  NOT NULL,
	salt          VARCHAR(32)  NOT NULL,
	deleted       BOOLEAN      NOT NULL DEFAULT FALSE,
	settings_ver  INTEGER      NOT NULL DEFAULT 0,
	settings      JSONB        NOT NULL DEFAULT '{}',
	create_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	update_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_sn        ON users(sn);
CREATE INDEX IF NOT EXISTS idx_users_no        ON users(no);
CREATE INDEX IF NOT EXISTS idx_users_tel       ON users(tel);
CREATE INDEX IF NOT EXISTS idx_users_create_at ON users(create_at);
CREATE INDEX IF NOT EXISTS idx_users_update_at ON users(update_at);

-- ============================================================
-- roles table: stores user roles/personalities
-- ============================================================
CREATE TABLE IF NOT EXISTS roles (
	id         BIGSERIAL PRIMARY KEY,
	user_id    BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role_no    INTEGER      NOT NULL,
	role_name  VARCHAR(60)  NOT NULL,
	uuid       VARCHAR(32)  NOT NULL,
	is_public  BOOLEAN      NOT NULL DEFAULT FALSE,
	is_active  BOOLEAN      NOT NULL DEFAULT TRUE,
	create_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	update_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_roles_user_id ON roles(user_id);

-- ============================================================
-- chat_sessions table: stores conversation sessions
-- ============================================================
CREATE TABLE IF NOT EXISTS chat_sessions (
	id             BIGSERIAL PRIMARY KEY,
	sn             VARCHAR(48)  NOT NULL UNIQUE,
	user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role_no        BIGINT       NOT NULL DEFAULT 0,
	title          TEXT         NOT NULL DEFAULT '',
	title_state    SMALLINT     NOT NULL DEFAULT 0,
	extract_mode   SMALLINT     NOT NULL DEFAULT 0,
	extracted_at   TIMESTAMPTZ,
	extracted_count INTEGER     NOT NULL DEFAULT 0,
	deleted        BOOLEAN      NOT NULL DEFAULT FALSE,
	pinned         BOOLEAN      NOT NULL DEFAULT FALSE,
	taged          BOOLEAN      NOT NULL DEFAULT FALSE,
	create_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	update_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_sessions_user_id       ON chat_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_sn            ON chat_sessions(sn);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_create_at     ON chat_sessions(deleted, create_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_update_at     ON chat_sessions(deleted, update_at DESC);

-- ============================================================
-- chat_messages table: stores messages within chat sessions
-- ============================================================
CREATE TABLE IF NOT EXISTS chat_messages (
	id          BIGSERIAL PRIMARY KEY,
	chat_id     BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
	group_index INTEGER      NOT NULL,
	role        SMALLINT     NOT NULL,
	reasoning   TEXT,
	content     TEXT         NOT NULL,
	extracted   BOOLEAN      NOT NULL DEFAULT FALSE,
	interrupted SMALLINT    NOT NULL DEFAULT 0,
	create_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	update_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_id ON chat_messages(chat_id, group_index, id);

-- ============================================================
-- web_sources table: stores web sources cited in messages
-- ============================================================
CREATE TABLE IF NOT EXISTS web_sources (
	id           BIGSERIAL PRIMARY KEY,
	chat_id      BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
	msg_id       BIGINT       NOT NULL,
	title        TEXT         NOT NULL DEFAULT '',
	content      TEXT         NOT NULL DEFAULT '',
	url          TEXT         NOT NULL DEFAULT '',
	site_name    TEXT         NOT NULL DEFAULT '',
	site_icon    TEXT         NOT NULL DEFAULT '',
	publish_date TEXT         NOT NULL DEFAULT '',
	score        REAL         NOT NULL DEFAULT 0,
	create_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_web_sources_chat_msg ON web_sources(chat_id, msg_id);

-- ============================================================
-- chat_tags table: stores tags for chat sessions
-- ============================================================
CREATE TABLE IF NOT EXISTS chat_tags (
	id        BIGSERIAL PRIMARY KEY,
	chat_id   BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
	tag       TEXT         NOT NULL,
	create_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	UNIQUE(chat_id, tag)
);

-- (chat_id, tag) uniqueness is enforced by the UNIQUE constraint above,
-- which also serves as a composite index for chat_id-based lookups.
CREATE INDEX IF NOT EXISTS idx_chat_tags_tag ON chat_tags(tag);

-- ============================================================
-- chat_favorites table: stores favorited chat sessions
-- ============================================================
CREATE TABLE IF NOT EXISTS chat_favorites (
	id         BIGSERIAL PRIMARY KEY,
	chat_id    BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
	custom_tag TEXT         NOT NULL DEFAULT '',
	create_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	update_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_favorites_unique ON chat_favorites(chat_id, custom_tag);

-- ============================================================
-- traits table: stores personal trait (feature) entities
-- ============================================================
CREATE TABLE IF NOT EXISTS traits (
	id             BIGSERIAL PRIMARY KEY,
	user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	chat_id        BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
	trait          TEXT         NOT NULL,
	category       INTEGER      NOT NULL,
	confidence     INTEGER      NOT NULL,
	half_life      INTEGER      NOT NULL,
	privacy_level  INTEGER      NOT NULL DEFAULT 0,
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
CREATE INDEX IF NOT EXISTS idx_traits_chat_id    ON traits(chat_id);

-- indexes for keywords table
CREATE INDEX IF NOT EXISTS idx_keywords_trait_id      ON keywords(trait_id);
CREATE INDEX IF NOT EXISTS idx_keywords_word          ON keywords(word);
CREATE INDEX IF NOT EXISTS idx_keywords_kind          ON keywords(kind);
CREATE INDEX IF NOT EXISTS idx_keywords_trait_kind    ON keywords(trait_id, kind);

-- HNSW index for vector similarity search (requires pgvector >= 0.5.0)
-- 1024-dim vectors: HNSW dimension limit is 2000 in pgvector <= 0.7.x, ok here.
CREATE INDEX IF NOT EXISTS idx_trait_vectors_hnsw
	ON trait_vectors USING hnsw (embedding vector_cosine_ops)
	WITH (m = 16, ef_construction = 64);

-- ============================================================
-- Trigger: auto-update update_at for tables that have the column
-- ============================================================
CREATE OR REPLACE FUNCTION update_update_at_column()
RETURNS TRIGGER AS $body$
BEGIN
	IF OLD IS DISTINCT FROM NEW THEN
		NEW.update_at = NOW();
	END IF;
	RETURN NEW;
END;
$body$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_update_at
	BEFORE UPDATE ON users
	FOR EACH ROW
	EXECUTE FUNCTION update_update_at_column();

CREATE TRIGGER trg_roles_update_at
	BEFORE UPDATE ON roles
	FOR EACH ROW
	EXECUTE FUNCTION update_update_at_column();

CREATE TRIGGER trg_chat_sessions_update_at
	BEFORE UPDATE ON chat_sessions
	FOR EACH ROW
	EXECUTE FUNCTION update_update_at_column();

CREATE TRIGGER trg_chat_messages_update_at
	BEFORE UPDATE ON chat_messages
	FOR EACH ROW
	EXECUTE FUNCTION update_update_at_column();

CREATE TRIGGER trg_chat_favorites_update_at
	BEFORE UPDATE ON chat_favorites
	FOR EACH ROW
	EXECUTE FUNCTION update_update_at_column();

CREATE TRIGGER trg_traits_update_at
	BEFORE UPDATE ON traits
	FOR EACH ROW
	EXECUTE FUNCTION update_update_at_column();
