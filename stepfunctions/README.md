# AWS Step Functions Deployment

This directory contains the AWS Step Functions state machine definition and IAM
policies for deploying the RDS Maintenance Machine as a serverless application.

## Architecture

The Step Functions deployment uses a step-at-a-time execution model where:

1. **Lambda executes one step per invocation** - Each Lambda call executes only
   the current operation step and returns immediately
2. **Step Functions handles orchestration** - Wait states, polling loops, and
   retry logic are managed by Step Functions
3. **EventBridge enables human intervention** - When operations pause for
   intervention, an event is sent to EventBridge with a callback token

This architecture allows long-running RDS operations (which can take 30-60+
minutes) to be executed reliably without hitting Lambda's 15-minute timeout.

## Files

| File                         | Description                                      |
| ---------------------------- | ------------------------------------------------ |
| `rds-maint-machine.asl.json` | Step Functions state machine definition (ASL)    |
| `iam-policy.json`            | IAM policy for the Step Functions execution role |
| `lambda-iam-policy.json`     | IAM policy for the Lambda function               |

## Prerequisites

1. AWS CLI configured with appropriate credentials
2. An S3 bucket for storing operation state (or use DynamoDB)
3. Optional: Slack webhook URL for notifications

## Deployment Steps

### 1. Create the Lambda Function

Build and deploy the Lambda function:

```bash
# Build the Lambda binary
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap cmd/lambda/main.go

# Create deployment package
zip function.zip bootstrap

# Create the Lambda function
aws lambda create-function \
  --function-name rds-maint-machine \
  --runtime provided.al2023 \
  --handler bootstrap \
  --zip-file fileb://function.zip \
  --role arn:aws:iam::ACCOUNT_ID:role/rds-maint-machine-lambda-role \
  --timeout 300 \
  --memory-size 256 \
  --environment "Variables={DATA_DIR=/tmp,AWS_REGION=us-east-1}"
```

### 2. Create IAM Roles

Create the Lambda execution role:

```bash
# Create role
aws iam create-role \
  --role-name rds-maint-machine-lambda-role \
  --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Principal": {"Service": "lambda.amazonaws.com"},
      "Action": "sts:AssumeRole"
    }]
  }'

# Attach policy (update lambda-iam-policy.json with your bucket name first)
aws iam put-role-policy \
  --role-name rds-maint-machine-lambda-role \
  --policy-name rds-maint-machine-policy \
  --policy-document file://lambda-iam-policy.json
```

Create the Step Functions execution role:

```bash
# Create role
aws iam create-role \
  --role-name rds-maint-machine-sfn-role \
  --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Principal": {"Service": "states.amazonaws.com"},
      "Action": "sts:AssumeRole"
    }]
  }'

# Attach policy (update iam-policy.json with your account ID first)
aws iam put-role-policy \
  --role-name rds-maint-machine-sfn-role \
  --policy-name rds-maint-machine-sfn-policy \
  --policy-document file://iam-policy.json
```

### 3. Create the Step Functions State Machine

```bash
# Replace ${LambdaArn} in the ASL with your Lambda ARN
LAMBDA_ARN="arn:aws:lambda:us-east-1:ACCOUNT_ID:function:rds-maint-machine"
sed "s|\${LambdaArn}|$LAMBDA_ARN|g" rds-maint-machine.asl.json > state-machine.json

# Create the state machine
aws stepfunctions create-state-machine \
  --name rds-maint-machine \
  --definition file://state-machine.json \
  --role-arn arn:aws:iam::ACCOUNT_ID:role/rds-maint-machine-sfn-role \
  --type STANDARD
```

## Usage

### Starting an Operation

Start a new maintenance operation:

```bash
aws stepfunctions start-execution \
  --state-machine-arn arn:aws:states:us-east-1:ACCOUNT_ID:stateMachine:rds-maint-machine \
  --input '{
    "params": {
      "type": "instance_type_change",
      "cluster_id": "my-aurora-cluster",
      "region": "us-east-1",
      "params": {
        "target_instance_type": "db.r6g.xlarge"
      }
    }
  }'
```

### Resuming an Existing Operation

Resume a paused or previously running operation:

```bash
aws stepfunctions start-execution \
  --state-machine-arn arn:aws:states:us-east-1:ACCOUNT_ID:stateMachine:rds-maint-machine \
  --input '{
    "operation_id": "existing-operation-uuid"
  }'
```

### Responding to Interventions

When an operation pauses for human intervention, an event is sent to EventBridge
with a task token. To respond:

1. **Continue**: Resume the operation
2. **Abort**: Stop the operation and mark it as failed
3. **Rollback**: Attempt to rollback changes
4. **Mark Complete**: Mark the operation as successful despite errors

Use the AWS SDK or CLI to send the response:

```bash
aws stepfunctions send-task-success \
  --task-token "TASK_TOKEN_FROM_EVENT" \
  --task-output '{"action": "continue", "comment": "Approved by operator"}'
```

Or to abort:

```bash
aws stepfunctions send-task-failure \
  --task-token "TASK_TOKEN_FROM_EVENT" \
  --error "OperationAborted" \
  --cause "Aborted by operator"
```

## EventBridge Integration

The state machine sends events to EventBridge when intervention is required. You
can create EventBridge rules to:

1. Send notifications to Slack/Teams/PagerDuty
2. Trigger a Lambda function to handle automated responses
3. Route to an SNS topic for email notifications

Example EventBridge rule pattern:

```json
{
  "source": ["rds-maint-machine"],
  "detail-type": ["InterventionRequired"]
}
```

## Monitoring

### CloudWatch Logs

Both Lambda and Step Functions emit logs to CloudWatch:

- Lambda: `/aws/lambda/rds-maint-machine`
- Step Functions: Enable logging when creating the state machine

### X-Ray Tracing

Enable X-Ray tracing for end-to-end visibility:

```bash
aws stepfunctions update-state-machine \
  --state-machine-arn arn:aws:states:us-east-1:ACCOUNT_ID:stateMachine:rds-maint-machine \
  --tracing-configuration enabled=true
```

## Operation Types

The following operation types are supported:

| Type                   | Description                                       |
| ---------------------- | ------------------------------------------------- |
| `instance_type_change` | Change instance types across the cluster          |
| `storage_type_change`  | Change storage type (e.g., gp2 to gp3)            |
| `engine_upgrade`       | Major version upgrade using Blue-Green deployment |
| `instance_cycle`       | Reboot all instances to apply pending changes     |

See the main README for detailed parameter documentation for each operation
type.

## Troubleshooting

### Lambda Timeout

If Lambda times out, it's likely executing a wait step. The step-at-a-time model
should prevent this. Check:

1. Lambda timeout is set to at least 300 seconds
2. The `step` action is being used (not `start`)

### State Machine Stuck in WaitBeforePoll

This is normal behavior for long-running RDS operations. The state machine polls
every 30 seconds. You can:

1. Wait for the operation to complete
2. Check RDS console for operation progress
3. Use the `status` action to check current state

### Intervention Not Received

Ensure EventBridge rule is configured and the task token is being
stored/forwarded correctly.
