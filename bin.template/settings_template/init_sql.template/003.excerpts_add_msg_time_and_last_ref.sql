-- ============================================================
-- Migration 003: add msg_time and last_ref_at to excerpts table
--
-- Adds two columns:
--   msg_time    TIMESTAMPTZ  NOT NULL — source message creation time
--   last_ref_at TIMESTAMPTZ           — last referenced time (nullable)
--
-- Backfills existing NULL msg_time rows with create_at before
-- applying the NOT NULL constraint.
--
-- This migration is idempotent — safe to run multiple times.
-- ============================================================

BEGIN;

-- Step 1: Add msg_time column (nullable initially for backfill)
ALTER TABLE excerpts ADD COLUMN IF NOT EXISTS msg_time TIMESTAMPTZ;

-- Step 2: Backfill NULL msg_time with create_at for existing rows
UPDATE excerpts SET msg_time = create_at WHERE msg_time IS NULL;

-- Step 3: Set NOT NULL constraint now that all rows have a value
ALTER TABLE excerpts ALTER COLUMN msg_time SET NOT NULL;

-- Step 4: Add last_ref_at column (nullable, no backfill needed)
ALTER TABLE excerpts ADD COLUMN IF NOT EXISTS last_ref_at TIMESTAMPTZ;

-- Step 5: Create composite index for efficient user-scoped time-ordered queries
CREATE INDEX IF NOT EXISTS idx_excerpts_user_msg_time ON excerpts(user_id, msg_time DESC);

-- Step 6: Create index for sorting by last referenced time
CREATE INDEX IF NOT EXISTS idx_excerpts_user_last_ref ON excerpts(user_id, last_ref_at DESC);

COMMIT;
