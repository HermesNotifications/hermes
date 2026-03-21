variable "ecr_repository_arns" {
  description = "List of ECR repository ARNs"
  type        = list(string)
}

variable "github_org" {
  description = "GitHub organization or user"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name"
  type        = string
  default     = "hermes"
}
