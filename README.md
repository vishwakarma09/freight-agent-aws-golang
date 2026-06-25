# Freight Agent AWS - Go Migration

A replica of the Python `freight-agent-aws` application, rebuilt in **Golang** for high-performance execution on **AWS Lambda** (custom `provided.al2023` ARM64 runtime). The project connects to a PostgreSQL database (supporting `pgvector` for embedding searches), uses Amazon S3 for private document storage, and hosts a React/Vite web application on S3.

**Demo URL**: [http://freight-agent-frontend-go-dev-865122443732.s3-website.us-east-2.amazonaws.com/](http://freight-agent-frontend-go-dev-865122443732.s3-website.us-east-2.amazonaws.com/)

---

## AWS Architecture

```mermaid
graph TD
    Client[Web Browser Client] -->|HTTPS Web App| S3Web[S3 Public Web Hosting Bucket]
    Client -->|REST API Requests| APIGateway[AWS API Gateway HTTP API v2]
    
    APIGateway -->|Proxy ANY| LambdaAPI[Lambda: API Function - Go]
    EventBridge[EventBridge Scheduler] -->|Rate: 10m| LambdaCron[Lambda: Cron Function - Go]
    
    LambdaAPI -->|Connect| RDS[Amazon RDS PostgreSQL + pgvector]
    LambdaCron -->|Connect| RDS
    
    LambdaAPI -->|Presign / Upload BOL & Invoice| S3Private[S3 Private Storage Bucket]
    
    LambdaAPI -->|Send Notifications| SMTP[SMTP Server / Mailpit]
    LambdaCron -->|Ingest Carrier Bids| IMAP[IMAP Polling / Mailpit API]
```

---

## App Architecture

```mermaid
graph TD
    subgraph CMD [Entrypoints]
        API[cmd/api/main.go]
        CRON[cmd/cron/main.go]
    end

    subgraph Internal [Internal Router & Middleware]
        Setup[internal/routes/router.go]
        Auth[internal/routes/middleware.go]
    end

    subgraph Services [Business Logic Layer]
        WorkflowService[internal/services/workflow.go]
        AIService[internal/services/ai.go]
        EmailService[internal/services/email.go]
        S3Service[internal/services/s3_storage.go]
        EmbeddingService[internal/services/embedding.go]
        BillingService[internal/services/billing.go]
    end

    subgraph DB [Data Layer]
        GORM[GORM ORM Engine]
        Models[internal/models/models.go]
        RDS_PG[(PostgreSQL Database)]
    end

    API --> Setup
    Setup --> Auth
    Auth --> WorkflowService
    CRON --> WorkflowService

    WorkflowService --> AIService
    WorkflowService --> EmailService
    WorkflowService --> S3Service
    WorkflowService --> EmbeddingService
    WorkflowService --> BillingService

    WorkflowService --> GORM
    GORM --> Models
    Models --> RDS_PG
```

---

## State Machine Workflow

```mermaid
stateDiagram-v2
    [*] --> INTAKE : Customer Email / API Quote Request
    INTAKE --> OUT_TO_CARRIERS : Broadcast RFQs to Carriers & Set 2-Min Timer
    
    state OUT_TO_CARRIERS {
        [*] --> AwaitFirstRoundBids
        AwaitFirstRoundBids --> EvaluateFirstRoundBids : 2-Min Timer Expires
    }
    
    EvaluateFirstRoundBids --> RE_BID_ROUND : Multiple Bids (Send current low-bid to others)
    EvaluateFirstRoundBids --> AWAITING_APPROVAL : Single Bid or No Re-bids
    
    state RE_BID_ROUND {
        [*] --> AwaitRebids
        AwaitRebids --> EvaluateFinalBids : 30-Min Timer Expires
    }
    
    EvaluateFinalBids --> AWAITING_APPROVAL
    
    AWAITING_APPROVAL --> BOOKED : Broker Approves Bid (Gen Invoice/BOL, upload to S3)
    AWAITING_APPROVAL --> LOST : Broker Rejects / Lost to Competitor
    AWAITING_APPROVAL --> CANCELLED : Manual Cancellation
    
    BOOKED --> [*]
    LOST --> [*]
    CANCELLED --> [*]
```

---

## Local Development Setup

Follow these steps to configure and run the application locally on your machine:

### Prerequisites
- **Go**: Version 1.21 or higher.
- **Node.js**: Version 18 or higher (for the frontend React app).
- **PostgreSQL**: A local PostgreSQL database (or access to the RDS database).
- **pgvector Extension**: Required on the PostgreSQL server. Enable it using:
  ```sql
  CREATE EXTENSION IF NOT EXISTS vector;
  ```

### 1. Configuration (`.env`)
Create a `.env` file in the root directory (based on the template below) and fill in the credentials:
```ini
ACCESS_KEY=your-aws-access-key-id
SECRET_ACCESS_KEY=your-aws-secret-access-key
API_URL=http://localhost:8000
FRONTEND_URL=http://localhost:5173
DATABASE_URL=postgresql://user:password@localhost:5432/dbname
CEREBRAS_API_KEY=your-cerebras-key
GOOGLE_CLIENT_ID=your-google-client-id
GOOGLE_CLIENT_SECRET=your-google-client-secret

# SMTP/IMAP Email Server
SMTP_HOST=your-smtp-host
SMTP_PORT=587
SMTP_USER=your-smtp-username
SMTP_PASSWORD=your-smtp-password
EMAILS_FROM_EMAIL=your-smtp-username
SMTP_TLS=True
```

### 2. Start the Backend API Server
Start the Gin HTTP server locally (listens on `:8000` by default):
```bash
go run cmd/api/main.go
```
The server will automatically run table schema migrations and seeds on startup.

### 3. Run Cron Tasks Locally
You can trigger the state machine timer evaluations and email polling manually:
```bash
go run cmd/cron/main.go
```

### 4. Start the Frontend React Web App
1. Navigate to the `frontend` directory:
   ```bash
   cd frontend
   ```
2. Install dependencies:
   ```bash
   npm install
   ```
3. Start the Vite development server (runs on `http://localhost:5173`):
   ```bash
   npm run dev
   ```

### 5. Running Integration Tests
To verify that all local endpoints are working as expected:
```bash
python3 test_api.py
```
*(Ensure `API_URL` in `.env` is set to `http://localhost:8000` before running tests).*

---

## Deployment to AWS

To compile the binaries and deploy the infrastructure using the Serverless Framework:

1. **Build Binaries (cross-compiles to Linux/ARM64 and packages zips):**
   ```bash
   make build
   ```
2. **Deploy Backend (API Gateway, Lambdas, S3 Resources):**
   ```bash
   npx serverless@3 deploy --stage dev --region us-east-2
   ```
3. **Deploy Frontend (Uploads compiled assets to the public S3 Bucket):**
   ```bash
   ./deploy_frontend.sh dev
   ```
