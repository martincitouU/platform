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