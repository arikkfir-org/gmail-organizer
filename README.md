# Gmail Organizer

Gmail Organizer is a command-line tool written in Go for synchronizing emails from a source Gmail account to a target
Gmail account. It uses the IMAP protocol to ensure that all mailboxes and messages are replicated accurately.

The tool is designed to be run either locally for manual syncs or deployed as a scheduled Cloud Run Job on Google Cloud
Platform for automated, continuous synchronization.

## Project Overview

The primary function of this tool is to perform a one-way sync of mailboxes and emails. It achieves this by:

1. Connecting to both the source and target Gmail accounts via IMAP.
2. Listing all mailboxes (labels) in the source account.
3. Ensuring all source mailboxes exist in the target account, creating them if necessary.
4. Iterating through each mailbox and identifying messages present in the source but not in the target (by comparing
   `Message-Id`).
5. Copying the missing messages from the source to the target account, preserving flags and dates.

## Installation

You can install and run the tool in several ways.

### From Source

Ensure you have Go installed (version 1.21 or newer).

```sh
go install github.com/arikkfir-org/gmail-organizer/cli@latest
```

### With Docker

You can build a Docker image from the provided Dockerfile.

```sh
docker build -t gmail-organizer .
```

## Usage

The tool is executed as a single command with flags or environment variables for configuration.

**Important:** You must use a [Google Account App Password](https://support.google.com/accounts/answer/185833) for the
password fields, not your regular account password.

### Example

```sh
export SOURCE_USERNAME="source@gmail.com"
export SOURCE_PASSWORD="your-source-app-password"
export TARGET_USERNAME="target@gmail.com"
export TARGET_PASSWORD="your-target-app-password"

gmail-organizer --batch-size=100
```

To perform a dry run without making any changes:

```sh
gmail-organizer --dry-run
```

## Configuration

Configuration is managed via command-line flags, which can also be provided as environment variables.

| Flag                | Environment Variable | Description                                   | Default | Required |
|---------------------|----------------------|-----------------------------------------------|---------|----------|
| `--source-username` | `SOURCE_USERNAME`    | Source Gmail username (email address).        |         | Yes      |
| `--source-password` | `SOURCE_PASSWORD`    | Source Gmail App Password.                    |         | Yes      |
| `--target-username` | `TARGET_USERNAME`    | Target Gmail username (email address).        |         | Yes      |
| `--target-password` | `TARGET_PASSWORD`    | Target Gmail App Password.                    |         | Yes      |
| `--batch-size`      | `BATCH_SIZE`         | Number of messages to process in each batch.  | `5000`  | No       |
| `--dry-run`         | `DRY_RUN`            | If true, logs actions without executing them. | `false` | No       |
| `--json-logging`    | `JSON_LOGGING`       | If true, outputs logs in JSON format.         | `false` | No       |

## Deployment on GCP

The included `/infra` directory contains Terraform configurations to deploy this tool as a scheduled Cloud Run Job on
GCP. See the variables in `infra/variables.tf` for deployment configuration.

## Contributing

Contributions are welcome! Please feel free to submit a pull request.

1. Fork the repository.
2. Create a new branch (`git checkout -b feature/your-feature`).
3. Make your changes.
4. Commit your changes (`git commit -am 'Add some feature'`).
5. Push to the branch (`git push origin feature/your-feature`).
6. Create a new Pull Request.

Please ensure your code is formatted with `go fmt` before submitting.

## License

This project is licensed under the Apache 2.0 License. See the `LICENSE` file for details.