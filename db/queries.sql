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
SELECT ID FROM Log WHERE Timestamp < ? ORDER BY Timestamp LIMIT 100;

-- name: DeleteLogByIDs :exec
DELETE FROM Log WHERE ID IN (sqlc.slice('ids'));
