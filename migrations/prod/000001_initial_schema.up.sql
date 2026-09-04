CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE "users" (
                         "user_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
                         "email" varchar UNIQUE NOT NULL,
                         "password_hash" varchar NOT NULL,
                         "first_name" varchar NOT NULL,
                         "last_name" varchar,
                         "middle_name" varchar,
                         "email_verified_at" timestamp,
                         "updated_at" timestamp NOT NULL DEFAULT (now()),
                         "created_at" timestamp NOT NULL DEFAULT (now())
);

CREATE TABLE "groups" (
                          "group_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
                          "title" varchar NOT NULL,
                          "updated_at" timestamp NOT NULL DEFAULT (now()),
                          "created_at" timestamp NOT NULL DEFAULT (now())
);

CREATE TABLE "group_members" (
                                 "group_id" uuid NOT NULL,
                                 "user_id" uuid NOT NULL,
                                 "role" varchar NOT NULL CHECK (role IN ('owner', 'member')),
                                 PRIMARY KEY ("group_id", "user_id")
);

CREATE TABLE "goals" (
                         "goal_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
                         "group_id" uuid NOT NULL,
                         "title" varchar NOT NULL,
                         "note" text,
                         "target_amount_cents" bigint NOT NULL CHECK (target_amount_cents >= 0),
                         "creator_id" uuid NOT NULL,
                         "updated_at" timestamp NOT NULL DEFAULT (now()),
                         "created_at" timestamp NOT NULL DEFAULT (now())
);

CREATE TABLE "contributions" (
                                 "contribution_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
                                 "user_id" uuid NOT NULL,
                                 "goal_id" uuid NOT NULL,
                                 "amount_cents" bigint NOT NULL CHECK (amount_cents != 0),
  "note" text,
  "created_at" timestamp NOT NULL DEFAULT (now())
);

CREATE TABLE "goal_shares" (
                               "goal_id" uuid NOT NULL,
                               "user_id" uuid NOT NULL,
                               "percent" int NOT NULL CHECK (percent >= 0 AND percent <= 100),
                               PRIMARY KEY ("goal_id", "user_id")
);

ALTER TABLE "group_members" ADD FOREIGN KEY ("user_id") REFERENCES "users" ("user_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE "group_members" ADD FOREIGN KEY ("group_id") REFERENCES "groups" ("group_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE "goals" ADD FOREIGN KEY ("group_id") REFERENCES "groups" ("group_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE "goals" ADD FOREIGN KEY ("creator_id") REFERENCES "users" ("user_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE "contributions" ADD FOREIGN KEY ("user_id") REFERENCES "users" ("user_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE "contributions" ADD FOREIGN KEY ("goal_id") REFERENCES "goals" ("goal_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE "goal_shares" ADD FOREIGN KEY ("goal_id") REFERENCES "goals" ("goal_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE "goal_shares" ADD FOREIGN KEY ("user_id") REFERENCES "users" ("user_id") ON DELETE RESTRICT DEFERRABLE INITIALLY IMMEDIATE;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON "users"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER groups_set_updated_at
    BEFORE UPDATE ON "groups"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER goals_set_updated_at
    BEFORE UPDATE ON "goals"
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();