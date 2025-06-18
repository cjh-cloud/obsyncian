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

func Sync(source string, destination string, awsCredentials Credentials) (error) {
	// fmt.Println("Syncing in module...")
	// fmt.Println("source: %s", source)
	// fmt.Println("destination: %s", destination)
	// Creates an AWS session
	sess, _ := session.NewSession(&aws.Config{
		// TODO
		Credentials: credentials.NewStaticCredentials(awsCredentials.Key, awsCredentials.Secret, ""),
		Region: aws.String("ap-southeast-2"), // TODO
	})

	syncManager := s3sync.New(sess, s3sync.WithDelete())

	// Sync from local to s3
	syncManager.Sync(source, destination)

	// fmt.Println("Synced in module.") // TODO, return this

	return nil
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

	// fmt.Println("SYNC DRY RUN!!!")
	// testing := syncManager.Sync(source, destination)
	// fmt.Println(testing)
	// fmt.Println("SYNC DRY RUN!!!")
	
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
	// fmt.Println("\n--- RAW DRY RUN OUTPUT (Captured from Stderr) ---")
	// fmt.Println(capturedOutput) // TODO, return this
	// fmt.Println("\n--- RAW DRY RUN OUTPUT (Captured from Stderr) ---")

	// --- Parse the captured output ---
	// var plannedActions []SyncAction
	// Adjust regex to account for potential log prefixes from the standard logger
	// Example: "2025/06/05 23:17:14 upload: path/to/file"
	// re := regexp.MustCompile(`^(?:.*\s)?(upload|download|delete): (.+)$`) // Non-capturing group for log prefix

	// lines := strings.Split(capturedOutput, "\n")
	// for _, line := range lines {
	// 	matches := re.FindStringSubmatch(line)
	// 	if len(matches) == 3 {
	// 		actionType := strings.Title(matches[1])
	// 		filePath := matches[2]
	// 		plannedActions = append(plannedActions, SyncAction{
	// 			Action: actionType,
	// 			Path:   filePath,
	// 		})
	// 	}
	// }

	isChanges := false

	// fmt.Println("\n--- PARSED DRY RUN RESULTS ---")
	// if len(plannedActions) == 0 {
	if capturedOutput == "" {
		// fmt.Println("No changes to sync. Local and S3 are in sync.")
		isChanges = false
	} else {
		isChanges = true
		// fmt.Printf("Total planned actions: %d\n", len(plannedActions))
		// for _, action := range plannedActions {
		// 	fmt.Printf("- Action: %-8s | Path: %s\n", action.Action, action.Path)
		// }
	}

	// fmt.Println("\nDry run complete. No files were actually synced or deleted.")

	return isChanges, capturedOutput, nil
}