package main

import (
	// "fmt"
	// "sync" // For thread-safe collection of actions
	"os"
	"bytes"
	"io"
	"log"
	// "regexp" // For parsing output
	// "strings"


	"github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/aws/credentials"
    "github.com/seqsense/s3sync"
)

func Sync(source string, destination string, awsCredentials Credentials) (string, error) {
	
	// --- Capture stderr ---
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w // Redirect os.Stderr to the pipe writer
	log.SetOutput(w)

	// Creates an AWS session
	sess, _ := session.NewSession(&aws.Config{
		// TODO
		Credentials: credentials.NewStaticCredentials(awsCredentials.Key, awsCredentials.Secret, ""),
		Region: aws.String("ap-southeast-2"), // TODO
	})

	syncManager := s3sync.New(sess, s3sync.WithDelete())

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

	// Creates an AWS session
	sess, _ := session.NewSession(&aws.Config{
		// TODO
		Credentials: credentials.NewStaticCredentials(awsCredentials.Key, awsCredentials.Secret, ""),
		Region: aws.String("ap-southeast-2"), // TODO
	})

	syncManager := s3sync.New(sess,
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