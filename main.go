package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

const defaultNATSURL = "nats://127.0.0.1:4222"
const version = "0.1"

type config struct {
	Subject string
	URL     string
	CA      string
	NKey    string
}

type statusResponse struct {
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
	Hostname  string    `json:"hostname"`
}

type logEvent struct {
	Event     string    `json:"event"`
	Version   string    `json:"version,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Subject   string    `json:"subject,omitempty"`
	Channel   string    `json:"channel,omitempty"`
}

type jsonLogger struct {
	mu  sync.Mutex
	out io.Writer
}

func (l *jsonLogger) write(event, subject, channel string) error {
	eventPayload := logEvent{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Subject:   subject,
		Channel:   channel,
	}
	if event == "startup" {
		eventPayload.Version = version
	}
	payload, err := json.Marshal(eventPayload)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.out.Write(payload)
	return err
}

func responseChannel(reply, destination string) string {
	if reply != "" {
		return "inbox"
	}
	return destination
}

func main() {
	cfg, err := parseConfig(os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	defaults := config{
		URL:  envOrDefault(getenv, "NATS_URL", defaultNATSURL),
		CA:   getenv("NATS_CA"),
		NKey: getenv("NATS_NKEY"),
	}

	flags := flag.NewFlagSet("barnhub-status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: barnhub-status --subject SUBJECT --key NKEY [options]")
		flags.PrintDefaults()
	}

	var cfg config = defaults
	flags.StringVar(&cfg.Subject, "subject", "", "NATS request subject to serve (required)")
	flags.StringVar(&cfg.URL, "url", defaults.URL, "NATS server URL (env: NATS_URL)")
	flags.StringVar(&cfg.CA, "ca", defaults.CA, "CA certificate file for NATS TLS (env: NATS_CA)")
	flags.StringVar(&cfg.NKey, "key", defaults.NKey, "NKey seed or seed-file path for NATS authentication (env: NATS_NKEY)")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if cfg.Subject == "" {
		return config{}, errors.New("--subject is required")
	}
	if cfg.URL == "" {
		return config{}, errors.New("--url or NATS_URL must not be empty")
	}
	if cfg.NKey == "" {
		return config{}, errors.New("--key or NATS_NKEY is required")
	}
	return cfg, nil
}

func envOrDefault(getenv func(string) string, name, fallback string) string {
	if value := getenv(name); value != "" {
		return value
	}
	return fallback
}

func readNKeySeed(value string) ([]byte, error) {
	if seed, err := os.ReadFile(value); err == nil {
		return bytes.TrimSpace(seed), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read NATS NKey file %q: %w", value, err)
	}
	return []byte(strings.TrimSpace(value)), nil
}

func responseSubject(listeningSubject, reply, hostname string) string {
	if reply != "" {
		return reply
	}
	return listeningSubject + "." + hostname
}

func run(cfg config) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get server hostname: %w", err)
	}

	seedData, err := readNKeySeed(cfg.NKey)
	if err != nil {
		return err
	}
	seed, err := nkeys.FromSeed(seedData)
	if err != nil {
		return fmt.Errorf("parse NATS NKey: %w", err)
	}
	publicKey, err := seed.PublicKey()
	if err != nil {
		return fmt.Errorf("derive NATS public key: %w", err)
	}

	opts := []nats.Option{
		nats.Name("barnhub-status"),
		nats.Nkey(publicKey, func(nonce []byte) ([]byte, error) {
			return seed.Sign(nonce)
		}),
	}
	if cfg.CA != "" {
		tlsConfig, tlsErr := makeTLSConfig(cfg.CA)
		if tlsErr != nil {
			return tlsErr
		}
		opts = append(opts, nats.Secure(tlsConfig))
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}
	defer nc.Drain()

	logger := &jsonLogger{out: os.Stdout}
	_, err = nc.Subscribe(cfg.Subject, func(msg *nats.Msg) {
		destination := responseSubject(cfg.Subject, msg.Reply, hostname)
		payload, marshalErr := json.Marshal(statusResponse{
			Version:   version,
			Timestamp: time.Now().UTC(),
			Status:    "healthy",
			Hostname:  hostname,
		})
		if marshalErr != nil {
			return
		}
		if publishErr := nc.Publish(destination, payload); publishErr != nil {
			return
		}
		if destination[:6] == "_INBOX" {
			destination = "INBOX"
		}
		_ = logger.write("response", destination, responseChannel(msg.Reply, destination))
	})
	if err != nil {
		return fmt.Errorf("subscribe to %q: %w", cfg.Subject, err)
	}
	if err := nc.Flush(); err != nil {
		return fmt.Errorf("flush NATS subscription: %w", err)
	}
	if err := logger.write("startup", cfg.Subject, ""); err != nil {
		return fmt.Errorf("write startup log: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	_ = logger.write("shutdown", cfg.Subject, "")
	return nil
}

func makeTLSConfig(caFile string) (*tls.Config, error) {
	pool := x509.NewCertPool()
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read NATS CA file %q: %w", caFile, err)
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("NATS CA file %q contains no PEM certificates", caFile)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}, nil
}
