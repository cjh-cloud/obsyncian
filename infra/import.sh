#!/bin/bash
# Terraform import script for existing Obsyncian resources
# Run this from the infra/ directory after losing terraform state
#
# Prerequisites:
# - AWS CLI configured with appropriate credentials
# - Terraform initialized (terraform init)
#
# Note: The IAM access key secret cannot be retrieved after import.
# You may need to create a new access key if the secret is lost.

set -e

echo "=== Obsyncian Terraform Import Script ==="
echo ""
echo "This script will import existing AWS resources into Terraform state."
echo "Make sure you have:"
echo "  1. Run 'terraform init' first"
echo "  2. AWS credentials configured"
echo ""

# Get AWS account ID for the access key import
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
echo "AWS Account ID: $AWS_ACCOUNT_ID"
echo ""

# You'll need to provide the access key ID - check your config.json or AWS console
# Example: AKIAXXXXXXXXXXXXXXXX
ACCESS_KEY_ID="${1:-}"

if [ -z "$ACCESS_KEY_ID" ]; then
    echo "Usage: $0 <ACCESS_KEY_ID>"
    echo ""
    echo "You need to provide the IAM access key ID for the obsyncian user."
    echo "You can find this in:"
    echo "  - Your ~/obsyncian/config.json file (the 'key' field)"
    echo "  - AWS Console > IAM > Users > obsyncian > Security credentials"
    echo ""
    echo "Example: $0 AKIAXXXXXXXXXXXXXXXX"
    exit 1
fi

echo "Starting imports..."
echo ""

# 1. Import S3 bucket
echo "[1/5] Importing S3 bucket 'obsyncian'..."
terraform import aws_s3_bucket.obsyncian obsyncian

# 2. Import DynamoDB table
echo "[2/5] Importing DynamoDB table 'Obsyncian'..."
terraform import aws_dynamodb_table.obsyncian Obsyncian

# 3. Import IAM user
echo "[3/5] Importing IAM user 'obsyncian'..."
terraform import aws_iam_user.obsyncian obsyncian

# 4. Import IAM access key
echo "[4/5] Importing IAM access key..."
terraform import aws_iam_access_key.obsyncian obsyncian/$ACCESS_KEY_ID

# 5. Import IAM user policy
echo "[5/5] Importing IAM user policy 'obsyncian'..."
terraform import aws_iam_user_policy.obsyncian obsyncian:obsyncian

echo ""
echo "=== Import complete! ==="
echo ""
echo "Next steps:"
echo "  1. Run 'terraform plan' to verify the state matches your configuration"
echo "  2. The plan should show the NEW resources to be created:"
echo "     - aws_s3_bucket_notification.obsyncian"
echo "     - aws_sns_topic.obsyncian_sync"
echo "     - aws_sns_topic_policy.obsyncian_sync"
echo "     - aws_cloudwatch_event_rule.s3_sync_events"
echo "     - aws_cloudwatch_event_target.sns_target"
echo "  3. The IAM user policy will show changes (adding SQS/SNS permissions)"
echo ""
echo "WARNING: The IAM access key secret cannot be retrieved after import."
echo "If you need the secret, you'll need to create a new access key and"
echo "update your ~/obsyncian/config.json file."
