package hub

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/acme/krennic/internal/audit"
	"github.com/acme/krennic/internal/model"
)

func testReport(id, user, repo string) audit.Report {
	return audit.Report{
		ReportID:   id,
		ChangeID:   id,
		ReportedAt: time.Unix(1700000000, 0).UTC(),
		Developer:  model.Developer{UserSlug: user, GitName: user, Machine: "m1"},
		Repo:       repo,
		Branch:     "main",
		Files:      []string{"a.go"},
		Verdict:    "pass",
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestAppendIdempotentAndChain(t *testing.T) {
	st := openTestStore(t)
	if _, _, err := st.Append(testReport("c1", "alice", "svc")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Append(testReport("c2", "bob", "svc")); err != nil {
		t.Fatal(err)
	}
	// Re-appending the same report_id must not duplicate.
	if _, _, err := st.Append(testReport("c1", "alice", "svc")); err != nil {
		t.Fatal(err)
	}
	feed, err := st.Feed(10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 2 {
		t.Fatalf("expected 2 entries (idempotent), got %d", len(feed))
	}
	res, err := st.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Count != 2 {
		t.Fatalf("expected valid chain of 2, got %+v", res)
	}
}

func TestFeedFilter(t *testing.T) {
	st := openTestStore(t)
	_, _, _ = st.Append(testReport("c1", "alice", "svc-a"))
	_, _, _ = st.Append(testReport("c2", "bob", "svc-b"))
	feed, _ := st.Feed(10, "alice", "")
	if len(feed) != 1 {
		t.Fatalf("expected 1 entry for alice, got %d", len(feed))
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	st := openTestStore(t)
	_, _, _ = st.Append(testReport("c1", "alice", "svc"))
	_, _, _ = st.Append(testReport("c2", "bob", "svc"))
	_, _, _ = st.Append(testReport("c3", "carol", "svc"))

	// Simulate someone editing a past record's payload directly in the DB.
	if _, err := st.db.Exec(`UPDATE audit SET payload='{"tampered":true}' WHERE change_id='c2'`); err != nil {
		t.Fatal(err)
	}
	res, err := st.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("expected Verify to detect tampering")
	}
	if res.BrokenSeq != 2 {
		t.Errorf("expected break at seq 2, got %d", res.BrokenSeq)
	}
}

func TestVerifyDetectsDeletion(t *testing.T) {
	st := openTestStore(t)
	_, _, _ = st.Append(testReport("c1", "alice", "svc"))
	_, _, _ = st.Append(testReport("c2", "bob", "svc"))
	_, _, _ = st.Append(testReport("c3", "carol", "svc"))

	// Delete a middle record — the chain's prev_hash linkage must break.
	if _, err := st.db.Exec(`DELETE FROM audit WHERE change_id='c2'`); err != nil {
		t.Fatal(err)
	}
	res, _ := st.Verify()
	if res.OK {
		t.Fatal("expected Verify to detect deletion")
	}
}
