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
