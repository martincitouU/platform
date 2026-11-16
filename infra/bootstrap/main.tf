terraform {
  required_version = ">= 1.10"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" {
  region = "us-east-2"
  default_tags { tags = { Project = "platform", ManagedBy = "terraform-bootstrap" } }
}

data "aws_caller_identity" "current" {}

# ---------- El bucket del state ----------
resource "aws_s3_bucket" "tfstate" {
  # los nombres de bucket son únicos en todo AWS: el ID de cuenta lo garantiza
  bucket = "platform-tfstate-${data.aws_caller_identity.current.account_id}"

  lifecycle {
    prevent_destroy = true   # terraform destroy se niega a borrarlo
  }
}

resource "aws_s3_bucket_versioning" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id
  versioning_configuration { status = "Enabled" }
}

# Riesgo aceptado: SSE-S3 (AES256) cifra el state en reposo con llaves administradas por AWS.
# Una llave KMS propia (CMK) agregaría rotación y auditoría por llave, pero cuesta ~USD 1/mes
# más llamadas a KMS; en un proyecto de una persona el control de acceso lo dan IAM y la
# política del bucket (solo HTTPS, sin acceso público). Revisar si el bucket se comparte.
#trivy:ignore:AWS-0132
resource "aws_s3_bucket_server_side_encryption_configuration" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}

resource "aws_s3_bucket_public_access_block" "tfstate" {
  bucket                  = aws_s3_bucket.tfstate.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Rechaza cualquier acceso que no sea por HTTPS
resource "aws_s3_bucket_policy" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.tfstate.arn, "${aws_s3_bucket.tfstate.arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })

  depends_on = [aws_s3_bucket_public_access_block.tfstate]
}

output "bucket" {
  value = aws_s3_bucket.tfstate.bucket
}