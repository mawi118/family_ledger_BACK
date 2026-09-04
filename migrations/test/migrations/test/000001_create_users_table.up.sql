CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE "users" (
                         "user_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
                         "email" varchar UNIQUE NOT NULL,
                         "password_hash" varchar NOT NULL,
                         "created_at" timestamp NOT NULL DEFAULT (now())
);