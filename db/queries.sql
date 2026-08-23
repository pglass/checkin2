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
INSERT INTO Log (StudentID, StudentName, Action, Timestamp, AuthorizedAdult) VALUES (?, ?, ?, ?, ?);

-- name: RecentAuthorizedAdults :many
-- Distinct authorized adults who recently signed this student in or out, most
-- recently used first. The inner query walks idx_log_student_ts backwards and
-- stops after 'scan' rows, so cost does not grow with the student's history;
-- grouping outside it collapses the usual "same adult over and over" case, and
-- MAX(Timestamp) orders the names by their latest use.
SELECT AuthorizedAdult FROM (
    SELECT AuthorizedAdult, Timestamp FROM Log
    WHERE StudentID = sqlc.arg('student_id')
      AND Action IN ('Checked In', 'Checked Out')
      AND AuthorizedAdult IS NOT NULL AND AuthorizedAdult <> ''
    ORDER BY Timestamp DESC
    LIMIT sqlc.arg('scan')
)
GROUP BY AuthorizedAdult
ORDER BY MAX(Timestamp) DESC
LIMIT sqlc.arg('lim');

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
SELECT StudentName, Action, Timestamp, AuthorizedAdult FROM Log
WHERE Action IN ('Checked In', 'Checked Out')
  AND (sqlc.arg('has_start') = 0 OR Timestamp >= sqlc.arg('start'))
  AND (sqlc.arg('has_end') = 0 OR Timestamp <= sqlc.arg('end'))
  AND (sqlc.arg('all_students') = 1 OR StudentName IN (SELECT value FROM json_each(sqlc.arg('names_json'))))
ORDER BY Timestamp ASC
LIMIT sqlc.arg('lim');

-- name: HistoryByName :many
SELECT StudentName, Action, Timestamp, AuthorizedAdult FROM Log
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
