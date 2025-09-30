resource "google_artifact_registry_repository" "ghcr_proxy" {
  location      = var.region
  repository_id = "ghcr-proxy"
  description   = "Pull-through cache for ghcr.io"
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

resource "google_artifact_registry_repository_iam_member" "dispatcher" {
  repository = google_artifact_registry_repository.ghcr_proxy.id
  role       = "roles/artifactregistry.reader"
  member     = google_service_account.dispatcher.member
}

resource "google_artifact_registry_repository_iam_member" "worker" {
  repository = google_artifact_registry_repository.ghcr_proxy.id
  role       = "roles/artifactregistry.reader"
  member     = google_service_account.worker.member
}
