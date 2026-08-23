CREATE TABLE IF NOT EXISTS Student (
    ID        INTEGER PRIMARY KEY,
    FirstName TEXT NOT NULL,
    LastName  TEXT NOT NULL,

    -- A student is identified by the pair, not by either part alone. Ordered
    -- (LastName, FirstName) so the index also serves the roster sort.
    UNIQUE (LastName, FirstName)
);

CREATE TABLE IF NOT EXISTS Log (
    ID          INTEGER PRIMARY KEY,
    StudentID   INTEGER,               -- not a hard FK so 'Deleted' rows survive student removal
    -- Names are copied onto the row rather than joined from Student, so log
    -- entries still read correctly after the student is removed or renamed.
    FirstName   TEXT NOT NULL,
    LastName    TEXT NOT NULL,
    Action      TEXT NOT NULL,         -- 'Added' | 'Checked In' | 'Checked Out' | 'Deleted'
    Timestamp   INTEGER NOT NULL,      -- unix epoch seconds; integer -> fast range scans

    -- Name typed by the parent/authorized adult signing the student in or out.
    -- NULL where it does not apply ('Added'/'Deleted' rows) or was not entered.
    AuthorizedAdult TEXT
);

-- Fast "recent"/"today" range queries, including the History window's date
-- bounds.
CREATE INDEX IF NOT EXISTS idx_log_timestamp ON Log(Timestamp);
-- Fast "today's rows for a given student".
CREATE INDEX IF NOT EXISTS idx_log_student_ts ON Log(StudentID, Timestamp);
