-- +migrate Down
ALTER TABLE app_versions
    DROP COLUMN IF EXISTS notes_markdown,
    DROP COLUMN IF EXISTS notes_format;
