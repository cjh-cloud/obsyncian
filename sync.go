package main

import (
	"bufio"
	"context"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/seqsense/s3sync/v2"
)

// SyncResult contains the result of a sync operation
type SyncResult struct {
	Output string
	Err    error
}

// executeSyncAsync runs the sync operation asynchronously and streams output line by line
func executeSyncAsync(source string, destination string, awsCredentials Credentials, outputChan chan<- string, doneChan chan<- SyncResult, syncOptions ...s3sync.Option) {
	go func() {
		// --- Capture stderr ---
		oldStderr := os.Stderr
		oldLogOutput := log.Writer()
		r, w, err := os.Pipe()
		if err != nil {
			doneChan <- SyncResult{Err: err}
			return
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
			log.SetOutput(oldLogOutput)
			doneChan <- SyncResult{Err: err}
			return
		}

		// Start a goroutine to read from the pipe and stream to outputChan
		streamDone := make(chan struct{})
		go func() {
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				line := scanner.Text()
				if line != "" {
					outputChan <- line
				}
			}
			close(streamDone)
		}()

		syncManager := s3sync.New(cfg, syncOptions...)
		syncErr := syncManager.Sync(context.TODO(), source, destination)

		// --- Restore stderr ---
		w.Close()
		os.Stderr = oldStderr
		log.SetOutput(oldLogOutput)

		// Wait for streaming to complete
		<-streamDone

		doneChan <- SyncResult{Err: syncErr}
	}()
}

// executeSync handles the common logic for running s3sync with output capturing (synchronous version).
func executeSync(source string, destination string, awsCredentials Credentials, syncOptions ...s3sync.Option) (string, error) {
	outputChan := make(chan string, 100)
	doneChan := make(chan SyncResult, 1)

	executeSyncAsync(source, destination, awsCredentials, outputChan, doneChan, syncOptions...)

	var output string
	for {
		select {
		case line := <-outputChan:
			if output != "" {
				output += "\n"
			}
			output += line
		case result := <-doneChan:
			// Drain any remaining output
			for {
				select {
				case line := <-outputChan:
					if output != "" {
						output += "\n"
					}
					output += line
				default:
					return output, result.Err
				}
			}
		}
	}
}

// SyncAsync performs an asynchronous sync from source to destination, streaming output
func SyncAsync(source string, destination string, awsCredentials Credentials, outputChan chan<- string, doneChan chan<- SyncResult) {
	executeSyncAsync(source, destination, awsCredentials, outputChan, doneChan, s3sync.WithDelete())
}

// SyncDryRunAsync performs an async dry run to see what changes would be made
func SyncDryRunAsync(source string, destination string, awsCredentials Credentials, outputChan chan<- string, doneChan chan<- SyncResult) {
	executeSyncAsync(source, destination, awsCredentials, outputChan, doneChan, s3sync.WithDelete(), s3sync.WithDryRun())
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
