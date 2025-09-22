package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/seqsense/s3sync/v2"
)

// executeSync handles the common logic for running s3sync with output capturing.
func executeSync(source string, destination string, awsCredentials Credentials, syncOptions ...s3sync.Option) (string, error) {
	// --- Capture stderr ---
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stderr = w
	log.SetOutput(w)

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(awsCredentials.Key, awsCredentials.Secret, "")),
		config.WithRegion("ap-southeast-2"), // TODO: This should be moved to the config file
	)
	if err != nil {
		w.Close()
		os.Stderr = oldStderr
		log.SetOutput(oldStderr)
		return "", err
	}

	syncManager := s3sync.New(cfg, syncOptions...)
	syncErr := syncManager.Sync(context.TODO(), source, destination)

	// --- Restore stderr and read captured output ---
	w.Close()
	os.Stderr = oldStderr
	log.SetOutput(oldStderr)

	var buf bytes.Buffer
	if _, readErr := io.Copy(&buf, r); readErr != nil {
		return "", readErr
	}

	return buf.String(), syncErr
}

// Sync performs a synchronization from a source to a destination.
func Sync(source string, destination string, awsCredentials Credentials) (string, error) {
	return executeSync(source, destination, awsCredentials, s3sync.WithDelete())
}

// SyncDryRun performs a dry run of a synchronization to see what changes would be made.
func SyncDryRun(source string, destination string, awsCredentials Credentials) (bool, string, error) {
	output, err := executeSync(source, destination, awsCredentials, s3sync.WithDelete(), s3sync.WithDryRun())
	isChanges := output != ""
	return isChanges, output, err
}