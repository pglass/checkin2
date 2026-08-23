CREATE TABLE IF NOT EXISTS Student (
    ID   INTEGER PRIMARY KEY,
    Name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS Log (
    ID          INTEGER PRIMARY KEY,
    StudentID   INTEGER,               -- not a hard FK so 'Deleted' rows survive student removal
    StudentName TEXT NOT NULL,
    Action      TEXT NOT NULL,         -- 'Added' | 'Checked In' | 'Checked Out' | 'Deleted'
    Timestamp   INTEGER NOT NULL,      -- unix epoch seconds; integer -> fast range scans

    -- Name typed by the parent/authorized adult signing the student in or out.
    -- NULL where it does not apply ('Added'/'Deleted' rows) or was not entered.
    AuthorizedAdult TEXT
);

-- Fast "recent"/"today" range queries and fast prune scan of oldest rows.
CREATE INDEX IF NOT EXISTS idx_log_timestamp ON Log(Timestamp);
-- Fast "today's rows for a given student".
CREATE INDEX IF NOT EXISTS idx_log_student_ts ON Log(StudentID, Timestamp);
