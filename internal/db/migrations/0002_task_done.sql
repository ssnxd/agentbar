-- +goose Up
-- Tasks gain an explicit lifecycle beyond active/archived: 'done' means the
-- orchestrator reported the work merged and verified — ready for human
-- review of the task branch.
ALTER TABLE tasks ADD COLUMN summary TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tasks DROP COLUMN summary;
