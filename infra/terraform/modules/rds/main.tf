# ------------------------------------------------------------------------------
# Subnet Group
# ------------------------------------------------------------------------------

resource "aws_db_subnet_group" "main" {
  name       = "hermes-${var.environment}"
  subnet_ids = var.private_subnet_ids

  tags = {
    Name = "hermes-${var.environment}"
  }
}

# ------------------------------------------------------------------------------
# Security Group
# ------------------------------------------------------------------------------

resource "aws_security_group" "rds" {
  name_prefix = "hermes-${var.environment}-rds-"
  description = "Security group for Hermes Aurora cluster"
  vpc_id      = var.vpc_id

  ingress {
    description     = "PostgreSQL from EKS nodes"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [var.eks_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "hermes-${var.environment}-rds"
  }

  lifecycle {
    create_before_destroy = true
  }
}

# ------------------------------------------------------------------------------
# Password
# ------------------------------------------------------------------------------

resource "random_password" "master" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}|:?"
}

# ------------------------------------------------------------------------------
# Cluster Parameter Group
# ------------------------------------------------------------------------------

resource "aws_rds_cluster_parameter_group" "main" {
  name_prefix = "hermes-${var.environment}-"
  family      = "aurora-postgresql16"

  parameter {
    name  = "log_min_duration_statement"
    value = "1000"
  }

  tags = {
    Name = "hermes-${var.environment}"
  }

  lifecycle {
    create_before_destroy = true
  }
}

# ------------------------------------------------------------------------------
# Aurora Cluster
# ------------------------------------------------------------------------------

resource "aws_rds_cluster" "main" {
  cluster_identifier = "hermes-${var.environment}"

  engine         = "aurora-postgresql"
  engine_version = "16.4"

  database_name   = "hermes"
  master_username = "hermes"
  master_password = random_password.master.result

  db_subnet_group_name            = aws_db_subnet_group.main.name
  vpc_security_group_ids          = [aws_security_group.rds.id]
  db_cluster_parameter_group_name = aws_rds_cluster_parameter_group.main.name

  storage_encrypted = true

  backup_retention_period = var.backup_retention_period
  preferred_backup_window = "03:00-04:00"
  deletion_protection     = var.environment == "production"

  skip_final_snapshot       = var.environment != "production"
  final_snapshot_identifier = var.environment == "production" ? "hermes-${var.environment}-final" : null

  tags = {
    Name = "hermes-${var.environment}"
  }
}

# ------------------------------------------------------------------------------
# Aurora Instances
# ------------------------------------------------------------------------------

resource "aws_rds_cluster_instance" "main" {
  count = var.instance_count

  identifier         = "hermes-${var.environment}-${count.index}"
  cluster_identifier = aws_rds_cluster.main.id

  engine         = aws_rds_cluster.main.engine
  engine_version = aws_rds_cluster.main.engine_version
  instance_class = var.instance_class

  performance_insights_enabled = true

  tags = {
    Name = "hermes-${var.environment}-${count.index}"
  }
}
