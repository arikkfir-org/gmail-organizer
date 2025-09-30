resource "google_service_account" "worker" {
  account_id   = "worker"
  display_name = "Runs the worker Cloud Run jobs."
}

resource "google_project_iam_member" "worker" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
  ])
  project = var.project_id
  role    = each.key
  member  = google_service_account.worker.member
}

resource "google_service_account_iam_member" "gha_actAs_worker" {
  service_account_id = google_service_account.worker.id
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.gha.member
}

resource "google_cloud_run_v2_job" "worker" {
  depends_on = [
    google_project_service.run,
    google_artifact_registry_repository_iam_member.worker,
    google_project_iam_member.worker,
    google_service_account_iam_member.gha_actAs_worker,
    google_firestore_database.default,
    google_secret_manager_secret.sync,
    google_secret_manager_secret_iam_member.sync_dispatcher_access,
    google_secret_manager_secret_version.sync_version,
  ]
  name                = "worker"
  location            = var.region
  deletion_protection = false

  template {
    template {
      service_account = google_service_account.worker.email
      timeout         = "${60 * 60 * 24}s"
      containers {
        image = "${google_artifact_registry_repository.ghcr_proxy.registry_uri}/arikkfir-org/gmail-organizer/worker:${var.image_tag}"
        resources {
          limits = {
            memory = "512Mi"
            cpu    = 1
          }
        }
        env {
          name  = "WORKER_MODE"
          value = "task"
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
        env {
          name  = "JSON_LOGGING"
          value = "true"
        }
        env {
          name  = "GCP_PROJECT_ID"
          value = var.project_id
        }
        env {
          name  = "DRY_RUN"
          value = "true"
        }
      }
    }
  }
}
