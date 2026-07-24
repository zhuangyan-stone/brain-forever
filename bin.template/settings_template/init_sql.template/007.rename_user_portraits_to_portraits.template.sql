-- ============================================================
-- Migration: Rename user_portraits → portraits
-- ============================================================
BEGIN;

-- Rename the table (only if the old name exists and new name doesn't)
DO $$
BEGIN
	IF EXISTS (SELECT 1 FROM pg_tables WHERE tablename = 'user_portraits')
	   AND NOT EXISTS (SELECT 1 FROM pg_tables WHERE tablename = 'portraits') THEN
		ALTER TABLE user_portraits RENAME TO portraits;
	END IF;
END $$;

-- Rename indexes (only if they still bear the old names)
ALTER INDEX IF EXISTS idx_user_portraits_user_id
	RENAME TO idx_portraits_user_id;
ALTER INDEX IF EXISTS idx_user_portraits_user_created
	RENAME TO idx_portraits_user_created;

COMMIT;
