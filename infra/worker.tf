resource "google_service_account" "worker" {
  account_id   = "worker"
  display_name = "Runs the worker Cloud Run job."
}

resource "google_project_iam_member" "worker" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
    "roles/cloudtrace.agent",
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

resource "google_service_account_iam_member" "worker_actAs_worker" {
  service_account_id = google_service_account.worker.id
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.worker.member
}

resource "google_secret_manager_secret" "otel_config" {
  depends_on = [google_project_service.secretmanager]
  secret_id  = "otel-config"
  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }
}

resource "google_secret_manager_secret_iam_member" "otel_config_worker_access" {
  secret_id = google_secret_manager_secret.otel_config.id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.worker.member
}

resource "google_secret_manager_secret_version" "otel_config" {
  secret                 = google_secret_manager_secret.otel_config.id
  secret_data_wo_version = var.sync_secrets_version
  secret_data_wo         = file("./otel.yaml")
  deletion_policy        = "DISABLE"
}

resource "google_cloud_run_v2_job" "worker" {
  depends_on = [
    google_project_service.pubsub,
    google_project_service.run,
    google_artifact_registry_repository_iam_member.worker,
    google_project_iam_member.worker,
    google_service_account_iam_member.gha_actAs_worker,
    google_secret_manager_secret.sync,
    google_secret_manager_secret_iam_member.sync_worker_access,
    google_secret_manager_secret_version.sync_version,
    google_secret_manager_secret_version.otel_config,
  ]
  name                = "worker"
  location            = var.region
  deletion_protection = false
  launch_stage        = "BETA"
  template {
    template {
      service_account = google_service_account.worker.email
      timeout         = "${60 * 60 * 24 * 6}s"
      max_retries     = 1
      volumes {
        name = "otel-config"
        secret {
          secret       = google_secret_manager_secret.otel_config.secret_id
          default_mode = 292 # 0444
          items {
            path    = "config.yaml"
            version = "latest"
          }
        }
      }
      containers {
        image = "${google_artifact_registry_repository.ghcr_proxy.registry_uri}/arikkfir-org/gmail-organizer/worker:${var.image_tag}"
        resources {
          limits = {
            memory = "2Gi"
            cpu    = 2
          }
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
          name = "TARGET_ACCOUNT_USERNAME"
          value_source {
            secret_key_ref {
              secret  = "sync_target_username"
              version = "latest"
            }
          }
        }
        env {
          name = "TARGET_ACCOUNT_PASSWORD"
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
          name  = "OTEL_EXPORTER_OTLP_ENDPOINT"
          value = "http://localhost:4317"
        }
        env {
          name  = "MAX_EMAILS"
          value = "9999999"
        }
        env {
          name  = "LOG_LEVEL"
          value = "info"
        }
      }
      containers {
        name  = "otel-collector"
        image = "us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google:0.135.0"
        args = [
          "--config=/etc/otelcol-google/config.yaml",
          "--set=service.telemetry.logs.encoding=json",
        ]
        ports {
          container_port = 4317
          name           = "http1"
        }
        resources {
          limits = {
            memory = "512Mi"
            cpu    = 1
          }
        }
        startup_probe {
          initial_delay_seconds = 30
          period_seconds        = 30
          timeout_seconds       = 30
          failure_threshold     = 3
          http_get {
            port = 13133
            path = "/"
          }
        }
        volume_mounts {
          mount_path = "/etc/otelcol-google"
          name       = "otel-config"
        }
      }
    }
  }
}
