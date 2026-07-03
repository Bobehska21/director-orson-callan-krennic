// Package store is the local durable persistence layer. It backs three
// concerns over one SQLite file: the crash-safe work queue, the result store
// (shown in the UI), and per-day cost accounting for the budget gate. Pure-Go
// driver (modernc.org/sqlite) keeps the agent a single CGO-free binary.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/acme/krennic/internal/model"
	_ "modernc.org/sqlite"
)

// Queue states.
const (
	StatePending    = "pending"
	StatePublishing = "publishing"
	StateAnalyzing  = "analyzing"
	StateDone       = "done"
	StateFailed     = "failed"
)

// Store owns the SQLite connection.
type Store struct {
	db    *sql.DB
	nowFn func() time.Time
}

// Open opens (creating if needed) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // single writer keeps things simple and lock-free
	s := &Store{db: db, nowFn: time.Now}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS queue (
			change_id  TEXT PRIMARY KEY,
			state      TEXT NOT NULL,
			event      TEXT NOT NULL,
			attempts   INTEGER NOT NULL DEFAULT 0,
			enqueued_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS records (
			change_id  TEXT PRIMARY KEY,
			event      TEXT NOT NULL,
			triage     TEXT,
			review     TEXT,
			status     TEXT NOT NULL,
			error      TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS dedup (
			content_hash TEXT PRIMARY KEY,
			change_id    TEXT NOT NULL,
			seen_at      TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS costs (
			day TEXT PRIMARY KEY,
			usd REAL NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_records_updated ON records(updated_at DESC)`,
		// Outbox: reports awaiting delivery to the central hub. Guarantees no
		// audit record is lost if the hub is temporarily unreachable.
		`CREATE TABLE IF NOT EXISTS outbox (
			report_id  TEXT PRIMARY KEY,
			change_id  TEXT NOT NULL,
			payload    TEXT NOT NULL,
			attempts   INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) now() string { return s.nowFn().UTC().Format(time.RFC3339Nano) }

// SeenRecently reports whether contentHash was enqueued within the window
// (dedup: a formatter reverting a change produces the same hash → skip).
func (s *Store) SeenRecently(contentHash string, window time.Duration) (bool, error) {
	var seenAt string
	err := s.db.QueryRow(`SELECT seen_at FROM dedup WHERE content_hash=?`, contentHash).Scan(&seenAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	t, perr := time.Parse(time.RFC3339Nano, seenAt)
	if perr != nil {
		return false, nil
	}
	return s.nowFn().Sub(t) < window, nil
}

// Enqueue persists the event as pending work and upserts its record + dedup.
func (s *Store) Enqueue(ev model.ChangeEvent) error {
	blob, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	now := s.now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO queue(change_id,state,event,attempts,enqueued_at,updated_at)
		VALUES(?,?,?,0,?,?)
		ON CONFLICT(change_id) DO UPDATE SET state=excluded.state, event=excluded.event, updated_at=excluded.updated_at`,
		ev.ChangeID, StatePending, string(blob), now, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO records(change_id,event,status,updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(change_id) DO UPDATE SET event=excluded.event, status=excluded.status, updated_at=excluded.updated_at`,
		ev.ChangeID, string(blob), StatePending, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO dedup(content_hash,change_id,seen_at)
		VALUES(?,?,?)
		ON CONFLICT(content_hash) DO UPDATE SET change_id=excluded.change_id, seen_at=excluded.seen_at`,
		ev.ContentHash, ev.ChangeID, now); err != nil {
		return err
	}
	return tx.Commit()
}

// Claim atomically pulls the oldest pending event and marks it analyzing.
// Returns ok=false when the queue is empty.
func (s *Store) Claim() (ev model.ChangeEvent, ok bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ev, false, err
	}
	defer tx.Rollback()

	var id, blob string
	row := tx.QueryRow(`SELECT change_id,event FROM queue WHERE state=? ORDER BY enqueued_at LIMIT 1`, StatePending)
	if err := row.Scan(&id, &blob); err != nil {
		if err == sql.ErrNoRows {
			return ev, false, nil
		}
		return ev, false, err
	}
	now := s.now()
	if _, err := tx.Exec(`UPDATE queue SET state=?, attempts=attempts+1, updated_at=? WHERE change_id=?`,
		StateAnalyzing, now, id); err != nil {
		return ev, false, err
	}
	if _, err := tx.Exec(`UPDATE records SET status=?, updated_at=? WHERE change_id=?`, StateAnalyzing, now, id); err != nil {
		return ev, false, err
	}
	if err := tx.Commit(); err != nil {
		return ev, false, err
	}
	if err := json.Unmarshal([]byte(blob), &ev); err != nil {
		return ev, false, err
	}
	return ev, true, nil
}

// Complete marks a queue item done and removes it from the queue.
func (s *Store) Complete(changeID string) error {
	now := s.now()
	if _, err := s.db.Exec(`DELETE FROM queue WHERE change_id=?`, changeID); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE records SET status=?, updated_at=? WHERE change_id=?`, StateDone, now, changeID)
	return err
}

// Fail marks a queue item failed (kept in queue for inspection) and records the error.
func (s *Store) Fail(changeID, errMsg string) error {
	now := s.now()
	if _, err := s.db.Exec(`UPDATE queue SET state=?, updated_at=? WHERE change_id=?`, StateFailed, now, changeID); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE records SET status=?, error=?, updated_at=? WHERE change_id=?`,
		StateFailed, errMsg, now, changeID)
	return err
}

// ResetInflight moves interrupted (analyzing/publishing) items back to pending
// on startup so a crash/reboot never drops work.
func (s *Store) ResetInflight() (int, error) {
	res, err := s.db.Exec(`UPDATE queue SET state=? WHERE state IN(?,?)`, StatePending, StateAnalyzing, StatePublishing)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SaveTriage stores the triage result on a record.
func (s *Store) SaveTriage(changeID string, tr *model.TriageResult) error {
	blob, _ := json.Marshal(tr)
	_, err := s.db.Exec(`UPDATE records SET triage=?, updated_at=? WHERE change_id=?`, string(blob), s.now(), changeID)
	return err
}

// SaveReview stores the review result on a record.
func (s *Store) SaveReview(changeID string, rv *model.ReviewResult) error {
	blob, _ := json.Marshal(rv)
	_, err := s.db.Exec(`UPDATE records SET review=?, updated_at=? WHERE change_id=?`, string(blob), s.now(), changeID)
	return err
}

// GetRecord returns the record for a change id.
func (s *Store) GetRecord(changeID string) (*model.Record, error) {
	row := s.db.QueryRow(`SELECT event,triage,review,status,error,updated_at FROM records WHERE change_id=?`, changeID)
	return scanRecord(row)
}

// RecentRecords returns the most recently updated records.
func (s *Store) RecentRecords(limit int) ([]model.Record, error) {
	rows, err := s.db.Query(`SELECT event,triage,review,status,error,updated_at FROM records ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

// PendingCount returns the number of queued (pending) items — the queue depth metric.
func (s *Store) PendingCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM queue WHERE state=?`, StatePending).Scan(&n)
	return n, err
}

// AddCost accumulates spend for the given day (YYYY-MM-DD).
func (s *Store) AddCost(day string, usd float64) error {
	_, err := s.db.Exec(`INSERT INTO costs(day,usd) VALUES(?,?)
		ON CONFLICT(day) DO UPDATE SET usd = usd + excluded.usd`, day, usd)
	return err
}

// SpendForDay returns accumulated spend for the given day.
func (s *Store) SpendForDay(day string) (float64, error) {
	var usd float64
	err := s.db.QueryRow(`SELECT usd FROM costs WHERE day=?`, day).Scan(&usd)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return usd, err
}

// PruneOlderThan deletes records/dedup older than the cutoff (retention).
func (s *Store) PruneOlderThan(cutoff time.Time) (int, error) {
	c := cutoff.UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`DELETE FROM records WHERE updated_at < ? AND status IN(?,?)`, c, StateDone, StateFailed)
	if err != nil {
		return 0, err
	}
	_, _ = s.db.Exec(`DELETE FROM dedup WHERE seen_at < ?`, c)
	n, _ := res.RowsAffected()
	return int(n), nil
}

// --- Hub outbox ---

// OutboxItem is one report awaiting delivery to the hub.
type OutboxItem struct {
	ReportID string
	ChangeID string
	Payload  []byte
	Attempts int
}

// EnqueueOutbox stores a report for delivery (idempotent by report_id).
func (s *Store) EnqueueOutbox(reportID, changeID string, payload []byte) error {
	_, err := s.db.Exec(`INSERT INTO outbox(report_id,change_id,payload,attempts,created_at)
		VALUES(?,?,?,0,?)
		ON CONFLICT(report_id) DO UPDATE SET payload=excluded.payload`,
		reportID, changeID, string(payload), s.now())
	return err
}

// ListOutbox returns up to limit pending reports, oldest first.
func (s *Store) ListOutbox(limit int) ([]OutboxItem, error) {
	rows, err := s.db.Query(`SELECT report_id,change_id,payload,attempts FROM outbox ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		var payload string
		if err := rows.Scan(&it.ReportID, &it.ChangeID, &payload, &it.Attempts); err != nil {
			return nil, err
		}
		it.Payload = []byte(payload)
		out = append(out, it)
	}
	return out, rows.Err()
}

// DeleteOutbox removes a delivered report.
func (s *Store) DeleteOutbox(reportID string) error {
	_, err := s.db.Exec(`DELETE FROM outbox WHERE report_id=?`, reportID)
	return err
}

// IncOutboxAttempts records a failed delivery attempt.
func (s *Store) IncOutboxAttempts(reportID string) error {
	_, err := s.db.Exec(`UPDATE outbox SET attempts=attempts+1 WHERE report_id=?`, reportID)
	return err
}

// OutboxDepth returns the number of undelivered reports.
func (s *Store) OutboxDepth() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&n)
	return n, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(sc scanner) (*model.Record, error) {
	var eventBlob string
	var triageBlob, reviewBlob, errStr sql.NullString
	var status, updated string
	if err := sc.Scan(&eventBlob, &triageBlob, &reviewBlob, &status, &errStr, &updated); err != nil {
		return nil, err
	}
	rec := &model.Record{Status: status}
	_ = json.Unmarshal([]byte(eventBlob), &rec.Event)
	if triageBlob.Valid && triageBlob.String != "" {
		var tr model.TriageResult
		if json.Unmarshal([]byte(triageBlob.String), &tr) == nil {
			rec.Triage = &tr
		}
	}
	if reviewBlob.Valid && reviewBlob.String != "" {
		var rv model.ReviewResult
		if json.Unmarshal([]byte(reviewBlob.String), &rv) == nil {
			rec.Review = &rv
		}
	}
	if errStr.Valid {
		rec.Error = errStr.String
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		rec.UpdatedAt = t
	}
	return rec, nil
}
