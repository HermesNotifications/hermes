# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# Must match the build matrix in .github/workflows/cd.yml. ECR does not create a repository
# on push, so a service in the matrix and not here fails the whole matrix leg -- which is
# what `natsprovision` and `cleanup` were doing on every commit to main, silently, because a
# red leg in a 12-way matrix reads as "CD is flaky" rather than "CD is broken".
locals {
  services = toset([
    "admin",
    "dispatch",
    "send",
    "inbox",
    "user",
    "worker-events",
    "worker-email",
    "worker-sms",
    "worker-inbox",
    "migrate",
    "natsprovision",
    "cleanup",
  ])
}

resource "aws_ecr_repository" "services" {
  for_each = local.services

  name                 = "hermes-${each.value}"
  image_tag_mutability = "IMMUTABLE"

  # Finding 21. force_delete lets `terraform destroy` remove a repository that still
  # contains images, which quietly undoes the point of IMMUTABLE above: the tags cannot
  # be overwritten, but the whole repository — and every image a running deployment is
  # pulling — can be deleted in one apply.
  #
  # Left on outside production so throwaway environments stay disposable.
  force_delete = var.environment != "production"

  image_scanning_configuration {
    scan_on_push = true
  }

  encryption_configuration {
    encryption_type = "AES256"
  }

  tags = {
    Name    = "hermes-${each.value}"
    Service = each.value
  }
}

resource "aws_ecr_lifecycle_policy" "services" {
  for_each = local.services

  repository = aws_ecr_repository.services[each.key].name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images after 7 days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 7
        }
        action = {
          type = "expire"
        }
      },
      {
        rulePriority = 2
        description  = "Keep only 20 tagged images"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["v", "sha-"]
          countType     = "imageCountMoreThan"
          countNumber   = 20
        }
        action = {
          type = "expire"
        }
      },
    ]
  })
}
