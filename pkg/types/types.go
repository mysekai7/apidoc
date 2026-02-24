package types

import "time"

// Session records one imported traffic session.
type Session struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Scenario  string    `json:"scenario"`
	Host      string    `json:"host"`
	LogCount  int       `json:"log_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Status    string    `json:"status"`
}

// TrafficLog is one request/response pair.
type TrafficLog struct {
	ID                  int64               `json:"id"`
	SessionID           string              `json:"session_id"`
	Seq                 int                 `json:"seq"`
	Timestamp           time.Time           `json:"timestamp"`
	Method              string              `json:"method"`
	Host                string              `json:"host"`
	Path                string              `json:"path"`
	QueryParams         map[string][]string `json:"query_params,omitempty"`
	RequestHeaders      map[string]string   `json:"request_headers,omitempty"`
	RequestBody         string              `json:"request_body,omitempty"`
	RequestBodyEncoding string              `json:"request_body_encoding,omitempty"`
	ContentType         string              `json:"content_type,omitempty"`
	StatusCode          int                 `json:"status_code"`
	ResponseHeaders     map[string]string   `json:"response_headers,omitempty"`
	ResponseBody        string              `json:"response_body,omitempty"`
	ResponseContentType string              `json:"response_content_type,omitempty"`
	LatencyMs           int64               `json:"latency_ms"`
	CallCount           int                 `json:"call_count,omitempty"`
}

// LLMCache stores one batch generation output.
type LLMCache struct {
	SessionID  string    `json:"session_id"`
	BatchIndex int       `json:"batch_index"`
	BatchKey   string    `json:"batch_key"`
	Status     string    `json:"status"`
	RawOutput  string    `json:"raw_output"`
	Model      string    `json:"model"`
	TokensUsed int       `json:"tokens_used"`
	ErrorMsg   string    `json:"error_msg"`
	CreatedAt  time.Time `json:"created_at"`
}
