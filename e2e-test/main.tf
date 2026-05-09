terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-2"
}

# Single t3.micro on Linux on-demand — a small but non-zero cost diff
# (~$7-8/month) so the rendered comment exercises the Top-movers table
# and the LLM narrative path. The AMI is a well-known Amazon Linux 2
# image in us-east-2; terraform plan does not resolve it (no data
# sources), so the value flows straight into the plan JSON for the
# CloudOracle parser to read.
resource "aws_instance" "self_test" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = "t3.micro"

  tags = {
    Name = "cloudoracle-self-test"
  }
}
