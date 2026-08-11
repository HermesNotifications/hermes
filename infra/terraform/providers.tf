# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
