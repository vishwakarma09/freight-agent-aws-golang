#!/bin/bash
# Uploads key-value pairs from gitignored .env.production into AWS Secrets Manager
# as a single JSON secret for the Dispatch Freight Lambda API & Cron functions.
#
# Usage:
#   ./scripts/push_secrets_to_aws.sh prod us-east-2
set -euo pipefail

STAGE="${1:-prod}"
AWS_REGION="${2:-us-east-2}"
SECRET_NAME="freight-agent/${STAGE}/secrets"
ENV_FILE=".env.production"

if [ ! -f "${ENV_FILE}" ]; then
  echo "Error: ${ENV_FILE} not found. Copy .env.example to ${ENV_FILE} and populate production secrets first."
  exit 1
fi

echo "Building JSON payload from ${ENV_FILE} (excluding AWS CLI bootstrap keys)..."
SECRET_JSON=$(python3 - "${ENV_FILE}" <<'PY'
import json, sys

skip_keys = {"ACCESS_KEY", "SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SECRET_NAME"}
data = {}
with open(sys.argv[1], "r") as f:
    for line in f:
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        k, v = k.strip(), v.strip().strip('"').strip("'")
        if k and v and k not in skip_keys:
            data[k] = v

print(json.dumps(data))
PY
)

if aws secretsmanager describe-secret --secret-id "${SECRET_NAME}" --region "${AWS_REGION}" >/dev/null 2>&1; then
  echo "Updating existing AWS Secrets Manager secret: ${SECRET_NAME}..."
  aws secretsmanager put-secret-value \
    --secret-id "${SECRET_NAME}" \
    --secret-string "${SECRET_JSON}" \
    --region "${AWS_REGION}" >/dev/null
else
  echo "Creating new AWS Secrets Manager secret: ${SECRET_NAME}..."
  aws secretsmanager create-secret \
    --name "${SECRET_NAME}" \
    --description "Dispatch Freight Agent (${STAGE}) runtime credentials" \
    --secret-string "${SECRET_JSON}" \
    --region "${AWS_REGION}" >/dev/null
fi

echo "Done! Secret ${SECRET_NAME} synced in region ${AWS_REGION}."
