# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

data "aws_caller_identity" "current" {}

locals {
  cluster_name = "hermes-${var.environment}"
}

# ------------------------------------------------------------------------------
# EKS Cluster IAM Role
# ------------------------------------------------------------------------------

resource "aws_iam_role" "cluster" {
  name = "${local.cluster_name}-cluster"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "eks.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "cluster_policy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  role       = aws_iam_role.cluster.name
}

# ------------------------------------------------------------------------------
# Secrets encryption key (finding 5)
# ------------------------------------------------------------------------------
#
# A correction to the review's framing, because it changes how urgent this is. Finding 5
# says envelope encryption "cannot be added to a live cluster". That is not right, and it
# was worth checking rather than acting on:
#
#   - AWS AssociateEncryptionConfig exists precisely to "enable encryption on existing
#     clusters that don't already have encryption enabled".
#   - Provider v5.100.0 models absent -> present as an IN-PLACE update
#     (internal/service/eks/cluster.go; its own acceptance test
#     TestAccEKSCluster_Encryption_update asserts the cluster is not recreated).
#   - From Kubernetes 1.28 onward EKS already envelope-encrypts API data with an
#     AWS-owned key by default. This cluster is on 1.35. So this is not "encryption
#     versus none", it is a customer-managed KEK instead of an AWS-owned one.
#
# What IS irreversible, and is the real reason to do it now:
#
#   - AWS cannot disable secrets encryption once enabled.
#   - REMOVING this block from the config makes the provider force a full cluster
#     REPLACEMENT (a ForceNewIfChange on 1 -> 0). A reviewer skimming a plan could miss
#     that on a production control plane.
#   - Changing key_arn on an existing cluster is a SILENT NO-OP: the update handler only
#     calls the API for the 0 -> 1 transition, so a key swap reports success and does
#     nothing (upstream issue 34883, still open behaviour at v5.100.0). Treat this key as
#     permanent for the life of the cluster.
#
# Deleting this key degrades the cluster beyond recovery, hence the long deletion window.
resource "aws_kms_key" "eks_secrets" {
  description             = "Envelope encryption key for ${local.cluster_name} Kubernetes secrets"
  enable_key_rotation     = true
  deletion_window_in_days = 30

  tags = {
    Name = "${local.cluster_name}-secrets"
  }
}

resource "aws_kms_alias" "eks_secrets" {
  name          = "alias/${local.cluster_name}-secrets"
  target_key_id = aws_kms_key.eks_secrets.key_id
}

# AmazonEKSClusterPolicy already grants kms:DescribeKey, and the provider's own acceptance
# test associates a key with nothing but that attached, so this is defence in depth rather
# than a requirement. It is what terraform-aws-modules/eks attaches by default
# (attach_cluster_encryption_policy, default true) and it costs nothing to match.
resource "aws_iam_role_policy" "cluster_encryption" {
  name = "${local.cluster_name}-cluster-encryption"
  role = aws_iam_role.cluster.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "kms:Encrypt",
        "kms:Decrypt",
        "kms:ListGrants",
        "kms:DescribeKey",
      ]
      Resource = aws_kms_key.eks_secrets.arn
    }]
  })
}

# ------------------------------------------------------------------------------
# Control plane logs (finding 5)
# ------------------------------------------------------------------------------
#
# EKS auto-creates /aws/eks/<cluster>/cluster the first time logging is enabled, and
# aws_cloudwatch_log_group has no tolerance for an existing group — it calls CreateLogGroup
# and returns ResourceAlreadyExistsException verbatim. So the group must be created by
# Terraform BEFORE the cluster that would otherwise create it. That ordering is the
# documented pattern in terraform-aws-modules/eks, which does exactly this and has the
# cluster depend_on the group.
#
# Nothing has been applied here, so the ordering is reasoned from the provider source and
# that module, not observed. If a group already exists in the account, import it:
#   terraform import module.eks.aws_cloudwatch_log_group.cluster /aws/eks/<name>/cluster
resource "aws_cloudwatch_log_group" "cluster" {
  count = length(var.enabled_cluster_log_types) > 0 ? 1 : 0

  name              = "/aws/eks/${local.cluster_name}/cluster"
  retention_in_days = var.cluster_log_retention_days

  tags = {
    Name = "${local.cluster_name}-control-plane"
  }
}

# ------------------------------------------------------------------------------
# EKS Cluster
# ------------------------------------------------------------------------------

resource "aws_eks_cluster" "main" {
  name     = local.cluster_name
  version  = var.cluster_version
  role_arn = aws_iam_role.cluster.arn

  enabled_cluster_log_types = var.enabled_cluster_log_types

  encryption_config {
    provider {
      key_arn = aws_kms_key.eks_secrets.arn
    }
    resources = ["secrets"]
  }

  vpc_config {
    subnet_ids              = var.private_subnet_ids
    endpoint_public_access  = var.endpoint_public_access
    endpoint_private_access = true
    public_access_cidrs     = var.endpoint_public_access ? var.public_access_cidrs : null
  }

  # Cross-variable checks live here rather than in `variable ... validation` blocks
  # because referring to another variable from a validation rule needs Terraform >= 1.9
  # and versions.tf declares >= 1.5. Preconditions have worked since 1.2.
  lifecycle {
    precondition {
      condition     = !var.endpoint_public_access || length(var.public_access_cidrs) > 0
      error_message = "endpoint_public_access is true but public_access_cidrs is empty. An empty list does NOT block access — the provider omits the field and EKS defaults the endpoint to 0.0.0.0/0. Either set the CIDRs that should reach the API server, or set endpoint_public_access = false, which is the only way to express 'no public access'."
    }
    precondition {
      condition     = !var.endpoint_public_access || var.allow_public_access_from_anywhere || !contains(var.public_access_cidrs, "0.0.0.0/0")
      error_message = "public_access_cidrs contains 0.0.0.0/0, which exposes the Kubernetes API server to the entire internet. Replace it with the ranges that actually need to reach the API server (operator egress, CI egress), or set endpoint_public_access = false. If exposing it really is intended, set allow_public_access_from_anywhere = true so the choice is explicit and greppable."
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.cluster_policy,
    aws_iam_role_policy.cluster_encryption,
    aws_cloudwatch_log_group.cluster,
  ]

  tags = {
    Name = local.cluster_name
  }
}

# ------------------------------------------------------------------------------
# EKS Node Group IAM Role
# ------------------------------------------------------------------------------

resource "aws_iam_role" "node_group" {
  name = "${local.cluster_name}-node-group"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "node_worker" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"
  role       = aws_iam_role.node_group.name
}

resource "aws_iam_role_policy_attachment" "node_cni" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
  role       = aws_iam_role.node_group.name
}

resource "aws_iam_role_policy_attachment" "node_ecr" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
  role       = aws_iam_role.node_group.name
}

# ------------------------------------------------------------------------------
# EKS Node Group
# ------------------------------------------------------------------------------

resource "aws_eks_node_group" "main" {
  cluster_name    = aws_eks_cluster.main.name
  node_group_name = "${local.cluster_name}-nodes"
  node_role_arn   = aws_iam_role.node_group.arn
  subnet_ids      = var.node_subnet_ids
  instance_types  = var.node_instance_types
  ami_type        = "AL2023_ARM_64_STANDARD"

  disk_size = 50

  scaling_config {
    min_size     = var.node_min_size
    max_size     = var.node_max_size
    desired_size = var.node_desired_size
  }

  update_config {
    max_unavailable = 1
  }

  # Catching this at plan time rather than as an EKS API error mid-apply, by which point
  # the cluster and the KMS key already exist.
  lifecycle {
    precondition {
      condition     = length(var.node_subnet_ids) > 0
      error_message = "node_subnet_ids is empty. A node group with no subnets cannot launch instances; pass at least one of private_subnet_ids."
    }
    precondition {
      condition     = length(setsubtract(var.node_subnet_ids, var.private_subnet_ids)) == 0
      error_message = "node_subnet_ids contains subnets that are not in private_subnet_ids. Nodes can only join subnets the cluster's vpc_config registered, and EKS reports the mismatch as a generic InvalidParameterException during apply."
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.node_worker,
    aws_iam_role_policy_attachment.node_cni,
    aws_iam_role_policy_attachment.node_ecr,
  ]

  tags = {
    Name = "${local.cluster_name}-nodes"
  }
}

# ------------------------------------------------------------------------------
# Load generator node group (optional)
# ------------------------------------------------------------------------------
#
# Shares the node_group IAM role with the main pool: generators pull images from the
# same ECR repositories and need the same CNI and worker permissions, and nothing about
# running k6 wants anything more.
#
# It schedules into var.node_subnet_ids, the same subnets as the main pool. That is the
# point in a single-AZ environment — generator-to-service traffic IS the load test, and
# putting the generators in another AZ would bill every request twice on top of
# measuring a network path production does not have.
resource "aws_eks_node_group" "loadtest_generators" {
  count = var.loadtest_generator_node_pool == null ? 0 : 1

  cluster_name    = aws_eks_cluster.main.name
  node_group_name = "loadtest-generators"
  node_role_arn   = aws_iam_role.node_group.arn
  subnet_ids      = var.node_subnet_ids
  instance_types  = var.loadtest_generator_node_pool.instance_types
  ami_type        = "AL2023_ARM_64_STANDARD"

  disk_size = 50

  scaling_config {
    min_size     = var.loadtest_generator_node_pool.min_size
    max_size     = var.loadtest_generator_node_pool.max_size
    desired_size = var.loadtest_generator_node_pool.desired_size
  }

  # loadtest/k8s/node-pool.md. Both values are load-bearing: every manifest in
  # loadtest/k8s/ carries a matching toleration and nodeSelector, so a change here
  # leaves generator pods Pending with no obvious cause.
  labels = {
    pool = "loadtest-generators"
  }

  taint {
    key    = "loadtest"
    value  = "true"
    effect = "NO_SCHEDULE"
  }

  update_config {
    max_unavailable = 1
  }

  depends_on = [
    aws_iam_role_policy_attachment.node_worker,
    aws_iam_role_policy_attachment.node_cni,
    aws_iam_role_policy_attachment.node_ecr,
  ]

  # desired_size is the autoscaler's after the first run. Terraform reasserting the
  # configured value on every apply would yank capacity out from under a running test.
  lifecycle {
    ignore_changes = [scaling_config[0].desired_size]
  }

  tags = {
    Name = "${local.cluster_name}-loadtest-generators"
  }
}

# ------------------------------------------------------------------------------
# OIDC Provider
# ------------------------------------------------------------------------------

data "tls_certificate" "eks" {
  url = aws_eks_cluster.main.identity[0].oidc[0].issuer
}

resource "aws_iam_openid_connect_provider" "eks" {
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.eks.certificates[0].sha1_fingerprint]
  url             = aws_eks_cluster.main.identity[0].oidc[0].issuer

  tags = {
    Name = local.cluster_name
  }
}

# ------------------------------------------------------------------------------
# IRSA: External Secrets Operator
# ------------------------------------------------------------------------------

resource "aws_iam_role" "external_secrets" {
  name = "${local.cluster_name}-external-secrets"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRoleWithWebIdentity"
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.eks.arn
      }
      Condition = {
        StringEquals = {
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:sub" = "system:serviceaccount:external-secrets:external-secrets-sa"
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:aud" = "sts.amazonaws.com"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "external_secrets" {
  name = "${local.cluster_name}-external-secrets"
  role = aws_iam_role.external_secrets.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        # Finding 21. DescribeSecret added: External Secrets calls it to read metadata
        # and version stages, and without it ESO fails on secrets it can otherwise read
        # — an error that surfaces as a sync failure rather than a permissions message.
        # Both stay scoped to secret:hermes/*, unlike the Crossplane role in finding 6.
        Action   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
        Resource = "arn:aws:secretsmanager:*:${data.aws_caller_identity.current.account_id}:secret:hermes/*"
      },
      {
        Effect   = "Allow"
        Action   = "ssm:GetParameter"
        Resource = "arn:aws:ssm:*:${data.aws_caller_identity.current.account_id}:parameter/hermes/*"
      },
    ]
  })
}

# ------------------------------------------------------------------------------
# IRSA: EBS CSI Driver
# ------------------------------------------------------------------------------

resource "aws_iam_role" "ebs_csi" {
  name = "${local.cluster_name}-ebs-csi"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRoleWithWebIdentity"
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.eks.arn
      }
      Condition = {
        StringEquals = {
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:sub" = "system:serviceaccount:kube-system:ebs-csi-controller-sa"
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:aud" = "sts.amazonaws.com"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ebs_csi" {
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy"
  role       = aws_iam_role.ebs_csi.name
}

# ------------------------------------------------------------------------------
# IRSA: Kargo Controller (assumes project-specific roles)
# ------------------------------------------------------------------------------

resource "aws_iam_role" "kargo_controller" {
  name = "${local.cluster_name}-kargo-controller"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRoleWithWebIdentity"
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.eks.arn
      }
      Condition = {
        StringEquals = {
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:sub" = "system:serviceaccount:kargo:kargo-controller"
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:aud" = "sts.amazonaws.com"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "kargo_controller" {
  name = "${local.cluster_name}-kargo-assume-project-roles"
  role = aws_iam_role.kargo_controller.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "sts:AssumeRole"
      Resource = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/kargo-project-*"
    }]
  })
}

# ------------------------------------------------------------------------------
# Kargo Project Role: hermes (ECR read access)
# ------------------------------------------------------------------------------

resource "aws_iam_role" "kargo_project_hermes" {
  name = "kargo-project-hermes"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        AWS = aws_iam_role.kargo_controller.arn
      }
    }]
  })
}

resource "aws_iam_role_policy" "kargo_project_hermes" {
  name = "kargo-project-hermes-ecr"
  role = aws_iam_role.kargo_project_hermes.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "ecr:GetAuthorizationToken"
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchCheckLayerAvailability",
          "ecr:ListImages",
          "ecr:DescribeImages",
        ]
        Resource = var.ecr_repository_arns
      },
    ]
  })
}

# ------------------------------------------------------------------------------
# IRSA: Crossplane AWS Provider
# ------------------------------------------------------------------------------

resource "aws_iam_role" "crossplane" {
  name = "${local.cluster_name}-crossplane"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRoleWithWebIdentity"
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.eks.arn
      }
      Condition = {
        StringLike = {
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:sub" = "system:serviceaccount:crossplane-system:provider-aws-*"
        }
        StringEquals = {
          "${replace(aws_iam_openid_connect_provider.eks.url, "https://", "")}:aud" = "sts.amazonaws.com"
        }
      }
    }]
  })
}

# Finding 6. This policy was rds:*, elasticache:*, secretsmanager:* and ssm:* on
# Resource = "*". The worst of those was secretsmanager:* on *, which let an in-cluster
# controller read or overwrite every secret in the AWS account — including the ones this
# very cluster's ESO role is carefully scoped away from, three resources up in this file.
#
# The action lists below are derived from what the compositions in infra/crossplane
# actually create:
#
#   rds          SubnetGroup, ClusterParameterGroup, Cluster, ClusterInstance
#                (compositions/aws/database.yaml)
#   elasticache  SubnetGroup, ReplicationGroup
#                (compositions/aws/cache.yaml)
#   secretsmanager  Secret, under the name prefix hermes/<env>/
#                (compositions/aws/secrets.yaml)
#   ssm          Parameter, under the path /hermes/<env>/
#                (compositions/aws/secrets.yaml)
#   ec2          SecurityGroup, SecurityGroupIngressRule
#                (both of the above)
#
# Scoping honestly, and where it stops:
#
#   secretsmanager and ssm ARE prefix-scoped — secret:hermes/* and parameter/hermes/* —
#   the same shape as aws_iam_role_policy.external_secrets above. That closes the
#   read-every-secret hole, which was the substance of the finding.
#
#   rds and elasticache are scoped by resource TYPE and account, not by name, because
#   Crossplane derives external names for those resources and there is no stable prefix
#   to match on. Making them prefix-scopable means enforcing a naming convention in the
#   compositions first; that is a larger change than this one and is recorded as a
#   follow-up rather than smuggled in here.
#
#   Describe/List actions sit on "*" because AWS does not support resource-level
#   permissions for most of them (rds:DescribeDBEngineVersions and
#   ssm:DescribeParameters among them). They are read-only metadata.
#
#   The ec2 statement is UNCHANGED and still broad. Narrowing it wants a tag condition
#   (aws:ResourceTag/managed-by), and whether upjet tags a security group at creation or
#   in a follow-up call decides whether such a condition wedges provisioning entirely.
#   There is no cluster here to find that out on, and a policy that deadlocks the
#   provider is worse than the one being replaced. Left alone deliberately; recorded as a
#   follow-up.
#
# UNVERIFIED: no AWS account is reachable from this work, so this policy has been proven
# to be well-formed IAM and nothing more. It has NOT been proven sufficient — the first
# `terraform apply` plus a Crossplane claim is what will establish whether an action is
# missing. Expect AccessDenied on the first run to name the gap precisely; that is a far
# better failure than the account-wide grant this replaces.
resource "aws_iam_role_policy" "crossplane" {
  name = "${local.cluster_name}-crossplane"
  role = aws_iam_role.crossplane.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "RdsRead"
        Effect = "Allow"
        Action = [
          "rds:Describe*",
          "rds:ListTagsForResource",
        ]
        Resource = "*"
      },
      {
        Sid    = "RdsWrite"
        Effect = "Allow"
        Action = [
          "rds:CreateDBSubnetGroup",
          "rds:ModifyDBSubnetGroup",
          "rds:DeleteDBSubnetGroup",
          "rds:CreateDBClusterParameterGroup",
          "rds:ModifyDBClusterParameterGroup",
          "rds:ResetDBClusterParameterGroup",
          "rds:DeleteDBClusterParameterGroup",
          "rds:CreateDBCluster",
          "rds:ModifyDBCluster",
          "rds:DeleteDBCluster",
          "rds:CreateDBInstance",
          "rds:ModifyDBInstance",
          "rds:DeleteDBInstance",
          "rds:CreateDBClusterSnapshot",
          "rds:DeleteDBClusterSnapshot",
          "rds:AddTagsToResource",
          "rds:RemoveTagsFromResource",
        ]
        Resource = [
          "arn:aws:rds:*:${data.aws_caller_identity.current.account_id}:cluster:*",
          "arn:aws:rds:*:${data.aws_caller_identity.current.account_id}:db:*",
          "arn:aws:rds:*:${data.aws_caller_identity.current.account_id}:subgrp:*",
          "arn:aws:rds:*:${data.aws_caller_identity.current.account_id}:cluster-pg:*",
          "arn:aws:rds:*:${data.aws_caller_identity.current.account_id}:pg:*",
          "arn:aws:rds:*:${data.aws_caller_identity.current.account_id}:cluster-snapshot:*",
        ]
      },
      {
        Sid    = "ElastiCacheRead"
        Effect = "Allow"
        Action = [
          "elasticache:Describe*",
          "elasticache:ListTagsForResource",
        ]
        Resource = "*"
      },
      {
        Sid    = "ElastiCacheWrite"
        Effect = "Allow"
        Action = [
          "elasticache:CreateCacheSubnetGroup",
          "elasticache:ModifyCacheSubnetGroup",
          "elasticache:DeleteCacheSubnetGroup",
          "elasticache:CreateReplicationGroup",
          "elasticache:ModifyReplicationGroup",
          "elasticache:ModifyReplicationGroupShardConfiguration",
          "elasticache:DeleteReplicationGroup",
          "elasticache:IncreaseReplicaCount",
          "elasticache:DecreaseReplicaCount",
          "elasticache:CreateSnapshot",
          "elasticache:DeleteSnapshot",
          "elasticache:AddTagsToResource",
          "elasticache:RemoveTagsFromResource",
        ]
        Resource = [
          "arn:aws:elasticache:*:${data.aws_caller_identity.current.account_id}:replicationgroup:*",
          "arn:aws:elasticache:*:${data.aws_caller_identity.current.account_id}:subnetgroup:*",
          "arn:aws:elasticache:*:${data.aws_caller_identity.current.account_id}:cluster:*",
          "arn:aws:elasticache:*:${data.aws_caller_identity.current.account_id}:snapshot:*",
        ]
      },
      {
        # The statement finding 6 was really about. `secret:hermes/*` matches the
        # six-character suffix AWS appends to a secret ARN, which is why CreateSecret
        # can be scoped here at all — the same assumption the ESO policy above makes.
        Sid    = "SecretsManagerHermesPrefixOnly"
        Effect = "Allow"
        Action = [
          "secretsmanager:CreateSecret",
          "secretsmanager:DescribeSecret",
          "secretsmanager:GetSecretValue",
          "secretsmanager:PutSecretValue",
          "secretsmanager:UpdateSecret",
          "secretsmanager:DeleteSecret",
          "secretsmanager:RestoreSecret",
          "secretsmanager:ListSecretVersionIds",
          "secretsmanager:TagResource",
          "secretsmanager:UntagResource",
        ]
        Resource = "arn:aws:secretsmanager:*:${data.aws_caller_identity.current.account_id}:secret:hermes/*"
      },
      {
        Sid    = "SsmHermesPrefixOnly"
        Effect = "Allow"
        Action = [
          "ssm:PutParameter",
          "ssm:GetParameter",
          "ssm:GetParameters",
          "ssm:GetParameterHistory",
          "ssm:DeleteParameter",
          "ssm:DeleteParameters",
          "ssm:LabelParameterVersion",
          "ssm:AddTagsToResource",
          "ssm:RemoveTagsFromResource",
          "ssm:ListTagsForResource",
        ]
        Resource = "arn:aws:ssm:*:${data.aws_caller_identity.current.account_id}:parameter/hermes/*"
      },
      {
        # ssm:DescribeParameters does not support resource-level permissions. It returns
        # parameter metadata only, never values.
        Sid      = "SsmDescribeParametersNoResourceLevelSupport"
        Effect   = "Allow"
        Action   = "ssm:DescribeParameters"
        Resource = "*"
      },
      {
        # Unchanged from before this commit. See the note above on why.
        Sid    = "Ec2SecurityGroups"
        Effect = "Allow"
        Action = [
          "ec2:CreateSecurityGroup",
          "ec2:DeleteSecurityGroup",
          "ec2:AuthorizeSecurityGroupIngress",
          "ec2:AuthorizeSecurityGroupEgress",
          "ec2:RevokeSecurityGroupIngress",
          "ec2:RevokeSecurityGroupEgress",
          "ec2:Describe*",
          "ec2:CreateTags",
        ]
        Resource = "*"
      },
    ]
  })
}

# ------------------------------------------------------------------------------
# EKS Addons
# ------------------------------------------------------------------------------

resource "aws_eks_addon" "vpc_cni" {
  cluster_name = aws_eks_cluster.main.name
  addon_name   = "vpc-cni"

  depends_on = [aws_eks_node_group.main]
}

resource "aws_eks_addon" "coredns" {
  cluster_name = aws_eks_cluster.main.name
  addon_name   = "coredns"

  depends_on = [aws_eks_node_group.main]
}

resource "aws_eks_addon" "kube_proxy" {
  cluster_name = aws_eks_cluster.main.name
  addon_name   = "kube-proxy"

  depends_on = [aws_eks_node_group.main]
}

resource "aws_eks_addon" "ebs_csi" {
  cluster_name             = aws_eks_cluster.main.name
  addon_name               = "aws-ebs-csi-driver"
  service_account_role_arn = aws_iam_role.ebs_csi.arn

  depends_on = [aws_eks_node_group.main]
}
