resource "aws_security_group" "ssh" {
  name        = "platform-ssh"
  description = "SSH solo desde la IP del administrador"
  vpc_id      = aws_vpc.main.id

  tags = { Name = "platform-ssh" }
}

resource "aws_vpc_security_group_ingress_rule" "ssh" {
  security_group_id = aws_security_group.ssh.id
  description       = "SSH desde mi IP"
  cidr_ipv4         = var.my_ip_cidr
  ip_protocol       = "tcp"
  from_port         = 22
  to_port           = 22
}

locals {
  egress_ports = {
    https = 443 # GitHub (runner), GHCR, repositorio de Docker, apt
    http  = 80  # espejos de apt de Ubuntu en EC2
  }
}

# Riesgo aceptado: la VM debe alcanzar servicios con IPs cambiantes (GitHub, GHCR, apt),
# así que el destino no se puede acotar a rangos fijos. Se limita a TCP 80 y 443.
#trivy:ignore:AWS-0104
resource "aws_vpc_security_group_egress_rule" "web" {
  for_each = local.egress_ports

  security_group_id = aws_security_group.ssh.id
  description       = "Salida ${each.key}"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"
  from_port         = each.value
  to_port           = each.value
}

resource "aws_key_pair" "main" {
  key_name   = "platform-tf"
  public_key = file(pathexpand(var.public_key_path))
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }
}

resource "aws_instance" "vm" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.ssh.id]
  key_name               = aws_key_pair.main.key_name

  metadata_options {
    http_tokens = "required"
  }

  root_block_device {
    volume_size = 20
    volume_type = "gp3"
    encrypted   = true
  }

  lifecycle {
    ignore_changes = [ami]
  }

  tags = { Name = "platform-vm-02" }
}

resource "aws_eip" "vm" {
  domain   = "vpc"
  instance = aws_instance.vm.id

  depends_on = [aws_internet_gateway.main]

  tags = { Name = "platform-vm-02-eip" }
}

# ---------- Nodo k3s (fase 3) ----------
resource "aws_instance" "k3s" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.k3s_instance_type
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.ssh.id]
  key_name               = aws_key_pair.main.key_name

  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 1 # los pods no alcanzan los metadatos (ni sus credenciales)
  }

  root_block_device {
    volume_size = 30 # imágenes de contenedores y, más adelante, datos de Prometheus
    volume_type = "gp3"
    encrypted   = true
  }

  lifecycle {
    ignore_changes = [ami]
  }

  tags = { Name = "platform-k3s-01" }
}

resource "aws_eip" "k3s" {
  domain   = "vpc"
  instance = aws_instance.k3s.id

  depends_on = [aws_internet_gateway.main]

  tags = { Name = "platform-k3s-01-eip" }
}
