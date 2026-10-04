DROP TABLE IF EXISTS group_invites;
ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_title_len;
DROP INDEX IF EXISTS group_members_user_idx;
DROP INDEX IF EXISTS group_members_one_owner;
ALTER TABLE group_members DROP COLUMN IF EXISTS income_cents, DROP COLUMN IF EXISTS joined_at;
