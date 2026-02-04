# cmd

Entry points for the RDS Maintenance Machine.

| Directory | Description                                      | Make Command       |
| --------- | ------------------------------------------------ | ------------------ |
| server/   | http server (primary, recommended for local use) | `make server`      |
| demo/     | demo mode with mock rds api server               | `make demo`        |
| verify/   | integration test harness for mock server         | `make test-verify` |
