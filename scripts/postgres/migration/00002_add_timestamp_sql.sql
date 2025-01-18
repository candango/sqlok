-- +goose Up
-- +goose StatementBegin
ALTER TABLE auser
ADD COLUMN created_at TIMESTAMP with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE auser
DROP COLUMN created_at;
-- +goose StatementEnd
