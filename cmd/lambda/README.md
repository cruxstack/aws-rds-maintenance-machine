# lambda

AWS Lambda entry point for the RDS Maintenance Machine.

## Status

**Experimental** - This entry point is not production-ready.

## Usage

This handler is designed for two use cases:

1. **API Gateway** - Serve HTTP requests via Lambda + API Gateway (untested)
2. **Step Functions** - Execute operations via AWS Step Functions orchestration

Both modes require `APP_EXPERIMENTAL_STEPFN_ENABLED=true` to be set.

## Step Functions

When used with Step Functions, each Lambda invocation executes a single step of
an operation. Step Functions handles the wait/poll orchestration between steps.

See [docs/architecture.md](../../docs/architecture.md) for details on the
execution model.

## Recommendation

For most use cases, use `cmd/server/` instead. It runs as a local HTTP server
with persistent state and does not require AWS Lambda infrastructure.
