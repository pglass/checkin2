-- name: AddStudent :one
INSERT INTO Student (Name) VALUES (?) RETURNING *;

-- name: GetStudentByName :one
SELECT * FROM Student WHERE Name = ?;

-- name: GetStudentByID :one
SELECT * FROM Student WHERE ID = ?;

-- name: ListStudents :many
SELECT * FROM Student ORDER BY Name;

-- name: DeleteStudent :exec
DELETE FROM Student WHERE ID = ?;

-- name: AppendLog :exec
INSERT INTO Log (StudentID, StudentName, Action, Timestamp) VALUES (?, ?, ?, ?);

-- name: LogSince :many
SELECT * FROM Log
WHERE Timestamp >= ? AND Action IN ('Checked In', 'Checked Out')
ORDER BY Timestamp;

-- name: DeleteTodayCheckinsForStudent :exec
DELETE FROM Log
WHERE StudentID = ? AND Timestamp >= ? AND Action IN ('Checked In', 'Checked Out');

-- name: OldestLogIDs :many
SELECT ID FROM Log WHERE Timestamp < ? ORDER BY Timestamp LIMIT ?;

-- name: CountLogOlderThanCapped :one
-- Counts rows older than the cutoff but stops after the cap (second ?), so the
-- scan is bounded on huge backlogs. A result equal to the cap means "at least
-- this many" remain.
SELECT COUNT(*) FROM (SELECT 1 FROM Log WHERE Timestamp < ? LIMIT ?);

-- name: DeleteLogByIDs :exec
DELETE FROM Log WHERE ID IN (sqlc.slice('ids'));

-- name: HistoryByTime :many
SELECT StudentName, Action, Timestamp FROM Log
WHERE Action IN ('Checked In', 'Checked Out')
  AND (sqlc.arg('has_start') = 0 OR Timestamp >= sqlc.arg('start'))
  AND (sqlc.arg('has_end') = 0 OR Timestamp <= sqlc.arg('end'))
  AND (sqlc.arg('all_students') = 1 OR StudentName IN (SELECT value FROM json_each(sqlc.arg('names_json'))))
ORDER BY Timestamp ASC
LIMIT sqlc.arg('lim');

-- name: HistoryByName :many
SELECT StudentName, Action, Timestamp FROM Log
WHERE Action IN ('Checked In', 'Checked Out')
  AND (sqlc.arg('has_start') = 0 OR Timestamp >= sqlc.arg('start'))
  AND (sqlc.arg('has_end') = 0 OR Timestamp <= sqlc.arg('end'))
  AND (sqlc.arg('all_students') = 1 OR StudentName IN (SELECT value FROM json_each(sqlc.arg('names_json'))))
ORDER BY StudentName ASC, Timestamp ASC
LIMIT sqlc.arg('lim');

-- name: CountHistory :one
SELECT COUNT(*) FROM Log
WHERE Action IN ('Checked In', 'Checked Out')
  AND (sqlc.arg('has_start') = 0 OR Timestamp >= sqlc.arg('start'))
  AND (sqlc.arg('has_end') = 0 OR Timestamp <= sqlc.arg('end'))
  AND (sqlc.arg('all_students') = 1 OR StudentName IN (SELECT value FROM json_each(sqlc.arg('names_json'))));
