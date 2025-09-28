terraform {
  required_version = ">= 1.13.1"
  backend "gcs" {
    bucket = "arikkfir-devops"
    prefix = "terraform/gmail-organizer"
  }
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 7.4.0"
    }
  }
}

provider "google" {
  project = var.project_id
}

data "google_service_account" "gha" {
  account_id = "gmail-organizer-gha"
}
