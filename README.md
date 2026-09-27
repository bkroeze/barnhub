# barnhub-status

`barnhub-status` is a small NATS request/reply daemon. It listens on a configured subject and replies with a JSON health payload:

```json
{"timestamp":"2026-09-26T12:34:56.789Z","status":"healthy","hostname":"barnhub-01"}
```

The timestamp is generated for each request in UTC. The hostname identifies the machine running `barnhub-status`.

## Development

List the available project commands:

```sh
just
```

Run the checks used before committing:

```sh
just check
```

Build, test, format, inspect dependencies, or remove generated artifacts with
`just build`, `just test`, `just fmt`, `just deps`, and `just clean`.

## Run

Build and run it with the request subject and NKey seed or seed-file path:

```sh
just build
just run \
  --subject barn.status \
  --url tls://nats.barnlab \
  --ca /etc/barnlab/nats/ca.pem \
  --key /etc/barnlab/nats/user.nk
```

`--subject` is the message key: requests received on that NATS subject are answered. Requests without a reply inbox are ignored, as required for request/reply semantics.

The NATS connection settings also accept the standard NATS environment variable names. A command-line value takes precedence over its environment value:

| CLI flag | Environment variable | Default |
| --- | --- | --- |
| `--url` | `NATS_URL` | `nats://127.0.0.1:4222` |
| `--ca` | `NATS_CA` | system TLS roots; TLS is enabled when this flag is set |
| `--key` | `NATS_NKEY` | required; seed text or path to a seed file |

The NKey value may be the private seed text or a path to a file containing the private seed (the standard `NATS_NKEY` form). It is used only to sign the NATS server nonce; the corresponding public key is sent as the NATS identity.

The process stays running until it receives `SIGINT` or `SIGTERM`, making it suitable for a service manager.

## Request an answer

Using the NATS CLI or another request client, send a request to the configured subject. The response is JSON containing `status: "healthy"`, a current `timestamp`, and the running server's `hostname`.
