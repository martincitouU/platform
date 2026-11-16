terraform {
  backend "s3" {
    bucket       = "platform-tfstate-856121136559"
    key          = "platform/terraform.tfstate"
    region       = "us-east-2"
    encrypt      = true
    use_lockfile = true
  }
}
