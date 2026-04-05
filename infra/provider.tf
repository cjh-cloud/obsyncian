terraform {
  required_version = ">= 1.12.0"

  backend "s3" {
    bucket       = "cjscloudcity-terraform-backend-aszx"
    key          = "obsyncian/state"
    region       = "ap-southeast-2"
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.99"
    }
  }
}

provider "aws" {
  profile = "default"
  region = "ap-southeast-2"
  default_tags {
    tags = {
      Project   = "Obsyncian"
      ManagedBy = "Terraform"
    }
  }
}