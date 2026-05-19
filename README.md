# swim-attendance-app

A web app that allows swimmers to register for practices and admins to track attendance. The backend is a Go Lambda behind a Function URL; the frontend is a React/Vite SPA deployed to S3. Infrastructure is managed with AWS CDK (Go).

---

## Architecture

```
Frontend (React/Vite)
  └── S3 static website (swim-signup-ui-{env})
        └── calls Lambda Function URL

Backend (Go)
  └── Lambda (swim-signup-api-{env})
        └── DynamoDB single table (swim-app-{env})
              pk = entity ID, sk = type discriminator or swimmer email

Calendar sync
  └── Google Calendar API → POST /practices/sync
```

---

## Prerequisites

- Go 1.23+
- Node.js 18+ and npm
- AWS CLI v2 — [install guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- AWS CDK CLI — `npm install -g aws-cdk`
- An AWS account with credentials configured
- (Optional) A Google Calendar service account for practice syncing

---

## Local development

### 1. Configure AWS credentials

```bash
aws configure
# prompts for Access Key ID, Secret Access Key, region (e.g. us-east-1), output format
```

> Local dev hits real AWS DynamoDB using these credentials. Make sure the table `swim-app` exists (or deploy the stack first).

### 2. Run the backend

```bash
cd backend
PORT=8080 go run ./cmd/lambda
```

With Google Calendar sync enabled:

```bash
cd backend
PORT=8080 \
GOOGLE_CALENDAR_ID="your-calendar-id@group.calendar.google.com" \
go run ./cmd/lambda
```

The backend listens on `http://localhost:8080`. When `AWS_LAMBDA_FUNCTION_NAME` is not set it runs as a plain HTTP server.

### 3. Run the frontend

```bash
cd frontend
npm install       # first time only
npm run dev
```

Vite proxies `/practices`, `/signups`, and `/my-signups` to `localhost:8080`, so no CORS config is needed locally.

Open `http://localhost:5173`.

---

## Deployment

### 1. Bootstrap CDK (once per AWS account/region)

```bash
aws configure   # if not already done

cd cdk
cdk bootstrap -c env=dev
```

This creates the CDK toolkit stack in your account (S3 bucket, IAM roles). Only needed once per account/region.

### 2. Build the Lambda binary

```bash
make build
# produces backend/lambda.zip (ARM64 binary for provided.al2023 runtime)
```

### 3. Deploy

```bash
cd cdk

# dev
GOOGLE_CALENDAR_ID="..." \
GOOGLE_CREDENTIALS_JSON="$(cat path/to/service-account.json)" \
cdk deploy -c env=dev

# prod
GOOGLE_CALENDAR_ID="..." \
GOOGLE_CREDENTIALS_JSON="$(cat path/to/service-account.json)" \
cdk deploy -c env=prod
```

CDK will print the stack outputs when complete:

| Output | Description |
|---|---|
| `ApiUrl` | Lambda Function URL — set as `VITE_API_URL` for the frontend build |
| `UiBucketName` | S3 bucket to deploy the frontend to |
| `UiUrl` | Public URL of the frontend |

### 4. Deploy the frontend

```bash
cd frontend

# build with the API URL from the CDK output
VITE_API_URL="https://xxxx.lambda-url.us-east-1.on.aws" npm run build

# sync to S3
aws s3 sync dist/ s3://swim-signup-ui-dev --delete
```

---

## Useful make targets

| Target | Description |
|---|---|
| `make build` | Compile Go Lambda binary → `backend/lambda.zip` |
| `make dev-backend` | Run backend locally on port 8080 |
| `make dev-frontend` | Run Vite dev server |
| `make deploy` | Build + `cdk deploy` (Terraform legacy — use CDK instead) |
| `make update-lambda` | Push a new zip to an existing Lambda without a full deploy |
