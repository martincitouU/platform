variable "region" {
  description = "Región de AWS"
  type        = string
  default     = "us-east-2"
}

variable "vpc_cidr" {
  description = "Rango de IPs de la VPC"
  type        = string
  default     = "10.0.0.0/16"
}