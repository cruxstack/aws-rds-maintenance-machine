# Architecture

This document describes the internal architecture of the RDS Maintenance
Machine.

## Overview

```
+-------------------------------------------------------------------------+
|                              Request Flow                               |
|                                                                         |
|   Browser --> HTTP Server --> App --> Engine --> RDS Client --> AWS     |
|      |            |           |         |                               |
|      |            |           |         +--> Storage (persist state)    |
|      |            |           |                                         |
|      +----------------------------+--> Web UI (React SPA)               |
+-------------------------------------------------------------------------+
```

## Components

### Entry Points

| Path          | Description                              |
| ------------- | ---------------------------------------- |
| `cmd/server/` | HTTP server entry point (primary)        |
| `cmd/demo/`   | Demo mode - includes mock RDS API server |
| `cmd/lambda/` | AWS Lambda entry point (experimental)    |
| `cmd/verify/` | Integration test harness for mock server |

### Core Packages

| Package               | Description                                       |
| --------------------- | ------------------------------------------------- |
| `internal/app/`       | HTTP routing, request handling, application logic |
| `internal/machine/`   | State machine engine, step handlers, builders     |
| `internal/rds/`       | AWS RDS SDK wrapper with convenience methods      |
| `internal/storage/`   | Persistent storage abstraction (file-based)       |
| `internal/config/`    | Configuration loading from environment            |
| `internal/types/`     | Shared type definitions                           |
| `internal/mock/`      | Mock RDS API server for demo/testing              |
| `internal/notifiers/` | Slack notification integration                    |

### Web UI

The React frontend is built and embedded in `internal/app/ui/dist/`. Source code
is in the `ui/` directory.

## State Machine Engine

### Operation Lifecycle

```
                    +---------+
                    | created |
                    +----+----+
                         | start
                         v
                    +---------+
              +-----| running |-----+
              |     +----+----+     |
              |          |          |
         pause|          |          |error
              |          |          |
              v          v          v
         +--------+ +---------+ +--------+
         | paused | |completed| | failed |
         +---+----+ +---------+ +--------+
             |
        resume|
             |
             +------> (back to running)
```

### Operation States

| State          | Description                       |
| -------------- | --------------------------------- |
| `created`      | Operation created but not started |
| `running`      | Actively executing steps          |
| `paused`       | Waiting for human intervention    |
| `completed`    | Successfully finished             |
| `failed`       | Failed and cannot continue        |
| `rolling_back` | Rollback in progress              |
| `rolled_back`  | Rollback completed                |

### Step States

| State         | Description                                        |
| ------------- | -------------------------------------------------- |
| `pending`     | Not yet started                                    |
| `in_progress` | Currently executing                                |
| `waiting`     | Waiting for a condition (e.g., instance available) |
| `completed`   | Successfully finished                              |
| `failed`      | Step failed                                        |
| `skipped`     | Step was skipped                                   |

## Execution Model

When an operation is started:

1. HTTP handler validates request and returns immediately
2. Background goroutine begins executing steps sequentially
3. Each step:
   - Calls AWS RDS API
   - Enters wait loop if needed (polling every 30s)
   - Updates operation state and persists to disk
   - Sends Slack notification on completion (if configured)
4. On error, operation pauses for human intervention
5. User can resume, abort, or rollback via API/UI

```
POST /api/operations/{id}/start
         |
         v
+----------------------------------------------------------------+
|  go e.executeSteps(ctx, op)  <-- Background goroutine          |
|      |                                                         |
|      +--> Execute Step 1 (create_temp_instance)                |
|      |       +--> persist state, notify                        |
|      |                                                         |
|      +--> Execute Step 2 (wait_instance_available)             |
|      |       +--> poll loop until condition met                |
|      |                                                         |
|      +--> Execute Step 3 (failover_to_instance)                |
|      |       +--> persist state, notify                        |
|      |                                                         |
|      +--> ... (continues until complete or paused)             |
+----------------------------------------------------------------+
```

## Storage

Operations and events are persisted to the filesystem:

```
$APP_DATA_DIR/
  operations/
    {operation-id}/
      operation.json    # Operation state
      events.json       # Event log
```

The storage abstraction (`internal/storage/`) supports:

- **FileStore** - Local filesystem (default)
- **NullStore** - In-memory only (no persistence)

## Intervention Handling

When an operation requires human intervention:

1. Operation transitions to `paused` state with a reason
2. Slack notification sent (if configured)
3. User reviews situation via Web UI
4. User chooses action via `POST /api/operations/{id}/resume`:
   - `continue` - Resume from current step
   - `abort` - Mark operation as failed
   - `rollback` - Execute rollback steps
   - `mark_complete` - Force mark as completed

## Error Handling

- Transient errors trigger retry (configurable max retries per step)
- Persistent errors pause operation for human intervention
- Server crash: operations auto-resume or pause on restart (configurable)
- All state changes are persisted before acknowledging to client

## Experimental: Step Functions Mode

An experimental AWS Step Functions deployment mode is available. When enabled
(`APP_EXPERIMENTAL_STEPFN_ENABLED=true`), the application can be deployed as a
Lambda function with Step Functions orchestration.

This changes the execution model:

- Each Lambda invocation executes only one step
- Step Functions handles wait/poll orchestration via state machine
- No Lambda timeout issues (each invocation is short-lived)

This feature is not production-ready and may change in future releases.
