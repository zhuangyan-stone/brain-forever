-- ============================================================
-- user_portraits table: persisted user portrait (AI impression)
-- All historical records are kept permanently.
-- ============================================================
CREATE TABLE IF NOT EXISTS user_portraits (
	id              BIGSERIAL    PRIMARY KEY,
	user_id         BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,

	-- LLM-generated content
	title           TEXT         NOT NULL DEFAULT '',
	content         TEXT         NOT NULL DEFAULT '',

	-- Structured metadata (JSONB for flexibility)
	core_traits     JSONB        NOT NULL DEFAULT '[]',
	key_highlights  JSONB        NOT NULL DEFAULT '[]',
	hot_tags        JSONB        NOT NULL DEFAULT '[]',

	-- Denormalized: hottest tag for quick SQL-level query
	hottest_tag         TEXT    NOT NULL DEFAULT '',
	hottest_tag_count   INT     NOT NULL DEFAULT 0,

	-- Base info
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
