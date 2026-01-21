# Would like to be able to create the S3 bucket, and then configure it as a TF backend

# KMS Key
data "aws_caller_identity" "current" {}

# resource "aws_kms_key" "obsyncian" {
#   description             = "An example symmetric encryption KMS key"
#   enable_key_rotation     = true
#   deletion_window_in_days = 20
#   policy = jsonencode({
#     Version = "2012-10-17"
#     Id      = "obsyncian"
#     Statement = [
#       {
#         Sid    = "Enable IAM User Permissions"
#         Effect = "Allow"
#         Principal = {
#           AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
#         },
#         Action   = "kms:*"
#         Resource = "*"
#       }#,
#     #   {
#     #     Sid    = "Allow administration of the key"
#     #     Effect = "Allow"
#     #     Principal = {
#     #       AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:user/pulumi"
#     #     },
#     #     Action = [
#     #       "kms:ReplicateKey",
#     #       "kms:Create*",
#     #       "kms:Describe*",
#     #       "kms:Enable*",
#     #       "kms:List*",
#     #       "kms:Put*",
#     #       "kms:Update*",
#     #       "kms:Revoke*",
#     #       "kms:Disable*",
#     #       "kms:Get*",
#     #       "kms:Delete*",
#     #       "kms:ScheduleKeyDeletion",
#     #       "kms:CancelKeyDeletion"
#     #     ],
#     #     Resource = "*"
#     #   },
#     #   {
#     #     Sid    = "Allow use of the key"
#     #     Effect = "Allow"
#     #     Principal = {
#     #       AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:user/obsyncian"
#     #     },
#     #     Action = [
#     #       "kms:DescribeKey",
#     #       "kms:Encrypt",
#     #       "kms:Decrypt",
#     #       "kms:ReEncrypt*",
#     #       "kms:GenerateDataKey",
#     #       "kms:GenerateDataKeyWithoutPlaintext"
#     #     ],
#     #     Resource = "*"
#     #   }
#     ]
#   })
# }

# S3 bucket
resource "aws_s3_bucket" "obsyncian" {
  bucket = "obsyncian" # TODO need a suffix

  tags = {
    Name        = "My bucket"
    Environment = "Dev"
  }
}

# DynamoDB table
resource "aws_dynamodb_table" "obsyncian" {
  name           = "Obsyncian"
  billing_mode   = "PAY_PER_REQUEST" # "PROVISIONED"
  #   read_capacity  = 1
  #   write_capacity = 1
  hash_key       = "UserId"
  # range_key      = "TimeChanged"

  attribute {
    name = "UserId"
    type = "S"
  }

  # attribute {
  #   name = "TimeChanged"
  #   type = "S"
  # }

#   attribute {
#     name = "TopScore"
#     type = "N"
#   }

}

# IAM User
resource "aws_iam_user" "obsyncian" {
  name = "obsyncian"
  path = "/system/" # TODO : what is this?
}

resource "aws_iam_access_key" "obsyncian" {
  user = aws_iam_user.obsyncian.name
}

output "key" {
  value = aws_iam_access_key.obsyncian.id
}

output "secret" {
  value = aws_iam_access_key.obsyncian.secret
  sensitive = true
}

data "aws_iam_policy_document" "obsyncian" {
  statement {
    effect    = "Allow"
    actions   = ["S3:*"]
    resources = ["*"]
  }

  statement {
    effect    = "Allow"
    actions   = ["DynamoDB:*"]
    resources = ["*"]
  }

  statement {
    effect    = "Allow"
    actions   = ["ce:*"]
    resources = ["*"]
  }

  # SQS permissions for event-driven sync
  statement {
    effect = "Allow"
    actions = [
      "sqs:CreateQueue",
      "sqs:DeleteQueue",
      "sqs:GetQueueUrl",
      "sqs:GetQueueAttributes",
      "sqs:SetQueueAttributes",
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:PurgeQueue"
    ]
    resources = ["arn:aws:sqs:*:${data.aws_caller_identity.current.account_id}:obsyncian-*"]
  }

  # SNS permissions for subscribing to sync notifications
  statement {
    effect = "Allow"
    actions = [
      "sns:Subscribe",
      "sns:Unsubscribe",
      "sns:GetTopicAttributes"
    ]
    resources = ["arn:aws:sns:*:${data.aws_caller_identity.current.account_id}:obsyncian-sync-notifications"]
  }

  #   statement {
  #     effect    = "Allow"
  #     actions   = ["KMS:*"]
  #     resources = ["*"]
  #   }
}

resource "aws_iam_user_policy" "obsyncian" {
  name   = "obsyncian"
  user   = aws_iam_user.obsyncian.name
  policy = data.aws_iam_policy_document.obsyncian.json
}

# =============================================================================
# Event-Driven Sync Infrastructure
# =============================================================================

# Enable EventBridge notifications for S3 bucket
resource "aws_s3_bucket_notification" "obsyncian" {
  bucket      = aws_s3_bucket.obsyncian.id
  eventbridge = true
}

# SNS Topic for sync notifications (fan-out to multiple devices)
resource "aws_sns_topic" "obsyncian_sync" {
  name = "obsyncian-sync-notifications"
}

# SNS Topic policy to allow EventBridge to publish
resource "aws_sns_topic_policy" "obsyncian_sync" {
  arn    = aws_sns_topic.obsyncian_sync.arn
  policy = data.aws_iam_policy_document.sns_topic_policy.json
}

data "aws_iam_policy_document" "sns_topic_policy" {
  statement {
    sid    = "AllowEventBridgePublish"
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.obsyncian_sync.arn]
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.s3_sync_events.arn]
    }
  }

  statement {
    sid    = "AllowObsyncianUserSubscribe"
    effect = "Allow"
    principals {
      type        = "AWS"
      identifiers = [aws_iam_user.obsyncian.arn]
    }
    actions = [
      "sns:Subscribe",
      "sns:Receive"
    ]
    resources = [aws_sns_topic.obsyncian_sync.arn]
  }
}

# EventBridge rule to catch S3 object changes
resource "aws_cloudwatch_event_rule" "s3_sync_events" {
  name        = "obsyncian-s3-sync-events"
  description = "Capture S3 object changes for Obsyncian sync notifications"

  event_pattern = jsonencode({
    source      = ["aws.s3"]
    detail-type = ["Object Created", "Object Deleted"]
    detail = {
      bucket = {
        name = [aws_s3_bucket.obsyncian.id]
      }
    }
  })
}

# EventBridge target to send events to SNS
resource "aws_cloudwatch_event_target" "sns_target" {
  rule      = aws_cloudwatch_event_rule.s3_sync_events.name
  target_id = "SendToSNS"
  arn       = aws_sns_topic.obsyncian_sync.arn
}

# Output the SNS topic ARN for the client to use
output "sns_topic_arn" {
  value       = aws_sns_topic.obsyncian_sync.arn
  description = "SNS topic ARN for sync notifications"
}
