-- ============================================================
-- excerpt_vectors table: stores embeddings for excerpt entries
--
-- Using a separate table (like trait_vectors for traits) keeps
-- the vector dimension configurable and avoids bloating the
-- excerpts table with a high-dimensional column.
-- ============================================================

CREATE TABLE IF NOT EXISTS excerpt_vectors (
    excerpt_id  BIGINT       PRIMARY KEY REFERENCES excerpts(id) ON DELETE CASCADE,
    embedding   VECTOR(1024) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_excerpt_vectors_embedding
    ON excerpt_vectors USING hnsw (embedding vector_cosine_ops);
