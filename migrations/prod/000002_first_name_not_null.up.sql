UPDATE "users" SET "first_name" = 'Пользователь' WHERE "first_name" IS NULL;

ALTER TABLE "users" ALTER COLUMN "first_name" SET NOT NULL;