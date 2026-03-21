provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "hermes"
      Environment = var.environment
      ManagedBy   = "terraform"
    }
  }
}
