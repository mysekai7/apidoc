package har

import (
	"path/filepath"
	"testing"
)

func TestParseNormalHAR(t *testing.T) {
	logs, err := Parse(filepath.Join("..", "..", "testdata", "sample.har"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0].Seq != 1 || logs[1].Seq != 2 {
		t.Fatalf("seq not assigned")
	}
	if len(logs[1].QueryParams["id"]) != 2 {
		t.Fatalf("expected multi-value query params")
	}
}

func TestParseBase64Body(t *testing.T) {
	logs, err := Parse(filepath.Join("..", "..", "testdata", "base64-body.har"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log")
	}
	if logs[0].RequestBodyEncoding != "omitted" {
		t.Fatalf("expected omitted for binary body, got %s", logs[0].RequestBodyEncoding)
	}
	if logs[0].ResponseBody != "{\"ok\":true}" {
		t.Fatalf("unexpected decoded response body: %s", logs[0].ResponseBody)
	}
}

func TestParseEmptyHAR(t *testing.T) {
	logs, err := Parse(filepath.Join("..", "..", "testdata", "empty.har"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected empty logs")
	}
}

func TestParseMalformedJSON(t *testing.T) {
	_, err := Parse(filepath.Join("..", "..", "testdata", "not-exist.har"))
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}
