resource "google_project_service" "firestore" {
  service                    = "firestore.googleapis.com"
  disable_dependent_services = true
}

resource "google_firestore_database" "default" {
  depends_on = [
    google_project_service.firestore,
    google_project_iam_member.gha_firestore_owner,
  ]

  name        = "(default)"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"
}
