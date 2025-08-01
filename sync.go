package main

import (
	// "fmt"
	// "sync" // For thread-safe collection of actions
	"os"
	"bytes"
	"io"
	"log"
	"context"
	// "regexp" // For parsing output
	// "strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/seqsense/s3sync"
)

func Sync(source string, destination string, awsCredentials Credentials) (string, error) {
	
	// --- Capture stderr ---
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w // Redirect os.Stderr to the pipe writer
	log.SetOutput(w)

	cfg, _ := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(awsCredentials.Key, awsCredentials.Secret, "")),
		config.WithRegion("ap-southeast-2"), // TODO should be in config file
	)

	syncManager := s3sync.New(cfg, s3sync.WithDelete())

	// Sync from local to s3
	syncManager.Sync(source, destination)

	// --- Restore stderr and read captured output ---
	w.Close()
	os.Stderr = oldStderr // Restore original stderr
	log.SetOutput(oldStderr) // Restore standard logger output

	var buf bytes.Buffer
	_, readErr := io.Copy(&buf, r)
	if readErr != nil {
		log.Fatalf("Failed to read captured stderr: %v", readErr)
	}

	capturedOutput := buf.String()

	return capturedOutput, nil
}

// SyncAction represents a planned action (upload, download, delete)
type SyncAction struct {
	Action string // e.g., "Upload", "Download", "Delete"
	Path   string // Local path or S3 key
	Reason string // Why the action is planned (e.g., "different size", "missing locally")
}

// Changes to sync? false if no changes, true if there are
func SyncDryRun(source string, destination string, awsCredentials Credentials) (bool, string, error) { 

	// --- Capture stderr ---
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w // Redirect os.Stderr to the pipe writer

	// Important: The standard logger, by default, writes to os.Stderr.
	// You might also need to ensure the standard logger output goes to your pipe.
	// This line is often crucial if you're using `log.Printf` directly.
	log.SetOutput(w)

	// Create AWS config using AWS SDK v2
	cfg, _ := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(awsCredentials.Key, awsCredentials.Secret, "")),
		config.WithRegion("ap-southeast-2"), // TODO should be in config file
	)

	syncManager := s3sync.New(cfg,
		s3sync.WithDryRun(),
		s3sync.WithDelete(),
	)

	syncManager.Sync(source, destination)

	// --- Restore stderr and read captured output ---
	w.Close()
	os.Stderr = oldStderr // Restore original stderr
	log.SetOutput(oldStderr) // Restore standard logger output

	var buf bytes.Buffer
	_, readErr := io.Copy(&buf, r)
	if readErr != nil {
		log.Fatalf("Failed to read captured stderr: %v", readErr)
	}

	capturedOutput := buf.String()

	isChanges := false

	if capturedOutput == "" {
		isChanges = false
	} else {
		isChanges = true
	}

	return isChanges, capturedOutput, nil
}