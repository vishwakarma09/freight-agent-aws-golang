#!/bin/bash
set -euo pipefail

STAGE=${1:-prod}
AWS_REGION=${AWS_REGION:-us-east-2}

echo "Retrieving AWS Account ID..."
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
BUCKET_NAME="freight-agent-frontend-go-${STAGE}-${AWS_ACCOUNT_ID}"

echo "Target Frontend S3 Bucket: ${BUCKET_NAME}"

# NOTE: Never source root backend .env into the frontend build shell.
# Frontend production config is read exclusively from gitignored frontend/.env.production.
cd frontend

echo "Installing frontend dependencies cleanly (npm ci)..."
npm ci

echo "Building production frontend with Vite (reads frontend/.env.production)..."
npm run build -- --mode production

echo "Uploading assets to S3 bucket ${BUCKET_NAME}..."
aws s3 sync dist/ "s3://${BUCKET_NAME}" --delete

CF_DIST_ID=$(aws cloudformation describe-stacks \
  --stack-name "freight-agent-api-go-${STAGE}" \
  --region "${AWS_REGION}" \
  --query "Stacks[0].Outputs[?OutputKey=='CloudFrontDistributionId'].OutputValue" \
  --output text 2>/dev/null || true)

if [ -n "${CF_DIST_ID}" ] && [ "${CF_DIST_ID}" != "None" ]; then
  echo "Invalidating CloudFront cache (${CF_DIST_ID})..."
  aws cloudfront create-invalidation --distribution-id "${CF_DIST_ID}" --paths "/*" >/dev/null 2>&1 || echo "Skipping CloudFront invalidation (optional)."
fi

echo "Deployment complete!"
echo "Application is live at: https://freight.owera.ca"
