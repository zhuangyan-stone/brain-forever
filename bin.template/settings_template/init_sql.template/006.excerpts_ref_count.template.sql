-- ============================================================
-- Add ref_count to excerpts table
--
-- Tracks how many times each excerpt has been referenced during
-- conversation. Presented to the LLM so it can prefer less-used
-- excerpts when multiple suitable options are available.
-- ============================================================

ALTER TABLE excerpts
    ADD COLUMN IF NOT EXISTS ref_count INT NOT NULL DEFAULT 0;
