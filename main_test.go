package main

import (
	"reflect"
	"testing"
)

func TestExtractFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		flag     string
		wantVal  string
		wantRest []string
	}{
		{"found", []string{"--log-dir", "/tmp", "rest"}, "--log-dir", "/tmp", []string{"rest"}},
		{"not found", []string{"--model", "sonnet"}, "--log-dir", "", []string{"--model", "sonnet"}},
		{"no value", []string{"--log-dir"}, "--log-dir", "", []string{"--log-dir"}},
		{"multiple flags", []string{"--a", "1", "--b", "2"}, "--b", "2", []string{"--a", "1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotVal, gotRest := extractFlag(tc.args, tc.flag)
			if gotVal != tc.wantVal {
				t.Errorf("value = %q, want %q", gotVal, tc.wantVal)
			}
			if !reflect.DeepEqual(gotRest, tc.wantRest) {
				t.Errorf("rest = %v, want %v", gotRest, tc.wantRest)
			}
		})
	}
}

func TestLogPath(t *testing.T) {
	got := logPath()
	if got == "" || len(got) < 5 {
		t.Errorf("logPath() = %q, expected non-empty path", got)
	}
}

func TestOpenLogFile(t *testing.T) {
	got, err := openLogFile()
	if err != nil {
		t.Fatalf("openLogFile() error: %v", err)
	}
	got.Close()
}