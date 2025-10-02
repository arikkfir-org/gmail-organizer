resource "google_service_account" "dispatcher" {
  account_id   = "dispatcher"
  display_name = "Runs the dispatcher Cloud Run service."
}

resource "google_project_iam_member" "dispatcher" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
    "roles/pubsub.admin",
  ])
  project = var.project_id
  role    = each.key
  member  = google_service_account.dispatcher.member
}

resource "google_service_account_iam_member" "gha_actAs_dispatcher" {
  service_account_id = google_service_account.dispatcher.id
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.gha.member
}

resource "google_cloud_run_v2_job" "dispatcher" {
  depends_on = [
    google_project_service.pubsub,
    google_project_iam_member.gcp_pubsub_publish,
    google_project_service.run,
    google_artifact_registry_repository_iam_member.dispatcher,
    google_project_iam_member.dispatcher,
    google_service_account_iam_member.gha_actAs_dispatcher,
    google_secret_manager_secret.sync,
    google_secret_manager_secret_iam_member.sync_dispatcher_access,
    google_secret_manager_secret_version.sync_version,
  ]
  name                = "dispatcher"
  location            = var.region
  deletion_protection = false

  template {
    template {
      service_account = google_service_account.dispatcher.email
      timeout         = "${60 * 60 * 4}s"
      max_retries     = 3
      containers {
        image = "${google_artifact_registry_repository.ghcr_proxy.registry_uri}/arikkfir-org/gmail-organizer/dispatcher:${var.image_tag}"
        resources {
          limits = {
            memory = "512Mi"
            cpu    = 1
          }
        }
        env {
          name  = "PROCESSOR_ENDPOINT"
          value = google_cloud_run_v2_service.worker.uri
        }
        env {
          name  = "DISPATCHER_SERVICE_ACCOUNT_EMAIL"
          value = google_service_account.dispatcher.email
        }
        env {
          name = "SOURCE_ACCOUNT_USERNAME"
          value_source {
            secret_key_ref {
              secret  = "sync_source_username"
              version = "latest"
            }
          }
        }
        env {
          name = "SOURCE_ACCOUNT_PASSWORD"
          value_source {
            secret_key_ref {
              secret  = "sync_source_password"
              version = "latest"
            }
          }
        }
        env {
          name  = "JSON_LOGGING"
          value = "true"
        }
      }
    }
  }
}
