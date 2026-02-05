# verify

Integration test harness for testing RDS maintenance operations against the mock
server.

## Usage

```bash
make test-verify           # run integration tests
make test-verify-verbose   # run with verbose output
```

Or run directly with options:

```bash
go run ./cmd/verify -scenarios fixtures/scenarios.yaml
go run ./cmd/verify -filter "instance type" -verbose
```

## Flags

| Flag         | Default                   | Description                      |
| ------------ | ------------------------- | -------------------------------- |
| `-scenarios` | `fixtures/scenarios.yaml` | Path to test scenarios file      |
| `-filter`    | (empty)                   | Run only scenarios matching name |
| `-verbose`   | false                     | Enable verbose output            |

## Scenarios

Test scenarios are defined in YAML files. Each scenario specifies:

- Initial cluster state
- Operation to perform
- Expected outcomes

## Environment

The harness loads configuration from `.env` or `.env.test` in the `cmd/verify/`
directory.
