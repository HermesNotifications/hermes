data "aws_caller_identity" "current" {}

# ------------------------------------------------------------------------------
# GitHub Actions OIDC Provider
# ------------------------------------------------------------------------------

resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["1c58a3a8518e8759bf075b76b750d4f2df264fcd"]

  tags = {
    Name = "github-actions"
  }
}

# ------------------------------------------------------------------------------
# GitHub Actions IAM Role
# ------------------------------------------------------------------------------

resource "aws_iam_role" "github_actions" {
  name = "hermes-github-actions"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.github.arn
      }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        }
        # Finding 15. This was `repo:<org>/<repo>:*`, which trusts EVERY ref in the
        # repository — any branch, any tag, any pull_request target. A workflow added on
        # a feature branch, or a fork's PR run if the repo ever allows one, could assume
        # this role and reach ECR and Terraform state.
        #
        # Scoped to the refs that legitimately need it:
        #   - refs/heads/main   cd.yml, via workflow_run on CI success
        #   - refs/tags/v*      cd.yml, via push of a version tag
        #
        # Note this deliberately narrows loadtest.yml as well. It is workflow_dispatch and
        # holds `id-token: write`, so before this change it could assume the role from any
        # branch — a second, easily-missed entry point. It now works only when dispatched
        # from main, which is where a load test against real infrastructure belongs.
        #
        # A sub claim carries no wildcard for the branch form, so exact-matching main
        # would also work; both are kept in StringLike for symmetry with the tag pattern.
        StringLike = {
          "token.actions.githubusercontent.com:sub" = [
            "repo:${var.github_org}/${var.github_repo}:ref:refs/heads/main",
            "repo:${var.github_org}/${var.github_repo}:ref:refs/tags/v*",
          ]
        }
      }
    }]
  })

  tags = {
    Name = "hermes-github-actions"
  }
}

resource "aws_iam_role_policy" "github_actions_ecr" {
  name = "hermes-github-actions-ecr"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ecr:GetAuthorizationToken",
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:PutImage",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
        ]
        Resource = var.ecr_repository_arns
      },
    ]
  })
}
