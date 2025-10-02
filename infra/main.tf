terraform {
  required_version = ">= 1.13.1"
  backend "gcs" {
    bucket = "arikkfir-gmail-organizer-devops"
    prefix = "terraform"
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

data "google_project" "current" {
  project_id = var.project_id
}

data "google_storage_bucket" "devops" {
  name = "arikkfir-gmail-organizer-devops"
}

resource "google_project_service" "iam" {
  service                    = "iam.googleapis.com"
  disable_dependent_services = true
}

resource "google_project_service" "iamcredentials" {
  service                    = "iamcredentials.googleapis.com"
  disable_dependent_services = true
}

resource "google_project_service" "cloudresourcemanager" {
  service                    = "cloudresourcemanager.googleapis.com"
  disable_dependent_services = true
}

resource "google_project_service" "run" {
  service                    = "run.googleapis.com"
  disable_dependent_services = true
}

resource "google_project_service" "pubsub" {
  service                    = "pubsub.googleapis.com"
  disable_dependent_services = true
}
