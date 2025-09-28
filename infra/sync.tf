resource "google_service_account" "job" {
  account_id   = "gmail-organizer"
  display_name = "Gmail Organizer"
  description  = "Used for long-running Gmail re-organization."
  disabled     = false
}

resource "google_service_account_iam_member" "job_gha_access" {
  service_account_id = google_service_account.job.id
  role               = "roles/iam.serviceAccountUser"
  member             = data.google_service_account.gha.member
}

resource "google_artifact_registry_repository" "ghcr_proxy" {
  location      = var.region
  repository_id = "gmail-organizer-ghcr-proxy"
  description   = "Pull-through cache for ghcr.io/arikkfir-org/gmail-organizer"
  format        = "DOCKER"
  mode          = "REMOTE_REPOSITORY"

  remote_repository_config {
    docker_repository {
      custom_repository {
        uri = "https://ghcr.io"
      }
    }
  }
}

resource "google_artifact_registry_repository_iam_member" "proxy_job_reader" {
  repository = google_artifact_registry_repository.ghcr_proxy.id
  role       = "roles/artifactregistry.reader"
  member     = google_service_account.job.member
}

resource "google_project_iam_member" "job" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
  ])
  project = var.project_id
  role    = each.key
  member  = google_service_account.job.member
}

resource "google_secret_manager_secret" "sync" {
  for_each = toset([
    "sync_source_username",
    "sync_source_password",
    "sync_target_username",
    "sync_target_password",
  ])
  secret_id = each.key
  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }
}

resource "google_secret_manager_secret_iam_member" "sync_job_access" {
  for_each = toset([
    "sync_source_username",
    "sync_source_password",
    "sync_target_username",
    "sync_target_password",
  ])
  secret_id = google_secret_manager_secret.sync[each.key].id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.job.member
}

resource "google_secret_manager_secret_version" "sync_source_username" {
  secret                 = google_secret_manager_secret.sync["sync_source_username"].id
  secret_data_wo_version = var.sync_secrets_version
  secret_data_wo         = var.sync_source_username
  deletion_policy        = "DISABLE"
}

resource "google_secret_manager_secret_version" "sync_source_password" {
  secret                 = google_secret_manager_secret.sync["sync_source_password"].id
  secret_data_wo_version = var.sync_secrets_version
  secret_data_wo         = var.sync_source_password
  deletion_policy        = "DISABLE"
}

resource "google_secret_manager_secret_version" "sync_target_username" {
  secret                 = google_secret_manager_secret.sync["sync_target_username"].id
  secret_data_wo_version = var.sync_secrets_version
  secret_data_wo         = var.sync_target_username
  deletion_policy        = "DISABLE"
}

resource "google_secret_manager_secret_version" "sync_target_password" {
  secret                 = google_secret_manager_secret.sync["sync_target_password"].id
  secret_data_wo_version = var.sync_secrets_version
  secret_data_wo         = var.sync_target_password
  deletion_policy        = "DISABLE"
}

resource "google_cloud_run_v2_job" "sync" {
  name         = "gmail-organizer-sync"
  location     = var.region
  launch_stage = "BETA"
  template {
    task_count = 1
    template {
      service_account = google_service_account.job.email
      timeout         = "${60 * 60 * 24 * 6}s"
      containers {
        image = "${google_artifact_registry_repository.ghcr_proxy.location}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.ghcr_proxy.repository_id}/arikkfir-org/gmail-organizer/sync:${var.sync_image_tag}"
        args = [
          "-batch-size", "5000",
          "-json-logging",
        ]
        resources {
          limits = {
            memory = "1Gi"
            cpu    = "1"
          }
        }
        env {
          name = "SOURCE_USERNAME"
          value_source {
            secret_key_ref {
              secret  = "sync_source_username"
              version = "latest"
            }
          }
        }
        env {
          name = "SOURCE_PASSWORD"
          value_source {
            secret_key_ref {
              secret  = "sync_source_password"
              version = "latest"
            }
          }
        }
        env {
          name = "TARGET_USERNAME"
          value_source {
            secret_key_ref {
              secret  = "sync_target_username"
              version = "latest"
            }
          }
        }
        env {
          name = "TARGET_PASSWORD"
          value_source {
            secret_key_ref {
              secret  = "sync_target_password"
              version = "latest"
            }
          }
        }
      }
    }
  }
}
