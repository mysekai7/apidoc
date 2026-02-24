package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yourorg/apidoc/pkg/types"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &SQLiteStore{db: db}
	if err := s.Init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Init() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			scenario TEXT NOT NULL,
			host TEXT NOT NULL,
			log_count INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS traffic_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			timestamp DATETIME NOT NULL,
			method TEXT NOT NULL,
			host TEXT NOT NULL,
			path TEXT NOT NULL,
			query_params TEXT,
			request_headers TEXT,
			request_body TEXT,
			request_body_encoding TEXT,
			content_type TEXT,
			status_code INTEGER NOT NULL,
			response_headers TEXT,
			response_body TEXT,
			response_content_type TEXT,
			latency_ms INTEGER NOT NULL,
			call_count INTEGER NOT NULL DEFAULT 1
		);`,
		`CREATE INDEX IF NOT EXISTS idx_traffic_session ON traffic_logs(session_id);`,
		`CREATE TABLE IF NOT EXISTS llm_cache (
			session_id TEXT NOT NULL,
			batch_index INTEGER NOT NULL,
			batch_key TEXT NOT NULL,
			status TEXT NOT NULL,
			raw_output TEXT NOT NULL,
			model TEXT NOT NULL,
			tokens_used INTEGER NOT NULL,
			error_msg TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			PRIMARY KEY(session_id, batch_index)
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) CreateSession(source, scenario, host string) (*types.Session, error) {
	now := time.Now().UTC()
	id, err := s.nextSessionID(now)
	if err != nil {
		return nil, err
	}
	sess := &types.Session{ID: id, Source: source, Scenario: scenario, Host: host, Status: "imported", CreatedAt: now, UpdatedAt: now}
	_, err = s.db.Exec(`INSERT INTO sessions(id,source,scenario,host,log_count,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		sess.ID, sess.Source, sess.Scenario, sess.Host, sess.LogCount, sess.Status, sess.CreatedAt, sess.UpdatedAt)
	return sess, err
}

func (s *SQLiteStore) nextSessionID(now time.Time) (string, error) {
	prefix := fmt.Sprintf("sess_%s_", now.Format("20060102"))
	rows, err := s.db.Query(`SELECT id FROM sessions WHERE id LIKE ?`, prefix+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	maxN := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		var n int
		_, _ = fmt.Sscanf(id, prefix+"%03d", &n)
		if n > maxN {
			maxN = n
		}
	}
	return fmt.Sprintf("%s%03d", prefix, maxN+1), nil
}

func (s *SQLiteStore) GetSession(id string) (*types.Session, error) {
	row := s.db.QueryRow(`SELECT id,source,scenario,host,log_count,status,created_at,updated_at FROM sessions WHERE id=?`, id)
	var out types.Session
	if err := row.Scan(&out.ID, &out.Source, &out.Scenario, &out.Host, &out.LogCount, &out.Status, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *SQLiteStore) UpdateSessionStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE sessions SET status=?, updated_at=? WHERE id=?`, status, time.Now().UTC(), id)
	return err
}

func (s *SQLiteStore) ListSessions() ([]types.Session, error) {
	rows, err := s.db.Query(`SELECT id,source,scenario,host,log_count,status,created_at,updated_at FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Session
	for rows.Next() {
		var s1 types.Session
		if err := rows.Scan(&s1.ID, &s1.Source, &s1.Scenario, &s1.Host, &s1.LogCount, &s1.Status, &s1.CreatedAt, &s1.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s1)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteSession(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM traffic_logs WHERE session_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM llm_cache WHERE session_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) SaveLogs(sessionID string, logs []types.TrafficLog) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO traffic_logs(session_id,seq,timestamp,method,host,path,query_params,request_headers,request_body,request_body_encoding,content_type,status_code,response_headers,response_body,response_content_type,latency_ms,call_count) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, l := range logs {
		qp, _ := json.Marshal(l.QueryParams)
		rh, _ := json.Marshal(l.RequestHeaders)
		respH, _ := json.Marshal(l.ResponseHeaders)
		callCount := l.CallCount
		if callCount == 0 {
			callCount = 1
		}
		if _, err := stmt.Exec(sessionID, l.Seq, l.Timestamp, l.Method, l.Host, l.Path, string(qp), string(rh), l.RequestBody, l.RequestBodyEncoding, l.ContentType, l.StatusCode, string(respH), l.ResponseBody, l.ResponseContentType, l.LatencyMs, callCount); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE sessions SET log_count=log_count+?, updated_at=? WHERE id=?`, len(logs), time.Now().UTC(), sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetLogs(sessionID string) ([]types.TrafficLog, error) {
	rows, err := s.db.Query(`SELECT id,session_id,seq,timestamp,method,host,path,query_params,request_headers,request_body,request_body_encoding,content_type,status_code,response_headers,response_body,response_content_type,latency_ms,call_count FROM traffic_logs WHERE session_id=? ORDER BY seq ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]types.TrafficLog, 0)
	for rows.Next() {
		var l types.TrafficLog
		var qpS, rhS, respHS string
		if err := rows.Scan(&l.ID, &l.SessionID, &l.Seq, &l.Timestamp, &l.Method, &l.Host, &l.Path, &qpS, &rhS, &l.RequestBody, &l.RequestBodyEncoding, &l.ContentType, &l.StatusCode, &respHS, &l.ResponseBody, &l.ResponseContentType, &l.LatencyMs, &l.CallCount); err != nil {
			return nil, err
		}
		if qpS != "" {
			_ = json.Unmarshal([]byte(qpS), &l.QueryParams)
		}
		if rhS != "" {
			_ = json.Unmarshal([]byte(rhS), &l.RequestHeaders)
		}
		if respHS != "" {
			_ = json.Unmarshal([]byte(respHS), &l.ResponseHeaders)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SaveBatchCache(cache *types.LLMCache) error {
	if cache.CreatedAt.IsZero() {
		cache.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO llm_cache(session_id,batch_index,batch_key,status,raw_output,model,tokens_used,error_msg,created_at)
	VALUES(?,?,?,?,?,?,?,?,?)
	ON CONFLICT(session_id,batch_index) DO UPDATE SET batch_key=excluded.batch_key,status=excluded.status,raw_output=excluded.raw_output,model=excluded.model,tokens_used=excluded.tokens_used,error_msg=excluded.error_msg,created_at=excluded.created_at`,
		cache.SessionID, cache.BatchIndex, cache.BatchKey, cache.Status, cache.RawOutput, cache.Model, cache.TokensUsed, cache.ErrorMsg, cache.CreatedAt)
	return err
}

func (s *SQLiteStore) GetBatchCaches(sessionID string) ([]types.LLMCache, error) {
	rows, err := s.db.Query(`SELECT session_id,batch_index,batch_key,status,raw_output,model,tokens_used,error_msg,created_at FROM llm_cache WHERE session_id=?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.LLMCache
	for rows.Next() {
		var c types.LLMCache
		if err := rows.Scan(&c.SessionID, &c.BatchIndex, &c.BatchKey, &c.Status, &c.RawOutput, &c.Model, &c.TokensUsed, &c.ErrorMsg, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BatchIndex < out[j].BatchIndex })
	return out, rows.Err()
}

func (s *SQLiteStore) GetFailedBatches(sessionID string) ([]types.LLMCache, error) {
	rows, err := s.db.Query(`SELECT session_id,batch_index,batch_key,status,raw_output,model,tokens_used,error_msg,created_at FROM llm_cache WHERE session_id=? AND status='failed' ORDER BY batch_index ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.LLMCache
	for rows.Next() {
		var c types.LLMCache
		if err := rows.Scan(&c.SessionID, &c.BatchIndex, &c.BatchKey, &c.Status, &c.RawOutput, &c.Model, &c.TokensUsed, &c.ErrorMsg, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ClearCaches(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM llm_cache WHERE session_id=?`, sessionID)
	return err
}

func (s *SQLiteStore) Close() error {
	if s.db == nil {
		return errors.New("store is nil")
	}
	return s.db.Close()
}
