-- ============================================================
-- excerpt_value_dict table: dictionary of excerpt value types
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
-- ============================================================
CREATE TABLE IF NOT EXISTS excerpts (
	id              BIGSERIAL    PRIMARY KEY,
	user_id         BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	chat_id         BIGINT       NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
	msg_id          BIGINT       NOT NULL,
	msg_time        TIMESTAMPTZ,               -- 来源消息的发送时间，方便前端展示
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

-- ============================================================
-- chat_excerpt_progress table: tracks excerpt extraction progress per chat
-- ============================================================
CREATE TABLE IF NOT EXISTS chat_excerpt_progress (
	chat_id      BIGINT       PRIMARY KEY REFERENCES chat_sessions(id) ON DELETE CASCADE,
	processed_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
