terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" {
  region                      = "us-east-2"
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
  access_key                  = "test"
  secret_key                  = "test"
}

resource "aws_instance" "web" {
  ami           = "ami-12345"
  instance_type = "t3.large"

  root_block_device {
    volume_size = 50
    volume_type = "gp3"
  }
}
resource "aws_db_instance" "main" {
  identifier          = "main"
  engine              = "postgres"
  instance_class      = "db.t3.medium"
  allocated_storage   = 100
  username            = "admin"
  password            = "changeme"
  skip_final_snapshot = true
}

resource "aws_ebs_volume" "data" {
  availability_zone = "us-east-2a"
  size              = 200
  type              = "gp3"
  throughput        = 125
}

resource "aws_lambda_function" "worker" {
  function_name = "worker"
  role          = "arn:aws:iam::123456789012:role/lambda"
  handler       = "index.handler"
  runtime       = "python3.12"
  memory_size   = 512
  timeout       = 30
  filename      = "dummy.zip"
  architectures = ["arm64"]
}

resource "aws_nat_gateway" "main" {
  allocation_id = "eipalloc-12345"
  subnet_id     = "subnet-12345"
}

resource "aws_rds_cluster_instance" "replica" {
  identifier         = "replica"
  cluster_identifier = "main-cluster"
  instance_class     = "db.r5.large"
  engine             = "aurora-postgresql"
}
