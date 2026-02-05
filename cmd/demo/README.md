# demo

Demo mode entry point that runs the application with a mock RDS API server.

## Usage

```bash
# run directly
go run ./cmd/demo
```

## Flags

When running directly (not via Docker):

| Flag            | Default | Description                               |
| --------------- | ------- | ----------------------------------------- |
| `-port`         | 8080    | HTTP server port                          |
| `-mock-port`    | 9080    | Mock RDS API server port                  |
| `-base-wait`    | 500     | Base wait time (ms) for state transitions |
| `-random-range` | 200     | Random additional wait (ms)               |
| `-fast`         | false   | Fast mode (minimal waits)                 |
| `-verbose`      | false   | Verbose logging                           |

## Demo Clusters

The mock server is seeded with test clusters:

- `demo-single` - single instance cluster
- `demo-multi` - cluster with 3 instances
- `demo-autoscaled` - cluster with 4 instances (2 autoscaled)
- `demo-upgrade` - cluster ready for engine upgrade

## Endpoints

- `http://localhost:8080` - Web UI and API
- `http://localhost:9080` - Mock RDS API
- `http://localhost:9080/mock/state` - View/modify mock state
