DROP TRIGGER IF EXISTS goals_set_updated_at ON "goals";
DROP TRIGGER IF EXISTS groups_set_updated_at ON "groups";
DROP TRIGGER IF EXISTS users_set_updated_at ON "users";
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS "goal_shares";
DROP TABLE IF EXISTS "contributions";
DROP TABLE IF EXISTS "goals";
DROP TABLE IF EXISTS "group_members";
DROP TABLE IF EXISTS "groups";
DROP TABLE IF EXISTS "users";