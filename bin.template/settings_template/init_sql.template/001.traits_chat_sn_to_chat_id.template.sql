-- ============================================================
-- Migration 001: traits.chat_sn → traits.chat_id
--
-- Changes the foreign key column in the traits table from
-- chat_sn (VARCHAR, referencing chat_sessions.sn) to
-- chat_id (BIGINT, referencing chat_sessions.id).
--
-- This migration is idempotent — safe to run multiple times.
-- ============================================================

BEGIN;

-- Step 1: Add the new chat_id column (nullable initially)
ALTER TABLE traits ADD COLUMN IF NOT EXISTS chat_id BIGINT;

-- Step 2: Backfill chat_id from chat_sessions for existing rows
UPDATE traits t
SET chat_id = cs.id
FROM chat_sessions cs
WHERE t.chat_sn = cs.sn
  AND t.chat_id IS NULL;

-- Step 3: Clean up orphaned traits (no matching chat_session)
DELETE FROM traits WHERE chat_id IS NULL;

-- Step 4: Set chat_id to NOT NULL now that all rows have a value
ALTER TABLE traits ALTER COLUMN chat_id SET NOT NULL;

-- Step 5: Drop the old chat_sn column and its index
DROP INDEX IF EXISTS idx_traits_chat_sn;
ALTER TABLE traits DROP COLUMN IF EXISTS chat_sn;

-- Step 6: Add foreign key constraint and new index
ALTER TABLE traits ADD CONSTRAINT fk_traits_chat_id
    FOREIGN KEY (chat_id) REFERENCES chat_sessions(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_traits_chat_id ON traits(chat_id);

COMMIT;
