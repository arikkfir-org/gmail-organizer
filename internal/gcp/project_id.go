package gcp

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/oauth2/google"
)

func GetProjectId(ctx context.Context) (string, error) {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return "", fmt.Errorf("failed inferring current GCP project: %w", err)
		} else if creds.ProjectID == "" {
			return "", fmt.Errorf("failed inferring current GCP project: project ID in ADC is empty")
		} else {
			return creds.ProjectID, nil
		}
	} else {
		return projectID, nil
	}
}
