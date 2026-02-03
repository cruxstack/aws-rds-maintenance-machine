# server

HTTP server entry point for the RDS Maintenance Machine.

## Usage

```bash
# create .env and configure aws credentials
cp .env.example .env

# build and run directly
export AWS_REGION=us-east-1
export AWS_PROFILE=your-profile
make build-server
./var/dist/server
```

The server loads configuration from environment variables and `.env` file (if
present). See the main [README](../../README.md) for configuration options.

## Authentication

There is no built-in authentication. The recommended way to use this tool is to
run it locally on your machine where AWS credentials are configured.

If you need to expose the server, set `APP_ADMIN_TOKEN` and use the
`Authorization: Bearer <token>` header for protected endpoints.

## Other Entry Points

### cmd/lambda

The Lambda entry point (`cmd/lambda/`) can be used with API Gateway, but this
has not been tested. It is also used as the handler for AWS Step Functions
orchestration, which is experimental and disabled by default.

### cmd/demo

Demo mode (`cmd/demo/`) runs a mock RDS API server for testing the UI and
operations without affecting real AWS infrastructure.
