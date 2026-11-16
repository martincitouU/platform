output "public_ip" {
  description = "IP fija de la VM"
  value       = aws_eip.vm.public_ip
}

output "instance_id" {
  value = aws_instance.vm.id
}

output "ami_id" {
  description = "Imagen con la que se construyó la VM"
  value       = aws_instance.vm.ami
}

output "ssh_command" {
  value = "ssh -i ~/.ssh/platform-tf ubuntu@${aws_eip.vm.public_ip}"
}

output "k3s_public_ip" {
  description = "IP fija del nodo k3s"
  value       = aws_eip.k3s.public_ip
}

output "k3s_ssh_command" {
  value = "ssh -i ~/.ssh/platform-tf ubuntu@${aws_eip.k3s.public_ip}"
}

output "k3s_tunnel_command" {
  description = "Túnel para usar kubectl desde tu Mac sin exponer la API del cluster"
  value       = "ssh -i ~/.ssh/platform-tf -N -L 6443:127.0.0.1:6443 ubuntu@${aws_eip.k3s.public_ip}"
}
