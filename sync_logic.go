package main

import (
	"context"
	"fmt"
	"log"
	"time"
	"os"
	"errors"
	"io"
	"sort"
	"encoding/json"
	"net"

	"github.com/google/uuid"

	"github.com/charmbracelet/huh"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"strings"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	awshttp "github.com/aws/smithy-go/transport/http"
)

const tableName = "Obsyncian"

func check(e error) {
    if e != nil {
        panic(e)
    }
}

type Credentials struct {
    Key string `json: "key"`
    Secret string `json: "secret"`
}

type ObsyncianConfig struct {
    ID   string `json:"id"`
    Local   string `json:"local"`
    Cloud    string    `json:"cloud"` // cloud path, bucket?
    Provider string `json:"provider"` // only AWS allowed for starters
    Credentials Credentials `json:"credentials"`
}

// Item represents the structure of your DynamoDB table item
type Item struct {
    UserId      string `dynamodbav:"UserId"`
    Timestamp string `dynamodbav:"Timestamp"` // Assuming timestamp is stored as a string (e.g., ISO 8601)
}

// Configure the local settings if first time starting, and return the loaded config
func configure_local() ObsyncianConfig {
    // check config file exists in the home dir, and UUID exists
    // path := os.UserHomeDir() + "\\obsyncian\\config"
    home_dir, _ := os.UserHomeDir()
    path := fmt.Sprintf("%s/obsyncian/config.json", home_dir) // TODO : are we on windows? change slashes
    _, err := os.Stat(path)
	exists := !errors.Is(err, os.ErrNotExist)

    if (!exists){
        id := uuid.New()
        fmt.Printf("Creating new config for user %s", id)

        // create dir and file
        err := os.Mkdir(fmt.Sprintf("%s/obsyncian", home_dir), 0755)
        // check(err)

        f, err := os.Create(path)
        check(err)
        defer f.Close() // It’s idiomatic to defer a Close immediately after opening a file.
        _, err = f.WriteString(fmt.Sprintf("{\n  \"id\" : \"%s\"\n}\n", id))
        check(err)
        f.Sync() // Issue a Sync to flush writes to stable storage.
    }

    // if it does exist, does the config have what we need? id, aws creds, s3 bucket, optional subdir in bucket, local dir to sync ?
    // read from file
    // Open our jsonFile
    jsonFile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644) // os.Open(path)
    // if we os.Open returns an error then handle it
    if err != nil {
        fmt.Println(err)
    }
    // defer the closing of our jsonFile so that we can parse it later on
    defer jsonFile.Close()

    // read our opened jsonFile as a byte array.
    byteValue, _ := io.ReadAll(jsonFile)

    // we initialize our Users array
    var obsyncianConfig ObsyncianConfig

    // we unmarshal our byteArray which contains our
    // jsonFile's content into 'users' which we defined above
    json.Unmarshal(byteValue, &obsyncianConfig)

    // fmt.Println("Config ID: " + obsyncianConfig.ID)

    // TODO If values are missing, ask for user input

    if obsyncianConfig.Local == "" {
        huh.NewInput().
            Title("What’s your local path?").
            Value(&obsyncianConfig.Local).
            // Validating fields is easy. The form will mark erroneous fields
            // and display error messages accordingly.
            Validate(func(str string) error {
                if str == "Frank" {
                    return errors.New("Sorry, we don’t serve customers named Frank.")
                }
                return nil
            }).
            Run()
    }

    // Cloud
    if obsyncianConfig.Cloud == "" {
        huh.NewInput().
            Title("What's your cloud path (e.g. S3 bucket name)?").
            Value(&obsyncianConfig.Cloud).
            // Validating fields is easy. The form will mark erroneous fields
            // and display error messages accordingly.
            Validate(func(str string) error {
                if str == "Frank" {
                    return errors.New("Sorry, we don’t serve customers named Frank.")
                }
                return nil
            }).
            Run()
    }

    // Provider
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
    // * scan table for latest timestamp, and get the UUID, if not ours, sync
    scanInput := &dynamodb.ScanInput{
		TableName:      aws.String(tableName),
		// You can optionally project only the Partition Key and Timestamp attributes
		// if you don't need the full item for the comparison, saving some data transfer.
		ProjectionExpression: aws.String("UserId, #ts"), // Replace 'my_partition_key'
		ExpressionAttributeNames: map[string]string{
			"#ts": "Timestamp", // Use an expression attribute name for 'timestamp' as it's a reserved word
		},
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal, // For logging RCU usage // TODO : find out more
	}

	var allItems []Item
	var lastEvaluatedKey map[string]types.AttributeValue // For pagination
	totalConsumedCapacity := 0.0

	// Loop to handle pagination for Scan
	for {
		scanInput.ExclusiveStartKey = lastEvaluatedKey // Set the start key for the next page
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Add a timeout for each scan page
		defer cancel()

		// Handle network errors, so that we can retry once connection is back
		result, err := svc.Scan(ctx, scanInput)
		if err != nil {
			var reqErr *awshttp.RequestSendError
			if errors.As(err, &reqErr) {
				// Check for underlying network error
				var netErr net.Error
				if errors.As(reqErr.Unwrap(), &netErr) {
					// Retry or inform user
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
			break // No more items to scan
		}
	}

	// fmt.Printf("Total Consumed Capacity Units: %.2f", totalConsumedCapacity)

	if len(allItems) == 0 {
		return "", "", nil
	}

	// --- Find the item with the latest timestamp in memory ---
	// Assuming timestamp is ISO 8601 string, sort them directly.
	// If it's a Unix epoch number, you'd sort int64.
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].Timestamp > allItems[j].Timestamp // Descending sort
	})

	latestItem := allItems[0] // The first item after sorting is the latest

    return latestItem.Timestamp, latestItem.UserId, nil
}

func handleSync(m *mainModel) {
	// var tickerViewContent string

	// Get the last 12 months of costs
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Printf("Error loading AWS config: %v", err)
		return
	}
	
	costExplorerClient := costexplorer.NewFromConfig(cfg)
	
	end := time.Now()
	start := end.AddDate(0, -12, 0)
	
	ceInput := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(start.Format("2006-01-02")),
			End:   aws.String(end.Format("2006-01-02")),
		},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{"UnblendedCost"},
		Filter: &cetypes.Expression{
			Tags: &cetypes.TagValues{
				Key:    aws.String("Project"),
				Values: []string{"Obsyncian"},
			},
		},
	}
	
	ceResult, err := costExplorerClient.GetCostAndUsage(context.TODO(), ceInput)
	if err != nil {
		log.Printf("Error getting cost data: %v", err)
		return
	}

	var costs strings.Builder
	costs.WriteString("AWS Costs for Obsyncian Project:\n\n")
	
	for _, data := range ceResult.ResultsByTime {
		cost := *data.Total["UnblendedCost"].Amount
		period := *data.TimePeriod.Start
		costs.WriteString(fmt.Sprintf("%s: $%s\n", period, cost))
	}

	// Update textView with cost data
	sampleContent := costs.String()

	// // Previous content was: This is a scrollable text view.
	// // You can navigate using:
	// - Arrow keys (up/down) or k/j to scroll line by line
	// - Page Up/Page Down to scroll by pages

	// Here's some sample content to demonstrate scrolling:

	// Line 1: Configuration Details
	// Line 2: Local Path: ` + m.config.Local + `
	// Line 3: Cloud Path: ` + m.config.Cloud + `
	// Line 4: Provider: ` + m.config.Provider + `
	// Line 5: User ID: ` + m.config.ID + `

	// Line 6: Sync Status Information`

	m.updateTextViewContent(sampleContent)

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

	// Check our user
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
		changes, _ := Sync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials) // TODO check this works
		m.appendOutput(changes)
		m.latest_ts_synced = latest_sync
	} else {
		m.appendOutput("Already synced with Cloud")
	}

	isChanges, plannedChanges, _ := SyncDryRun(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)
	if isChanges {
		m.appendOutput(fmt.Sprintf("Local changes detected. Syncing up:\n %v", plannedChanges))
		Sync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)

		// Update timestamp in table
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
