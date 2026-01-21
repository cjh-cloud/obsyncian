package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"time"

	"encoding/json"

	"github.com/google/uuid"

	"github.com/charmbracelet/huh"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	awshttp "github.com/aws/smithy-go/transport/http"

	tea "github.com/charmbracelet/bubbletea"
)

const tableName = "Obsyncian"

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type Credentials struct {
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

type ObsyncianConfig struct {
	ID          string      `json:"id"`
	Local       string      `json:"local"`
	Cloud       string      `json:"cloud"`    // cloud path, bucket?
	Provider    string      `json:"provider"` // only AWS allowed for starters
	Credentials Credentials `json:"credentials"`
}

// Item represents the structure of your DynamoDB table item
type Item struct {
	UserId    string `dynamodbav:"UserId"`
	Timestamp string `dynamodbav:"Timestamp"` // Assuming timestamp is stored as a string (e.g., ISO 8601)
}

// SyncState tracks the current state of sync operations
type SyncState int

const (
	SyncIdle SyncState = iota
	SyncInProgress
)

// Message types for Bubble Tea
type SyncProgressMsg struct {
	Text string
}

type SyncCompleteMsg struct {
	Success bool
	Error   error
}

// SyncedDownMsg is sent when sync down completes to update the tracked timestamp
type SyncedDownMsg struct {
	Timestamp string
}

type FileChangeMsg struct{}

type CloudCheckMsg struct{}

// Configure the local settings if first time starting, and return the loaded config
func configure_local() ObsyncianConfig {
	home_dir, _ := os.UserHomeDir()
	path := fmt.Sprintf("%s/obsyncian/config.json", home_dir)
	_, err := os.Stat(path)
	exists := !errors.Is(err, os.ErrNotExist)

	if !exists {
		id := uuid.New()
		fmt.Printf("Creating new config for user %s", id)

		err := os.Mkdir(fmt.Sprintf("%s/obsyncian", home_dir), 0755)
		_ = err // ignore if already exists

		f, err := os.Create(path)
		check(err)
		defer f.Close()
		_, err = f.WriteString(fmt.Sprintf("{\n  \"id\" : \"%s\"\n}\n", id))
		check(err)
		f.Sync()
	}

	jsonFile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		fmt.Println(err)
	}
	defer jsonFile.Close()

	byteValue, _ := io.ReadAll(jsonFile)

	var obsyncianConfig ObsyncianConfig

	json.Unmarshal(byteValue, &obsyncianConfig)

	if obsyncianConfig.Local == "" {
		huh.NewInput().
			Title("What's your local path?").
			Value(&obsyncianConfig.Local).
			Validate(func(str string) error {
				if str == "Frank" {
					return errors.New("Sorry, we don't serve customers named Frank.")
				}
				return nil
			}).
			Run()
	}

	if obsyncianConfig.Cloud == "" {
		huh.NewInput().
			Title("What's your cloud path (e.g. S3 bucket name)?").
			Value(&obsyncianConfig.Cloud).
			Validate(func(str string) error {
				if str == "Frank" {
					return errors.New("Sorry, we don't serve customers named Frank.")
				}
				return nil
			}).
			Run()
	}

	if obsyncianConfig.Provider == "" {
		huh.NewSelect[string]().
			Title("Choose your burger").
			Options(
				huh.NewOption("Amazon Web Services", "AWS"),
				huh.NewOption("Microsoft Azure", "Azure"),
				huh.NewOption("Google Cloud", "GCP"),
			).
			Value(&obsyncianConfig.Provider).
			Run()
	}

	config_json, err := json.Marshal(obsyncianConfig)
	check(err)
	configFileWriter, err := os.Create(path)
	_, err = configFileWriter.Write(config_json)
	if err != nil {
		fmt.Println("Error writing to file:", err)
	}

	return obsyncianConfig
}

func create_user(userId string, tableName string, svc *dynamodb.Client) {
	userEntry := Item{UserId: userId, Timestamp: time.Now().Format("20060102150405")}
	item, err := attributevalue.MarshalMap(userEntry)
	fmt.Println(item)
	putInput := &dynamodb.PutItemInput{
		TableName: aws.String(tableName), Item: item,
	}
	_, err = svc.PutItem(context.TODO(), putInput)
	if err != nil {
		log.Printf("Couldn't add item to table. Here's why: %v", err)
	}
}

// Scan DynamoDB table for latest timestamp
func get_latest_sync(tableName string, svc *dynamodb.Client) (string, string, error) {
	scanInput := &dynamodb.ScanInput{
		TableName:            aws.String(tableName),
		ProjectionExpression: aws.String("UserId, #ts"),
		ExpressionAttributeNames: map[string]string{
			"#ts": "Timestamp",
		},
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
	}

	var allItems []Item
	var lastEvaluatedKey map[string]types.AttributeValue
	totalConsumedCapacity := 0.0

	for {
		scanInput.ExclusiveStartKey = lastEvaluatedKey
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := svc.Scan(ctx, scanInput)
		if err != nil {
			var reqErr *awshttp.RequestSendError
			if errors.As(err, &reqErr) {
				var netErr net.Error
				if errors.As(reqErr.Unwrap(), &netErr) {
					return "", "", fmt.Errorf("network error: %w", err)
				}
			} else if errors.Is(err, context.DeadlineExceeded) {
				return "", "", fmt.Errorf("context error: %w", err)
			} else {
				log.Fatalf("Failed to scan DynamoDB table: %v", err)
			}
		}
		totalConsumedCapacity += *result.ConsumedCapacity.CapacityUnits

		var pageItems []Item
		err = attributevalue.UnmarshalListOfMaps(result.Items, &pageItems)
		if err != nil {
			log.Fatalf("Failed to unmarshal scanned items: %v", err)
		}
		allItems = append(allItems, pageItems...)

		lastEvaluatedKey = result.LastEvaluatedKey
		if lastEvaluatedKey == nil {
			break
		}
	}

	if len(allItems) == 0 {
		return "", "", nil
	}

	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].Timestamp > allItems[j].Timestamp
	})

	latestItem := allItems[0]

	return latestItem.Timestamp, latestItem.UserId, nil
}

// handleSyncAsync performs sync operations asynchronously and sends progress to the Bubble Tea program
// lastSyncedTs is passed from the model to avoid reading stale state from a copy
func handleSyncAsync(m *mainModel, p *tea.Program, lastSyncedTs string) {
	// Send initial progress
	p.Send(SyncProgressMsg{Text: fmt.Sprintf("🚀 Last local sync: %v", time.Now().Format(time.RFC1123))})

	latest_sync, latest_sync_id, err := get_latest_sync(tableName, m.svc)
	if err != nil {
		p.Send(SyncProgressMsg{Text: fmt.Sprintf("❗ Error getting latest sync time: %v", err)})
		p.Send(SyncCompleteMsg{Success: false, Error: err})
		return
	}
	p.Send(SyncProgressMsg{Text: fmt.Sprintf("Latest cloud sync: %v", latest_sync)})

	if latest_sync == "" {
		p.Send(SyncProgressMsg{Text: "Table is empty. Syncing up..."})
		runAsyncSyncUp(m, p, true)
		return
	}

	// Check our user
	input := &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"UserId": &types.AttributeValueMemberS{Value: m.config.ID},
		},
	}
	result, err := m.svc.GetItem(context.TODO(), input)
	if err != nil {
		p.Send(SyncProgressMsg{Text: fmt.Sprintf("❗ Failed to get item from DynamoDB, %v", err)})
	}

	if result.Item == nil {
		p.Send(SyncProgressMsg{Text: fmt.Sprintf("No item found with UUID: %s in table: %s", m.config.ID, tableName)})
		create_user(m.config.ID, tableName, m.svc)
		p.Send(SyncProgressMsg{Text: "Syncing down..."})
		runAsyncSyncDown(m, p, latest_sync, false)
		return
	}

	var item Item
	err = attributevalue.UnmarshalMap(result.Item, &item)
	if err != nil {
		p.Send(SyncProgressMsg{Text: fmt.Sprintf("Failed to unmarshal DynamoDB item, %v", err)})
	}

	// Check if we need to sync down - use passed lastSyncedTs instead of m.latest_ts_synced
	needsSyncDown := m.config.ID != latest_sync_id && latest_sync >= item.Timestamp && lastSyncedTs < latest_sync
	if needsSyncDown {
		p.Send(SyncProgressMsg{Text: "Not synced with Cloud. Syncing down..."})
		runAsyncSyncDown(m, p, latest_sync, true)
		return
	}

	p.Send(SyncProgressMsg{Text: "Already synced with Cloud"})

	// Check for local changes
	checkLocalChangesAsync(m, p)
}

// runAsyncSyncDown syncs from cloud to local asynchronously
// latestSyncTs is the timestamp of the cloud version we're syncing - stored to prevent re-syncing
func runAsyncSyncDown(m *mainModel, p *tea.Program, latestSyncTs string, checkLocalAfter bool) {
	outputChan := make(chan string, 100)
	doneChan := make(chan SyncResult, 1)

	SyncAsync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials, outputChan, doneChan)

	// Stream output
	go func() {
		for {
			select {
			case line := <-outputChan:
				p.Send(SyncProgressMsg{Text: line})
			case result := <-doneChan:
				// Drain remaining output
				for {
					select {
					case line := <-outputChan:
						p.Send(SyncProgressMsg{Text: line})
					default:
						if result.Err != nil {
							p.Send(SyncProgressMsg{Text: fmt.Sprintf("❗ Sync down error: %v", result.Err)})
							p.Send(SyncCompleteMsg{Success: false, Error: result.Err})
						} else {
							// Send message to update the timestamp (must be done via message for Bubble Tea)
							p.Send(SyncedDownMsg{Timestamp: latestSyncTs})
							p.Send(SyncProgressMsg{Text: "✅ Finished syncing down."})
							if checkLocalAfter {
								checkLocalChangesAsync(m, p)
							} else {
								p.Send(SyncCompleteMsg{Success: true})
							}
						}
						return
					}
				}
			}
		}
	}()
}

// runAsyncSyncUp syncs from local to cloud asynchronously
func runAsyncSyncUp(m *mainModel, p *tea.Program, isNewUser bool) {
	outputChan := make(chan string, 100)
	doneChan := make(chan SyncResult, 1)

	SyncAsync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials, outputChan, doneChan)

	// Stream output
	go func() {
		for {
			select {
			case line := <-outputChan:
				p.Send(SyncProgressMsg{Text: line})
			case result := <-doneChan:
				// Drain remaining output
				for {
					select {
					case line := <-outputChan:
						p.Send(SyncProgressMsg{Text: line})
					default:
						if result.Err != nil {
							p.Send(SyncProgressMsg{Text: fmt.Sprintf("❗ Sync up error: %v", result.Err)})
							p.Send(SyncCompleteMsg{Success: false, Error: result.Err})
						} else {
							p.Send(SyncProgressMsg{Text: "✅ Finished syncing up."})
							if isNewUser {
								create_user(m.config.ID, tableName, m.svc)
							}
							updateTimestamp(m, p)
							p.Send(SyncCompleteMsg{Success: true})
						}
						return
					}
				}
			}
		}
	}()
}

// checkLocalChangesAsync checks for local changes and syncs up if needed
func checkLocalChangesAsync(m *mainModel, p *tea.Program) {
	outputChan := make(chan string, 100)
	doneChan := make(chan SyncResult, 1)

	SyncDryRunAsync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials, outputChan, doneChan)

	go func() {
		var plannedChanges string
		for {
			select {
			case line := <-outputChan:
				if plannedChanges != "" {
					plannedChanges += "\n"
				}
				plannedChanges += line
			case result := <-doneChan:
				// Drain remaining
				for {
					select {
					case line := <-outputChan:
						if plannedChanges != "" {
							plannedChanges += "\n"
						}
						plannedChanges += line
					default:
						if result.Err != nil {
							p.Send(SyncProgressMsg{Text: fmt.Sprintf("❗ Dry run error: %v", result.Err)})
							p.Send(SyncCompleteMsg{Success: false, Error: result.Err})
							return
						}

						if plannedChanges != "" {
							p.Send(SyncProgressMsg{Text: fmt.Sprintf("Local changes detected. Syncing up:\n%v", plannedChanges)})
							runAsyncSyncUp(m, p, false)
						} else {
							p.Send(SyncProgressMsg{Text: "No changes to sync"})
							p.Send(SyncCompleteMsg{Success: true})
						}
						return
					}
				}
			}
		}
	}()
}

// updateTimestamp updates the user's timestamp in DynamoDB
func updateTimestamp(m *mainModel, p *tea.Program) {
	update := expression.Set(expression.Name("Timestamp"), expression.Value(time.Now().Format("20060102150405")))
	expr, err := expression.NewBuilder().WithUpdate(update).Build()
	if err != nil {
		p.Send(SyncProgressMsg{Text: fmt.Sprintf("❗ Couldn't build expression. Here's why: %v", err)})
		return
	}
	putInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"UserId": &types.AttributeValueMemberS{Value: m.config.ID},
		},
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		UpdateExpression:          expr.Update(),
		ReturnValues:              types.ReturnValueUpdatedNew,
	}
	_, err = m.svc.UpdateItem(context.TODO(), putInput)
	if err != nil {
		p.Send(SyncProgressMsg{Text: fmt.Sprintf("❗ Couldn't update item. Here's why: %v", err)})
	}
}

// Legacy synchronous handleSync - kept for compatibility but should migrate to async
func handleSync(m *mainModel) {
	m.appendOutput(fmt.Sprintf("🚀Last local sync: %v", time.Now().Format(time.RFC1123)))

	latest_sync, latest_sync_id, err := get_latest_sync(tableName, m.svc)
	if err != nil {
		m.appendOutput(fmt.Sprintf("❗Error getting latest sync time: %v", err))
		return
	}
	m.appendOutput(fmt.Sprintf("Latest cloud sync: %v", latest_sync))

	if latest_sync == "" {
		m.appendOutput("Table is empty. Syncing up...")
		Sync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)
		m.appendOutput("Finished syncing.")
		create_user(m.config.ID, tableName, m.svc)
		return
	}

	input := &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"UserId": &types.AttributeValueMemberS{Value: m.config.ID},
		},
	}
	result, err := m.svc.GetItem(context.TODO(), input)
	if err != nil {
		m.appendOutput(fmt.Sprintf("❗failed to get item from DynamoDB, %v", err))
	}

	if result.Item == nil {
		m.appendOutput(fmt.Sprintf("No item found with UUID: %s in table: %s", m.config.ID, tableName))
		create_user(m.config.ID, tableName, m.svc)
		m.appendOutput("Syncing down...")
		Sync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials)
		m.appendOutput("Finished syncing.")
		return
	}

	var item Item
	err = attributevalue.UnmarshalMap(result.Item, &item)
	if err != nil {
		m.appendOutput(fmt.Sprintf("failed to unmarshal DynamoDB item, %v", err))
	}

	if m.config.ID != latest_sync_id && latest_sync >= item.Timestamp && m.latest_ts_synced < latest_sync {
		m.appendOutput("Not synced with Cloud. Syncing down...")
		changes, _ := Sync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials)
		m.appendOutput(changes)
		m.latest_ts_synced = latest_sync
	} else {
		m.appendOutput("Already synced with Cloud")
	}

	isChanges, plannedChanges, _ := SyncDryRun(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)
	if isChanges {
		m.appendOutput(fmt.Sprintf("Local changes detected. Syncing up:\n %v", plannedChanges))
		Sync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)

		update := expression.Set(expression.Name("Timestamp"), expression.Value(time.Now().Format("20060102150405")))
		expr, err := expression.NewBuilder().WithUpdate(update).Build()
		if err != nil {
			m.appendOutput(fmt.Sprintf("❗Couldn't build expression. Here's why: %v", err))
		}
		putInput := &dynamodb.UpdateItemInput{
			TableName: aws.String(tableName),
			Key: map[string]types.AttributeValue{
				"UserId": &types.AttributeValueMemberS{Value: m.config.ID},
			},
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			UpdateExpression:          expr.Update(),
			ReturnValues:              types.ReturnValueUpdatedNew,
		}
		_, err = m.svc.UpdateItem(context.TODO(), putInput)
		if err != nil {
			m.appendOutput(fmt.Sprintf("❗Couldn't update item. Here's why: %v", err))
		}
	} else {
		m.appendOutput("No changes to sync")
	}
}
