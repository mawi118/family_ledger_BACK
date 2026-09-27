CREATE TABLE "refresh_tokens" (
                                  "token_id" uuid PRIMARY KEY DEFAULT gen_random_uuid(),
                                  "user_id" uuid NOT NULL REFERENCES "users"("user_id") ON DELETE CASCADE,
                                  "token_hash" varchar NOT NULL UNIQUE,
                                  "expires_at" timestamp NOT NULL,
                                  "revoked_at" timestamp,
                                  "created_at" timestamp NOT NULL DEFAULT (now())
);

CREATE INDEX "idx_refresh_tokens_user_id" ON "refresh_tokens" ("user_id");