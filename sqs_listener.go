package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	tea "github.com/charmbracelet/bubbletea"
)

// Debounce time for SQS notifications to avoid triggering multiple syncs
const sqsDebounceTime = 2 * time.Second

// SQSListener listens for S3 change notifications via SQS
type SQSListener struct {
	sqsClient       *sqs.Client
	snsClient       *sns.Client
	queueURL        string
	queueARN        string
	subscriptionARN string
	snsTopicARN     string
	deviceID        string
	program         *tea.Program
	stopChan        chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	mu              sync.Mutex
	running         bool
	debounceTimer   *time.Timer
	pendingNotify   bool
}

// SNS message wrapper structure
type SNSMessage struct {
	Type      string `json:"Type"`
	MessageId string `json:"MessageId"`
	Message   string `json:"Message"`
}

// S3 EventBridge event structure (nested in SNS message)
type S3EventBridgeEvent struct {
	Version    string    `json:"version"`
	ID         string    `json:"id"`
	DetailType string    `json:"detail-type"`
	Source     string    `json:"source"`
	Account    string    `json:"account"`
	Time       time.Time `json:"time"`
	Region     string    `json:"region"`
	Detail     struct {
		Bucket struct {
			Name string `json:"name"`
		} `json:"bucket"`
		Object struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
		} `json:"object"`
	} `json:"detail"`
}

// NewSQSListener creates a new SQS listener for sync notifications
func NewSQSListener(awsCredentials Credentials, snsTopicARN string, deviceID string, program *tea.Program) (*SQSListener, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(awsCredentials.Key, awsCredentials.Secret, "")),
		config.WithRegion("ap-southeast-2"), // TODO: should be in config file
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	sqsClient := sqs.NewFromConfig(cfg)
	snsClient := sns.NewFromConfig(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	listener := &SQSListener{
		sqsClient:   sqsClient,
		snsClient:   snsClient,
		snsTopicARN: snsTopicARN,
		deviceID:    deviceID,
		program:     program,
		stopChan:    make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Create the SQS queue
	if err := listener.createQueue(); err != nil {
		return nil, fmt.Errorf("failed to create SQS queue: %w", err)
	}

	// Subscribe the queue to the SNS topic
	if err := listener.subscribeToSNS(); err != nil {
		// Clean up queue if subscription fails
		listener.deleteQueue()
		return nil, fmt.Errorf("failed to subscribe to SNS: %w", err)
	}

	return listener, nil
}

// createQueue creates a temporary SQS queue for this device
func (l *SQSListener) createQueue() error {
	queueName := fmt.Sprintf("obsyncian-%s", l.deviceID)

	// Create queue with long polling enabled
	createResult, err := l.sqsClient.CreateQueue(context.TODO(), &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
		Attributes: map[string]string{
			"ReceiveMessageWaitTimeSeconds": "20", // Long polling
			"MessageRetentionPeriod":        "300", // 5 minutes retention
			"VisibilityTimeout":             "30",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	l.queueURL = *createResult.QueueUrl

	// Get queue ARN for SNS subscription
	attrResult, err := l.sqsClient.GetQueueAttributes(context.TODO(), &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(l.queueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return fmt.Errorf("failed to get queue ARN: %w", err)
	}

	l.queueARN = attrResult.Attributes[string(types.QueueAttributeNameQueueArn)]

	// Set queue policy to allow SNS to send messages
	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [{
			"Sid": "AllowSNS",
			"Effect": "Allow",
			"Principal": {"Service": "sns.amazonaws.com"},
			"Action": "sqs:SendMessage",
			"Resource": "%s",
			"Condition": {
				"ArnEquals": {
					"aws:SourceArn": "%s"
				}
			}
		}]
	}`, l.queueARN, l.snsTopicARN)

	_, err = l.sqsClient.SetQueueAttributes(context.TODO(), &sqs.SetQueueAttributesInput{
		QueueUrl: aws.String(l.queueURL),
		Attributes: map[string]string{
			"Policy": policy,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to set queue policy: %w", err)
	}

	return nil
}

// subscribeToSNS subscribes the SQS queue to the SNS topic
func (l *SQSListener) subscribeToSNS() error {
	result, err := l.snsClient.Subscribe(context.TODO(), &sns.SubscribeInput{
		TopicArn: aws.String(l.snsTopicARN),
		Protocol: aws.String("sqs"),
		Endpoint: aws.String(l.queueARN),
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to SNS: %w", err)
	}

	l.subscriptionARN = *result.SubscriptionArn
	return nil
}

// Start begins listening for messages
func (l *SQSListener) Start() {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return
	}
	l.running = true
	l.mu.Unlock()

	l.wg.Add(1)
	go l.pollMessages()
}

// pollMessages continuously polls the SQS queue for messages
func (l *SQSListener) pollMessages() {
	defer l.wg.Done()

	for {
		select {
		case <-l.stopChan:
			return
		default:
			l.receiveAndProcessMessages()
		}
	}
}

// receiveAndProcessMessages receives messages from SQS and processes them
func (l *SQSListener) receiveAndProcessMessages() {
	// Use the listener's cancellable context with a timeout for the request
	ctx, cancel := context.WithTimeout(l.ctx, 25*time.Second)
	defer cancel()

	result, err := l.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(l.queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     20, // Long polling
	})
	if err != nil {
		// Check if we were cancelled (shutting down) - don't log this as an error
		if l.ctx.Err() != nil {
			return
		}
		// Log error but continue polling (could be transient)
		log.Printf("SQS receive error: %v", err)
		return
	}

	shouldNotify := false
	for _, msg := range result.Messages {
		if l.shouldTriggerSync(msg) {
			shouldNotify = true
		}

		// Delete the message after processing
		l.deleteMessage(msg.ReceiptHandle)
	}

	// Debounce notifications to avoid triggering multiple syncs
	if shouldNotify {
		l.triggerDebouncedNotification()
	}
}

// triggerDebouncedNotification triggers a notification after the debounce period
func (l *SQSListener) triggerDebouncedNotification() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Cancel any existing timer
	if l.debounceTimer != nil {
		l.debounceTimer.Stop()
	}

	// Set a new timer
	l.debounceTimer = time.AfterFunc(sqsDebounceTime, func() {
		if l.program != nil {
			l.program.Send(CloudCheckMsg{})
		}
	})
}

// shouldTriggerSync determines if a message should trigger a sync
// This filters out self-triggered events
func (l *SQSListener) shouldTriggerSync(msg types.Message) bool {
	if msg.Body == nil {
		return false
	}

	// Parse the SNS wrapper message
	var snsMsg SNSMessage
	if err := json.Unmarshal([]byte(*msg.Body), &snsMsg); err != nil {
		log.Printf("Failed to parse SNS message: %v", err)
		return true // Trigger sync on parse error to be safe
	}

	// Parse the actual S3 EventBridge event
	var event S3EventBridgeEvent
	if err := json.Unmarshal([]byte(snsMsg.Message), &event); err != nil {
		log.Printf("Failed to parse S3 event: %v", err)
		return true // Trigger sync on parse error to be safe
	}

	// For now, trigger sync for all S3 events
	// The self-filtering will be handled by checking DynamoDB in handleSyncAsync
	// which already checks if the latest sync was from this device
	return true
}

// deleteMessage deletes a message from the queue
func (l *SQSListener) deleteMessage(receiptHandle *string) {
	if receiptHandle == nil {
		return
	}

	_, err := l.sqsClient.DeleteMessage(context.TODO(), &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(l.queueURL),
		ReceiptHandle: receiptHandle,
	})
	if err != nil {
		log.Printf("Failed to delete SQS message: %v", err)
	}
}

// Stop stops the listener and cleans up resources
func (l *SQSListener) Stop() {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return
	}
	l.running = false

	// Cancel any pending debounce timer
	if l.debounceTimer != nil {
		l.debounceTimer.Stop()
	}
	l.mu.Unlock()

	// Cancel the context to immediately abort any in-flight SQS requests
	l.cancel()

	// Signal the polling goroutine to stop
	close(l.stopChan)

	// Wait for the polling goroutine to finish (should be immediate now)
	l.wg.Wait()

	// Unsubscribe from SNS
	l.unsubscribeFromSNS()

	// Delete the queue
	l.deleteQueue()
}

// unsubscribeFromSNS unsubscribes the queue from the SNS topic
func (l *SQSListener) unsubscribeFromSNS() {
	if l.subscriptionARN == "" || l.subscriptionARN == "pending confirmation" {
		return
	}

	_, err := l.snsClient.Unsubscribe(context.TODO(), &sns.UnsubscribeInput{
		SubscriptionArn: aws.String(l.subscriptionARN),
	})
	if err != nil {
		log.Printf("Failed to unsubscribe from SNS: %v", err)
	}
}

// deleteQueue deletes the SQS queue
func (l *SQSListener) deleteQueue() {
	if l.queueURL == "" {
		return
	}

	_, err := l.sqsClient.DeleteQueue(context.TODO(), &sqs.DeleteQueueInput{
		QueueUrl: aws.String(l.queueURL),
	})
	if err != nil {
		log.Printf("Failed to delete SQS queue: %v", err)
	}
}
