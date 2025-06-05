package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/seqsense/s3sync"
)

func Sync(source string, destination string) (error) {
	fmt.Println("Syncing in module...")
	fmt.Println("source: %s", source)
	fmt.Println("destination: %s", destination)
	// Creates an AWS session
	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String("ap-southeast-2"),
	})

	syncManager := s3sync.New(sess)

	// Sync from local to s3
	syncManager.Sync(source, destination)

	fmt.Println("Synced in module.")

	return nil
}