#!/bin/bash
# Uploads key-value secrets from local gitignored .env.production into AWS Secrets Manager.
# Usage: ./scripts/sync_secrets_to_aws.sh [prod|dev] [us-east-2]
set -euo pipefail

STAGE=${1:-prod}
REGION=${2:-us-east-2}
ENV_FILE=".env.production"
SECRET_NAME="freight-agent/${STAGE}/secrets"

if [ ! -f "${ENV_FILE}" ]; then
  echo "Error: ${ENV_FILE} not found. Copy .env.example to ${ENV_FILE} and populate production secrets first."
  exit 1
fi

SECRET_JSON=$(python3 -c '
import json, sys
data = {}
excluded = {"ACCESS_KEY", "SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
with open(sys.argv[1], "r") as f:
    for line in f:
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        k, v = k.strip(), v.strip().strip("\"").strip("\x27")
        if k and v and k not in excluded:
            data[k] = v
print(json.dumps(data))
' "${ENV_FILE}")

if aws secretsmanager describe-secret --secret-id "${SECRET_NAME}" --region "${REGION}" >/dev/null 2>&1; then
  echo "Updating existing AWS Secrets Manager secret: ${SECRET_NAME} (${REGION})..."
  aws secretsmanager put-secret-value \
    --secret-id "${SECRET_NAME}" \
    --secret-string "${SECRET_JSON}" \
    --region "${REGION}" >/dev/null
else
  echo "Creating new AWS Secrets Manager secret: ${SECRET_NAME} (${REGION})..."
  aws secretsmanager create-secret \
    --name "${SECRET_NAME}" \
    --description "Dispatch Freight Agent runtime secrets (${STAGE})" \
    --secret-string "${SECRET_JSON}" \
    --region "${REGION}" >/dev/null
fi

echo "Done! Secrets synced securely to AWS Secrets Manager: ${SECRET_NAME}"
