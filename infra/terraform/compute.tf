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

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.ssh.id
  description       = "Salida libre: apt, docker pull, runner de GitHub"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1" # todos los protocolos
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