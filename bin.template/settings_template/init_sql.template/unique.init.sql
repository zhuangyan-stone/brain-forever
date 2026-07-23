-- ============================================================
-- BrainForever Unified Database Initialization Script
-- 语义合自: 000.init.template.sql + 002.excerpts.template.sql
--           (001 的 chat_sn→chat_id 变更已在 000 中体现)
--           (003 的 msg_time/last_ref_at 已在 002 建表时纳入)
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
	user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	sn             VARCHAR(48)  NOT NULL UNIQUE,
	role_no        BIGINT       NOT NULL DEFAULT 0,
	title          TEXT         NOT NULL DEFAULT '',
	title_state    SMALLINT     NOT NULL DEFAULT 0,
	extract_mode   SMALLINT     NOT NULL DEFAULT 0,
	extracted_at   TIMESTAMPTZ,
	extracted_count INTEGER     NOT NULL DEFAULT 0,
	pinned         BOOLEAN      NOT NULL DEFAULT FALSE,
	taged          BOOLEAN      NOT NULL DEFAULT FALSE,
	deleted        BOOLEAN      NOT NULL DEFAULT FALSE,
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
	trait_id  BIGINT       NOT NULL REFERENCES traits(id) ON DELETE CASCADE,
	word      TEXT         NOT NULL,
	kind      INTEGER      NOT NULL,
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
-- excerpt_value_dict table: dictionary of excerpt value types
-- 来源: 002.excerpts.template.sql
-- ============================================================
CREATE TABLE IF NOT EXISTS excerpt_value_dict (
	id      SMALLINT     PRIMARY KEY,
	value   VARCHAR(38)  NOT NULL,
	value_cn VARCHAR(40) NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_excerpt_value_dict_value
	ON excerpt_value_dict(value);

-- Seed data: 14 excerpt value types
INSERT INTO excerpt_value_dict (id, value, value_cn) VALUES
	(1,  'insight',        '深刻见解'),
	(2,  'humor',          '幽默搞笑'),
	(3,  'vent',           '精彩吐槽'),
	(4,  'methodology',    '独到方法'),
	(5,  'rule',           '做事原则'),
	(6,  'confession',     '内心独白'),
	(7,  'nostalgia',      '深情回忆'),
	(8,  'regret',         '一生遗憾'),
	(9,  'self_discovery', '人生顿悟'),
	(10, 'conviction',     '人生信念'),
	(11, 'touching',       '感人言论'),
	(12, 'deed',           '动人事迹'),
	(13, 'privacy',        '隐私分享'),
	(14, 'literary',       '文采出众')
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- excerpts table: stores user quote excerpts
-- 来源: 002.excerpts.template.sql
-- msg_time / last_ref_at 已直接纳入建表（003 变更已合并）
-- ============================================================
-- excerpts table: stores user quote excerpts
-- 来源: 002.excerpts.template.sql + 003 + 006 (ref_count)
-- ============================================================
CREATE TABLE IF NOT EXISTS excerpts (
	id              BIGSERIAL    PRIMARY KEY,
	user_id         BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	chat_id         BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
	msg_id          BIGINT       NOT NULL,
	msg_time        TIMESTAMPTZ  NOT NULL,      -- 来源消息的发送时间，方便前端展示
	last_ref_at     TIMESTAMPTZ,                -- 上次被引用时间
	ref_count       INT          NOT NULL DEFAULT 0,  -- 被引用次数
	values          SMALLINT[]   NOT NULL,
	content         VARCHAR(380) NOT NULL,
	context_summary VARCHAR(520) NOT NULL DEFAULT '',
	reason          VARCHAR(400) NOT NULL DEFAULT '',
	create_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_excerpts_user_id    ON excerpts(user_id);
CREATE INDEX IF NOT EXISTS idx_excerpts_chat_id    ON excerpts(chat_id);
CREATE INDEX IF NOT EXISTS idx_excerpts_msg_id     ON excerpts(msg_id);
CREATE INDEX IF NOT EXISTS idx_excerpts_create_at  ON excerpts(create_at);
CREATE INDEX IF NOT EXISTS idx_excerpts_values_gin ON excerpts USING GIN(values);
CREATE INDEX IF NOT EXISTS idx_excerpts_user_msg_time ON excerpts(user_id, msg_time DESC);
CREATE INDEX IF NOT EXISTS idx_excerpts_user_last_ref ON excerpts(user_id, last_ref_at DESC);

-- ============================================================
-- excerpt_progress table: tracks excerpt extraction progress per chat
-- 来源: 002.excerpts.template.sql
-- ============================================================
CREATE TABLE IF NOT EXISTS excerpt_progress (
	chat_id      BIGINT       PRIMARY KEY REFERENCES chat_sessions(id) ON DELETE CASCADE,
	processed_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	last_msg_id  BIGINT       NOT NULL DEFAULT 0
);

-- ============================================================
-- excerpt_vectors table: stores embeddings for excerpt entries
-- 来源: 004.excerpts_embedding.template.sql
-- 语义合并: last_msg_id 已合并到 excerpt_progress 建表语句中
-- ============================================================
CREATE TABLE IF NOT EXISTS excerpt_vectors (
	excerpt_id  BIGINT       PRIMARY KEY REFERENCES excerpts(id) ON DELETE CASCADE,
	embedding   VECTOR(1024) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_excerpt_vectors_embedding
	ON excerpt_vectors USING hnsw (embedding vector_cosine_ops);

-- ============================================================
-- Trigger: auto-update update_at for tables that have the column
-- 来源: 000.init.template.sql
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

-- ============================================================
-- user_portraits table: persisted user portrait (AI impression)
-- ============================================================
CREATE TABLE IF NOT EXISTS user_portraits (
	id              BIGSERIAL    PRIMARY KEY,
	user_id         BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title           TEXT         NOT NULL DEFAULT '',
	content         TEXT         NOT NULL DEFAULT '',
	core_traits     JSONB        NOT NULL DEFAULT '[]',
	key_highlights  JSONB        NOT NULL DEFAULT '[]',
	hot_tags        JSONB        NOT NULL DEFAULT '[]',
	hottest_tag         TEXT    NOT NULL DEFAULT '',
	hottest_tag_count   INT     NOT NULL DEFAULT 0,
	chat_count      INT          NOT NULL DEFAULT 0,
	trait_count     INT          NOT NULL DEFAULT 0,
	span_days       INT          NOT NULL DEFAULT 0,
	earliest_date   DATE,
	latest_date     DATE,
	retouch         INT          NOT NULL DEFAULT 3,
	created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_portraits_user_id
	ON user_portraits(user_id);
CREATE INDEX IF NOT EXISTS idx_user_portraits_user_created
	ON user_portraits(user_id, created_at DESC);
