package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigCLIOverridesNATSEnvironment(t *testing.T) {
	env := map[string]string{
		"NATS_URL":  "tls://env.example:4222",
		"NATS_CA":   "/env/ca.pem",
		"NATS_NKEY": "SUAENVKEY",
	}
	getenv := func(name string) string { return env[name] }

	cfg, err := parseConfig([]string{
		"--subject", "barn.status",
		"--url", "tls://cli.example:4222",
		"--ca", "/cli/ca.pem",
		"--key", "SUACLIKEY",
	}, getenv)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	want := config{
		Subject: "barn.status",
		URL:     "tls://cli.example:4222",
		CA:      "/cli/ca.pem",
		NKey:    "SUACLIKEY",
	}
	if cfg != want {
		t.Fatalf("parseConfig() = %+v, want %+v", cfg, want)
	}
}

func TestParseConfigUsesNATSEnvironmentFallbacks(t *testing.T) {
	env := map[string]string{
		"NATS_URL":  "tls://env.example:4222",
		"NATS_CA":   "/env/ca.pem",
		"NATS_NKEY": "SUAENVKEY",
	}
	cfg, err := parseConfig([]string{"--subject", "barn.status"}, func(name string) string {
		return env[name]
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.URL != env["NATS_URL"] || cfg.CA != env["NATS_CA"] || cfg.NKey != env["NATS_NKEY"] {
		t.Fatalf("parseConfig() did not use NATS environment: %+v", cfg)
	}
}

func TestStatusResponseIsJSONWithHealthyStatusTimestampAndHostname(t *testing.T) {
	before := time.Now().UTC()
	payload, err := json.Marshal(statusResponse{
		Timestamp: time.Now().UTC(),
		Status:    "healthy",
		Hostname:  "test-server",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	after := time.Now().UTC()

	var got statusResponse
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.Status != "healthy" {
		t.Fatalf("status = %q, want healthy", got.Status)
	}
	if got.Hostname != "test-server" {
		t.Fatalf("hostname = %q, want test-server", got.Hostname)
	}
	if got.Timestamp.Before(before) || got.Timestamp.After(after) {
		t.Fatalf("timestamp %v outside response interval [%v, %v]", got.Timestamp, before, after)
	}
	payloadText := string(payload)
	if !strings.Contains(payloadText, `"timestamp"`) ||
		!strings.Contains(payloadText, `"status":"healthy"`) ||
		!strings.Contains(payloadText, `"hostname":"test-server"`) {
		t.Fatalf("payload = %s, want status, timestamp, and hostname fields", payload)
	}
}

func TestJSONLoggerWritesTimestampedEvents(t *testing.T) {
	var output bytes.Buffer
	logger := &jsonLogger{out: &output}
	before := time.Now().UTC()

	for _, event := range []string{"startup", "response", "shutdown"} {
		if err := logger.write(event, "barn.status"); err != nil {
			t.Fatalf("jsonLogger.write(%q) error = %v", event, err)
		}
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("log lines = %d, want 3", len(lines))
	}
	after := time.Now().UTC()
	for i, wantEvent := range []string{"startup", "response", "shutdown"} {
		var got logEvent
		if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
			t.Fatalf("log line %d is not JSON: %v", i, err)
		}
		if got.Event != wantEvent {
			t.Errorf("log line %d event = %q, want %q", i, got.Event, wantEvent)
		}
		if got.Subject != "barn.status" {
			t.Errorf("log line %d subject = %q, want barn.status", i, got.Subject)
		}
		if got.Timestamp.Before(before) || got.Timestamp.After(after) {
			t.Errorf("log line %d timestamp %v outside [%v, %v]", i, got.Timestamp, before, after)
		}
	}
}

func TestReadNKeySeedAcceptsSeedFileOrSeedText(t *testing.T) {
	seedFile := filepath.Join(t.TempDir(), "user.nk")
	if err := os.WriteFile(seedFile, []byte("  seed-from-file\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	fromFile, err := readNKeySeed(seedFile)
	if err != nil {
		t.Fatalf("readNKeySeed(file) error = %v", err)
	}
	if string(fromFile) != "seed-from-file" {
		t.Fatalf("readNKeySeed(file) = %q, want trimmed seed", fromFile)
	}

	fromText, err := readNKeySeed("  seed-from-text\n")
	if err != nil {
		t.Fatalf("readNKeySeed(text) error = %v", err)
	}
	if string(fromText) != "seed-from-text" {
		t.Fatalf("readNKeySeed(text) = %q, want trimmed seed", fromText)
	}
}

func TestMakeTLSConfigRejectsMissingCA(t *testing.T) {
	_, err := makeTLSConfig(filepath.Join(t.TempDir(), "missing.pem"))
	if err == nil {
		t.Fatal("makeTLSConfig() error = nil, want missing CA error")
	}
	if !os.IsNotExist(err) && !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("makeTLSConfig() error = %v, want missing-file error", err)
	}
}
