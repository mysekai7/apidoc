package har

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/yourorg/apidoc/pkg/types"
)

type HARFile struct {
	Log struct {
		Entries []Entry `json:"entries"`
	} `json:"log"`
}

type Entry struct {
	StartedDateTime string `json:"startedDateTime"`
	Time            int64  `json:"time"`
	Request         struct {
		Method  string `json:"method"`
		URL     string `json:"url"`
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		PostData struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status  int `json:"status"`
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Content struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
		} `json:"content"`
	} `json:"response"`
}

func Parse(filePath string) ([]types.TrafficLog, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var hf HARFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return nil, err
	}
	logs := make([]types.TrafficLog, 0, len(hf.Log.Entries))
	for _, e := range hf.Log.Entries {
		ts, err := time.Parse(time.RFC3339Nano, e.StartedDateTime)
		if err != nil {
			return nil, fmt.Errorf("parse startedDateTime: %w", err)
		}
		u, err := url.Parse(e.Request.URL)
		if err != nil {
			return nil, fmt.Errorf("parse request url: %w", err)
		}
		reqHeaders := map[string]string{}
		for _, h := range e.Request.Headers {
			reqHeaders[h.Name] = h.Value
		}
		respHeaders := map[string]string{}
		for _, h := range e.Response.Headers {
			respHeaders[h.Name] = h.Value
		}

		reqBody, reqEnc := decodeBody(e.Request.PostData.Text, e.Request.PostData.Encoding, e.Request.PostData.MimeType)
		respBody, _ := decodeBody(e.Response.Content.Text, e.Response.Content.Encoding, e.Response.Content.MimeType)

		logs = append(logs, types.TrafficLog{
			Timestamp:           ts,
			Method:              strings.ToUpper(e.Request.Method),
			Host:                u.Host,
			Path:                u.Path,
			QueryParams:         u.Query(),
			RequestHeaders:      reqHeaders,
			RequestBody:         reqBody,
			RequestBodyEncoding: reqEnc,
			ContentType:         e.Request.PostData.MimeType,
			StatusCode:          e.Response.Status,
			ResponseHeaders:     respHeaders,
			ResponseBody:        respBody,
			ResponseContentType: e.Response.Content.MimeType,
			LatencyMs:           e.Time,
			CallCount:           1,
		})
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp.Before(logs[j].Timestamp)
	})
	for i := range logs {
		logs[i].Seq = i + 1
	}
	return logs, nil
}

func decodeBody(text, encoding, mimeType string) (string, string) {
	if text == "" {
		return "", "plain"
	}
	if isBinaryContentType(mimeType) {
		return "", "omitted"
	}
	if strings.EqualFold(encoding, "base64") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return "", "omitted"
		}
		return string(decoded), "base64"
	}
	return text, "plain"
}

func isBinaryContentType(mimeType string) bool {
	mt := strings.ToLower(mimeType)
	return strings.HasPrefix(mt, "image/") || strings.HasPrefix(mt, "audio/") || strings.HasPrefix(mt, "video/") || mt == "application/octet-stream"
}
