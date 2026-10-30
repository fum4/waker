# .terraform.lock.hcl (committed) freezes the exact provider build + checksums;
# this is the human-readable floor.
terraform {
  required_version = ">= 1.9"

  required_providers {
    github = {
      source  = "integrations/github"
      version = "~> 6.13"
    }
  }
}
