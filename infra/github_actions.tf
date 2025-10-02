resource "google_service_account" "gha" {
  account_id   = "github-actions"
  display_name = "Performs actions in the project from GitHub Actions workflows."
}

resource "google_project_iam_member" "gha_run_admin" {
  project = var.project_id
  role    = "roles/run.admin"
  member  = google_service_account.gha.member
}

resource "google_project_iam_member" "gha_iam_admin" {
  project = var.project_id
  role    = "roles/iam.serviceAccountAdmin"
  member  = google_service_account.gha.member
}

resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github"
  display_name              = "GitHub"
  description               = "Pool for GitHub identities."
}

resource "google_iam_workload_identity_pool_provider" "gha" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-actions"
  display_name                       = "GitHub Actions Provider"
  description                        = "OIDC provider for GitHub Actions identities."

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }

  attribute_mapping = {
    "google.subject"             = "assertion.sub"
    "attribute.actor"            = "assertion.actor"
    "attribute.aud"              = "assertion.aud"
    "attribute.environment"      = "assertion.environment"
    "attribute.event_name"       = "assertion.event_name"
    "attribute.ref"              = "assertion.ref"
    "attribute.ref_type"         = "assertion.ref_type"
    "attribute.repository"       = "assertion.repository"
    "attribute.repository_owner" = "assertion.repository_owner"
    "attribute.sha"              = "assertion.sha"
    "attribute.workflow"         = "assertion.workflow"
  }

  attribute_condition = "assertion.repository_owner=='arikkfir-org'"
}

resource "google_service_account_iam_member" "github_actions" {
  service_account_id = google_service_account.gha.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/projects/${data.google_project.current.number}/locations/global/workloadIdentityPools/github/attribute.repository/arikkfir-org/gmail-organizer"
}

resource "google_storage_bucket_iam_member" "devops_gha" {
  bucket = data.google_storage_bucket.devops.name
  role   = "roles/storage.objectAdmin"
  member = google_service_account.gha.member
}
