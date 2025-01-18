-- +goose Up
-- +goose StatementBegin
CREATE TABLE auser (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT
);
-- +goose StatementEnd

-- GRANT SELECT, UPDATE, INSERT, DELETE ON TABLE auser TO sqlok;
-- +goose Down
-- +goose StatementBegin
DROP TABLE auser;
-- +goose StatementEnd
