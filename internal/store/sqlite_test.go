package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yourorg/apidoc/pkg/types"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "apidoc.db"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionAndLogsCRUD(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	sess, err := s.CreateSession("har", "login", "api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" {
		t.Fatalf("empty session id")
	}
	if err := s.SaveLogs(sess.ID, []types.TrafficLog{{Seq: 1, Timestamp: time.Now().UTC(), Method: "GET", Host: "api.example.com", Path: "/v1/me", QueryParams: map[string][]string{"a": {"1", "2"}}, RequestHeaders: map[string]string{"Accept": "application/json"}, RequestBodyEncoding: "plain", StatusCode: 200, ResponseHeaders: map[string]string{"Content-Type": "application/json"}, LatencyMs: 10}}); err != nil {
		t.Fatal(err)
	}
	logs, err := s.GetLogs(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Path != "/v1/me" {
		t.Fatalf("unexpected logs")
	}
	if got, err := s.GetSession(sess.ID); err != nil || got.LogCount != 1 {
		t.Fatalf("session log_count not updated: %+v err=%v", got, err)
	}
}

func TestCacheAndCascadeDelete(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	sess, _ := s.CreateSession("har", "flow", "api.example.com")
	_ = s.SaveLogs(sess.ID, []types.TrafficLog{{Seq: 1, Timestamp: time.Now().UTC(), Method: "GET", Host: "api.example.com", Path: "/v1", RequestBodyEncoding: "plain", StatusCode: 200, LatencyMs: 1}})
	_ = s.SaveBatchCache(&types.LLMCache{SessionID: sess.ID, BatchIndex: 0, BatchKey: "k", Status: "failed", Model: "gpt-4o", ErrorMsg: "boom"})

	failed, err := s.GetFailedBatches(sess.ID)
	if err != nil || len(failed) != 1 {
		t.Fatalf("failed batches mismatch")
	}
	if err := s.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if logs, _ := s.GetLogs(sess.ID); len(logs) != 0 {
		t.Fatalf("expected logs deleted")
	}
	if caches, _ := s.GetBatchCaches(sess.ID); len(caches) != 0 {
		t.Fatalf("expected caches deleted")
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()
	sess, _ := s.CreateSession("har", "concurrent", "api.example.com")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.SaveLogs(sess.ID, []types.TrafficLog{{Seq: i + 1, Timestamp: time.Now().UTC(), Method: "GET", Host: "api.example.com", Path: fmt.Sprintf("/v1/%d", i), RequestBodyEncoding: "plain", StatusCode: 200, LatencyMs: 1}})
		}(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.ListSessions()
		}()
	}
	wg.Wait()

	logs, err := s.GetLogs(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected logs")
	}
}
