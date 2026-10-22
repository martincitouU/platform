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

variable "public_subnet_cidr" {
  description = "Rango de IPs de la subnet pública"
  type        = string
  default     = "10.0.1.0/24"
}

variable "my_ip_cidr" {
  description = "Tu IP pública como /32: la única que puede entrar por SSH"
  type        = string
  # Agregar en cada sesión

  validation {
    condition     = can(cidrhost(var.my_ip_cidr, 0)) && endswith(var.my_ip_cidr, "/32")
    error_message = "Debe ser una sola IP en formato x.x.x.x/32."
  }
}

variable "public_key_path" {
  description = "Ruta de la llave pública que se instala en la VM"
  type        = string
  default     = "~/.ssh/platform-tf.pub"
}

variable "instance_type" {
  description = "Tamaño de la VM"
  type        = string
  default     = "t3.micro"
}