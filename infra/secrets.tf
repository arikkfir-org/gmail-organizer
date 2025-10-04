resource "google_project_service" "secretmanager" {
  service                    = "secretmanager.googleapis.com"
  disable_dependent_services = true
}

resource "google_secret_manager_secret" "sync" {
  depends_on = [google_project_service.secretmanager]
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

resource "google_secret_manager_secret_iam_member" "sync_worker_access" {
  for_each = toset([
    "sync_source_username",
    "sync_source_password",
    "sync_target_username",
    "sync_target_password",
  ])
  secret_id = google_secret_manager_secret.sync[each.key].id
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.worker.member
}

resource "google_secret_manager_secret_version" "sync_version" {
  for_each = {
    "sync_source_username" : var.sync_source_username,
    "sync_source_password" : var.sync_source_password,
    "sync_target_username" : var.sync_target_username,
    "sync_target_password" : var.sync_target_password,
  }
  secret                 = google_secret_manager_secret.sync[each.key].id
  secret_data_wo_version = var.sync_secrets_version
  secret_data_wo         = each.value
  deletion_policy        = "DISABLE"
}
