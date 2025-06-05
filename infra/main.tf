# Would like to be able to create the S3 bucket, and then configure it as a TF backend

# KMS Key
data "aws_caller_identity" "current" {}

resource "aws_kms_key" "obsyncian" {
  description             = "An example symmetric encryption KMS key"
  enable_key_rotation     = true
  deletion_window_in_days = 20
  policy = jsonencode({
    Version = "2012-10-17"
    Id      = "obsyncian"
    Statement = [
      {
        Sid    = "Enable IAM User Permissions"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        },
        Action   = "kms:*"
        Resource = "*"
      }#,
    #   {
    #     Sid    = "Allow administration of the key"
    #     Effect = "Allow"
    #     Principal = {
    #       AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:user/pulumi"
    #     },
    #     Action = [
    #       "kms:ReplicateKey",
    #       "kms:Create*",
    #       "kms:Describe*",
    #       "kms:Enable*",
    #       "kms:List*",
    #       "kms:Put*",
    #       "kms:Update*",
    #       "kms:Revoke*",
    #       "kms:Disable*",
    #       "kms:Get*",
    #       "kms:Delete*",
    #       "kms:ScheduleKeyDeletion",
    #       "kms:CancelKeyDeletion"
    #     ],
    #     Resource = "*"
    #   },
    #   {
    #     Sid    = "Allow use of the key"
    #     Effect = "Allow"
    #     Principal = {
    #       AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:user/obsyncian"
    #     },
    #     Action = [
    #       "kms:DescribeKey",
    #       "kms:Encrypt",
    #       "kms:Decrypt",
    #       "kms:ReEncrypt*",
    #       "kms:GenerateDataKey",
    #       "kms:GenerateDataKeyWithoutPlaintext"
    #     ],
    #     Resource = "*"
    #   }
    ]
  })
}

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
