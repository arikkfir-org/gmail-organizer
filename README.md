# Gmail Organizer

Gmail Organizer is a distributed, cloud-native application for synchronizing emails from a source Gmail account to a
target Gmail account. It is designed to be deployed on Google Cloud Platform and leverages a dispatcher/worker
architecture to handle large volumes of emails efficiently.

## Table of Contents

- [Project Overview](#project-overview)
    - [High-level Architecture](#high-level-architecture)
- [Cloud Infrastructure](#cloud-infrastructure)
    - [IaC Overview](#iac-overview)
- [Implementation Details](#implementation-details)
- [Local Development](#local-development)
- [CI/CD](#cicd)
- [Contributing](#contributing)
- [License](#license)

## Project Overview

The application is composed of two main Go-based components: a **dispatcher** and a **worker**.

1. **Dispatcher**: This is a Cloud Run Job responsible for initiating the synchronization process. It connects to the
   source Gmail account, lists all messages, chunks them into manageable batches, and publishes them as messages to a
   Pub/Sub topic.

2. **Worker**: This is a Cloud Run Service that processes messages from the Pub/Sub subscription. Each instance of the
   worker is responsible for processing a single message from the source account, fetching it, and copying it to the
   target account if it doesn't already exist.

This architecture allows for parallel processing of messages, making the synchronization process fast, scalable, and
resilient.

### High-level Architecture

```
┌───────────────────┐      ┌───────────────────┐      ┌───────────────────┐
│                   │      │                   │      │                   │
│  Source Gmail     ├─────►│    Dispatcher     ├─────►│      Pub/Sub      │
│   (IMAP)          │      │  (Cloud Run Job)  │      │                   │
│                   │      │                   │      │                   │
└───────────────────┘      └───────────────────┘      └───────────────────┘
        ▲                                                 │
        │                                                 ▼
        │                                     ┌───────────────────┐
        │                                     │                   │
        └────────────────────────────────────►│      Worker       │
                                              │ (Cloud Run Service) │
                                              │                   │
                                              └───────────────────┘
                                                          │
                                                          ▼
                                              ┌───────────────────┐
                                              │                   │
                                              │    Target Gmail   │
                                              │      (IMAP)       │
                                              │                   │
                                              └───────────────────┘
```

## Cloud Infrastructure

The entire infrastructure for this application is defined as code using Terraform.

### IaC Overview

The Terraform setup in the `/infra` directory provisions the following Google Cloud resources:

* **Cloud Run**: A Job for the `dispatcher` and a Service for the `worker`.
* **Pub/Sub**: A topic for messages and a corresponding subscription to trigger the worker.
* **Secret Manager**: Securely stores Gmail App Passwords.
* **Artifact Registry**: A pull-through cache for Docker images from `ghcr.io`.
* **IAM**: Service Accounts with fine-grained permissions for each component.
* **Workload Identity Federation**: To securely authenticate GitHub Actions with Google Cloud for CI/CD.

To deploy the infrastructure, you will need to:

1. Configure the variables in `infra/variables.tf`.
2. Set up the Terraform GCS backend.
3. Run `terraform init` and `terraform apply`.

## Implementation Details

* **Go**: Both the dispatcher and worker are written in Go.
* **IMAP**: The application uses the `go-imap` library to communicate with Gmail's IMAP servers.
* **Docker**: The services are containerized using Dockerfiles provided (`dispatcher.Dockerfile`, `worker.Dockerfile`).
  The base images are distroless for a smaller footprint and improved security.

**Note:** You must use a [Google Account App Password](https://support.google.com/accounts/answer/185833) for
authentication, not your regular account password.

## Local Development

To set up a local working environment, you will need:

1. **Go 1.25+**: Install the Go programming language.
2. **Docker**: For building and running containers.
3. **gcloud CLI**: To authenticate with Google Cloud.
4. **Terraform**: For managing infrastructure.

**Setup Steps:**

1. Clone the repository.
2. Authenticate with GCP: `gcloud auth application-default login`.
3. Set up the required environment variables for the dispatcher and worker. It is recommended to use a `.env` file for
   this.
4. Run the services locally using `go run ./cmd/dispatcher` or `go run ./cmd/worker`.

## CI/CD

This project uses GitHub Actions for its CI/CD pipeline, defined in the `.github/workflows` directory.

* **`deploy.yml`**: This workflow triggers on pushes to the `main` branch. It builds and pushes the Docker images to
  GHCR, and then applies the Terraform configuration to deploy the services to Cloud Run.
* **Gemini Workflows**: The `gemini-*.yml` files integrate Google's Gemini AI for automated code reviews, issue triage,
  and other development tasks.

## Contributing

Contributions are welcome! We follow standard open-source practices. Please read our [CONTRIBUTING.md](CONTRIBUTING.md)
file for details on how to contribute, including our code of conduct and commit message conventions.

## License

This project is licensed under the Apache 2.0 License. See the `LICENSE` file for details.
