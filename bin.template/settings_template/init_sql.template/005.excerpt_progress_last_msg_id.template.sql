-- ============================================================
-- Add last_msg_id to excerpt_progress table
--
-- Tracks the ID of the last message processed during excerpt
-- extraction. On re-processing, only messages with ID greater
-- than last_msg_id are sent to the LLM, avoiding redundant
-- re-extraction and saving tokens.
-- ============================================================

ALTER TABLE excerpt_progress
    ADD COLUMN IF NOT EXISTS last_msg_id BIGINT NOT NULL DEFAULT 0;
