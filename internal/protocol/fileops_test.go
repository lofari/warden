package protocol

import (
	"bytes"
	"testing"
)

func TestFileRequestResponseRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	req := &FileRequest{ID: 42, Op: OpStat, Path: "/etc/hosts"}
	if err := WriteMessage(&buf, req); err != nil {
		t.Fatal(err)
	}

	raw, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := raw.(*FileRequest)
	if !ok {
		t.Fatalf("expected FileRequest, got %T", raw)
	}
	if got.ID != 42 || got.Op != OpStat || got.Path != "/etc/hosts" {
		t.Fatalf("mismatch: %+v", got)
	}

	resp := &FileResponse{
		ID:   42,
		Stat: &FileStat{Name: "hosts", Size: 200, Mode: 0o644, IsDir: false},
	}
	if err := WriteMessage(&buf, resp); err != nil {
		t.Fatal(err)
	}
	raw2, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	gotResp, ok := raw2.(*FileResponse)
	if !ok {
		t.Fatalf("expected FileResponse, got %T", raw2)
	}
	if gotResp.Stat.Name != "hosts" || gotResp.Stat.Size != 200 {
		t.Fatalf("stat mismatch: %+v", gotResp.Stat)
	}
}
