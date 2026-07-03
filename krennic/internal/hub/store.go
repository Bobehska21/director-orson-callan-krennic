// Package hub is the central collection point. Every Krennic agent reports its
// attributed changes here. The audit log is append-only and hash-chained so the
// record of "who changed what, where, when" cannot be silently tampered with.
package hub

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/acme/krennic/internal/audit"
	_ "modernc.org/sqlite"
)

// Store is the hub's append-only audit database.
type Store struct {
	db    *sql.DB
	nowFn func() time.Time
}

// Entry is one row of the audit log as returned to the dashboard/CLI.
type Entry struct {
	Seq        int64           `json:"seq"`
	ReceivedAt string          `json:"received_at"`
	PrevHash   string          `json:"prev_hash"`
	EntryHash  string          `json:"entry_hash"`
	Report     json.RawMessage `json:"report"`
}

// OpenStore opens/creates the hub database at path.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, nowFn: time.Now}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS audit (
		seq         INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id   TEXT UNIQUE NOT NULL,
		change_id   TEXT NOT NULL,
		reported_at TEXT,
		received_at TEXT NOT NULL,
		user_slug   TEXT, git_name TEXT, git_email TEXT, machine TEXT, os_user TEXT,
		repo TEXT, branch TEXT, head_sha TEXT,
		relevance TEXT, verdict TEXT, findings INTEGER, status TEXT,
		prev_hash  TEXT NOT NULL,
		entry_hash TEXT NOT NULL,
		payload    TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_audit_user ON audit(user_slug)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_repo ON audit(repo)`,
	} {
		if _, err := s.db.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Append records a report. Idempotent by report_id (agent retries are safe).
// Returns the sequence number and entry hash.
func (s *Store) Append(r audit.Report) (seq int64, entryHash string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	// Idempotency: if we already have this report_id, return it unchanged.
	var existSeq int64
	var existHash string
	if err := tx.QueryRow(`SELECT seq, entry_hash FROM audit WHERE report_id=?`, r.ReportID).
		Scan(&existSeq, &existHash); err == nil {
		return existSeq, existHash, tx.Commit()
	}

	prevHash := audit.GenesisHash
	_ = tx.QueryRow(`SELECT entry_hash FROM audit ORDER BY seq DESC LIMIT 1`).Scan(&prevHash)

	payload := r.Payload()
	entryHash = audit.ChainHash(prevHash, payload)
	received := s.nowFn().UTC().Format(time.RFC3339Nano)

	res, err := tx.Exec(`INSERT INTO audit(
		report_id,change_id,reported_at,received_at,
		user_slug,git_name,git_email,machine,os_user,
		repo,branch,head_sha,relevance,verdict,findings,status,
		prev_hash,entry_hash,payload)
		VALUES(?,?,?,?, ?,?,?,?,?, ?,?,?,?,?,?,?, ?,?,?)`,
		r.ReportID, r.ChangeID, r.ReportedAt.Format(time.RFC3339Nano), received,
		r.Developer.UserSlug, r.Developer.GitName, r.Developer.GitEmail, r.Developer.Machine, r.Developer.OSUser,
		r.Repo, r.Branch, r.HeadSHA, r.Relevance, r.Verdict, r.FindingsCount, r.Status,
		prevHash, entryHash, string(payload))
	if err != nil {
		return 0, "", err
	}
	seq, _ = res.LastInsertId()
	return seq, entryHash, tx.Commit()
}

// Feed returns the most recent entries, optionally filtered by user or repo.
func (s *Store) Feed(limit int, user, repo string) ([]Entry, error) {
	q := `SELECT seq,received_at,prev_hash,entry_hash,payload FROM audit`
	var args []any
	var conds []string
	if user != "" {
		conds = append(conds, "user_slug=?")
		args = append(args, user)
	}
	if repo != "" {
		conds = append(conds, "repo=?")
		args = append(args, repo)
	}
	for i, c := range conds {
		if i == 0 {
			q += " WHERE " + c
		} else {
			q += " AND " + c
		}
	}
	q += " ORDER BY seq DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var payload string
		if err := rows.Scan(&e.Seq, &e.ReceivedAt, &e.PrevHash, &e.EntryHash, &payload); err != nil {
			return nil, err
		}
		e.Report = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyResult reports the integrity of the audit chain.
type VerifyResult struct {
	OK        bool   `json:"ok"`
	Count     int    `json:"count"`
	BrokenSeq int64  `json:"broken_seq,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Verify recomputes the hash chain from the start and reports the first break.
func (s *Store) Verify() (VerifyResult, error) {
	rows, err := s.db.Query(`SELECT seq,prev_hash,entry_hash,payload FROM audit ORDER BY seq ASC`)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rows.Close()
	prev := audit.GenesisHash
	count := 0
	for rows.Next() {
		var seq int64
		var storedPrev, storedHash, payload string
		if err := rows.Scan(&seq, &storedPrev, &storedHash, &payload); err != nil {
			return VerifyResult{}, err
		}
		if storedPrev != prev {
			return VerifyResult{OK: false, Count: count, BrokenSeq: seq,
				Detail: "prev_hash nenavazuje — záznam byl smazán nebo přeuspořádán"}, nil
		}
		want := audit.ChainHash(prev, []byte(payload))
		if want != storedHash {
			return VerifyResult{OK: false, Count: count, BrokenSeq: seq,
				Detail: "entry_hash nesedí — obsah záznamu byl změněn"}, nil
		}
		prev = storedHash
		count++
	}
	return VerifyResult{OK: true, Count: count}, rows.Err()
}

// Stats returns basic counters for the dashboard.
func (s *Store) Stats() (map[string]any, error) {
	var total, users, repos int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&total)
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT user_slug) FROM audit`).Scan(&users)
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT repo) FROM audit`).Scan(&repos)
	return map[string]any{"total": total, "developers": users, "repos": repos}, nil
}
