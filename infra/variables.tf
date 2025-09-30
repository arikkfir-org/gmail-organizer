variable "project_id" {
  description = "Target project to deploy to."
  type        = string
}

variable "region" {
  description = "Target region to deploy to."
  type        = string
}

variable "sync_secrets_version" {
  description = "Version for the secrets to store in GCP Secrets Manager. Update this to trigger updates to the secrets."
  type        = number
}

variable "sync_source_username" {
  description = "Username of the source account to sync from."
  type        = string
  sensitive   = true
}

variable "sync_source_password" {
  description = "Password of the source account to sync from."
  type        = string
  sensitive   = true
}

variable "sync_target_username" {
  description = "Username of the target account to sync to."
  type        = string
  sensitive   = true
}

variable "sync_target_password" {
  description = "Password of the target account to sync from."
  type        = string
  sensitive   = true
}

variable "image_tag" {
  description = "The tag of the container images that will run."
  type        = string
}
