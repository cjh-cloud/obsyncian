
terraform {
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
