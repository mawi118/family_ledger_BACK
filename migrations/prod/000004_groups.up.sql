-- Группы: дата вступления, доход участника (в копейках), коды приглашений, ограничения.

ALTER TABLE group_members
    ADD COLUMN joined_at    timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN income_cents bigint      NOT NULL DEFAULT 0 CHECK (income_cents >= 0);

-- у каждой группы ровно один создатель
CREATE UNIQUE INDEX group_members_one_owner ON group_members (group_id) WHERE role = 'owner';
CREATE INDEX group_members_user_idx ON group_members (user_id);

ALTER TABLE groups
    ADD CONSTRAINT groups_title_len CHECK (char_length(title) BETWEEN 1 AND 100);

-- Один код на группу. Код глобально уникален: пока он лежит в таблице, другая группа его получить не может.
-- Использованный код удаляется; просроченный перезаписывается при выдаче нового (см. internal/group).
CREATE TABLE group_invites (
    group_id   uuid PRIMARY KEY REFERENCES groups (group_id) ON DELETE CASCADE,
    code       varchar(4)  NOT NULL UNIQUE CHECK (code ~ '^[0-9]{4}$'),
    expires_at timestamptz NOT NULL
);
