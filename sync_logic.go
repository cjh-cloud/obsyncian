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

	"github.com/google/uuid"

	"github.com/charmbracelet/huh"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
            Value(&obsyncianConfig.Provider). // store the chosen option in the "burger" variable
            Run()
    }

    config_json, err := json.Marshal(obsyncianConfig)
    check(err)
    configFileWriter, err := os.Create(path) // os.Open(path)
    _, err = configFileWriter.Write(config_json)
    // check(err)
    if err != nil {
        fmt.Println("Error writing to file:", err)
    }

    // err = form.Run()
    // if err != nil {
	// 	fmt.Println("Uh oh:", err)
	// 	os.Exit(1)
	// }

    // if !discount {
    //     fmt.Println("What? You didn’t take the discount?!")
    // }

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
        log.Printf("Couldn't add item to table. Here's why: %v\n", err)
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

    // --- Execute Scan and Find Latest ---
	var allItems []Item
	var lastEvaluatedKey map[string]types.AttributeValue // For pagination
	totalConsumedCapacity := 0.0

	// fmt.Printf("Scanning table '%s' to find the globally latest item (WARNING: Can be costly!)...\n", tableName)

	// Loop to handle pagination for Scan
	for {
		scanInput.ExclusiveStartKey = lastEvaluatedKey // Set the start key for the next page
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Add a timeout for each scan page
		defer cancel()

		result, err := svc.Scan(ctx, scanInput)
		if err != nil {
			log.Fatalf("Failed to scan DynamoDB table: %v", err)
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
			// No more items to scan
			break
		}
	}

	// fmt.Printf("Total Consumed Capacity Units: %.2f\n", totalConsumedCapacity)

	if len(allItems) == 0 {
		// fmt.Printf("Table '%s' is empty.\n", tableName)
		return "", "", nil
	}

	// --- Find the item with the latest timestamp in memory ---
	// Assuming timestamp is ISO 8601 string, sort them directly.
	// If it's a Unix epoch number, you'd sort int64.
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].Timestamp > allItems[j].Timestamp // Descending sort
	})

	latestItem := allItems[0] // The first item after sorting is the latest

	// fmt.Printf("\nGlobally Latest Item Found:\n")
	// fmt.Printf("  Partition Key: %s\n", latestItem.UserId)
	// fmt.Printf("  Timestamp: %s\n", latestItem.Timestamp)

    return latestItem.Timestamp, latestItem.UserId, nil
}

// func handleFirstSync(m *mainModel) string {
	
// }

func handleSync(m *mainModel) string {
	var tickerViewContent string

	// Update textView with sample scrollable content
	sampleContent := `This is a scrollable text view.

You can navigate using:
- Arrow keys (up/down) or k/j to scroll line by line
- Page Up/Page Down to scroll by pages

Here's some sample content to demonstrate scrolling:

Line 1: Configuration Details
Line 2: Local Path: ` + m.config.Local + `
Line 3: Cloud Path: ` + m.config.Cloud + `
Line 4: Provider: ` + m.config.Provider + `
Line 5: User ID: ` + m.config.ID + `

Line 6: Sync Status Information
Line 7: Last sync timestamp will appear here
Line 8: 
Line 9: Cost Explorer Data
Line 10: This will show AWS cost data
Line 11: 
Line 12: Recent Sync Activities
Line 13: Upload activities
Line 14: Download activities
Line 15: 
Line 16: System Information
Line 17: Memory usage
Line 18: Network status
Line 19: 
Line 20: Additional Information
Line 21: Debug information
Line 22: Error logs
Line 23: Performance metrics
Line 24: 
Line 25: End of content - scroll up to see more`

	m.updateTextViewContent(sampleContent)

	tickerViewContent = fmt.Sprintf("Last updated: %v", time.Now().Format(time.RFC1123))

	latest_sync, latest_sync_id, err := get_latest_sync(tableName, m.svc)
	if err != nil {
		log.Fatalf("failed to get latest sync from DynamoDB, %v", err)
	}
	tickerViewContent += fmt.Sprintf("\n LATEST CLOUD SYNC: %v \n", latest_sync)

	if latest_sync == "" {
		tickerViewContent += "Table is empty. Syncing up...\n"
		Sync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)
		tickerViewContent += "Finished syncing.\n"
		create_user(m.config.ID, tableName, m.svc)
		return tickerViewContent // TODO should we be returning here?
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
		log.Fatalf("failed to get item from DynamoDB, %v", err)
	}

	if result.Item == nil {
		tickerViewContent += fmt.Sprintf("No item found with UUID: %s in table: %s\n", m.config.ID, tableName)
		create_user(m.config.ID, tableName, m.svc)
		tickerViewContent += "Syncing down...\n"
		Sync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials)
		tickerViewContent += "Finished syncing.\n"
		return tickerViewContent
	}

	var item Item
	err = attributevalue.UnmarshalMap(result.Item, &item)
	if err != nil {
		log.Fatalf("failed to unmarshal DynamoDB item, %v", err)
	}

	// tickerViewContent += fmt.Sprintf("Our ID: %v latest ID: %v latest t: %v latest t synced: %v \n", m.config.ID, item.UserId, latest_sync, m.latest_ts_synced)

	if m.config.ID != latest_sync_id && latest_sync >= item.Timestamp && m.latest_ts_synced < latest_sync {
		tickerViewContent += "Not synced with Cloud. Syncing down...\n"
		changes, _ := Sync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials) // TODO check this works
		tickerViewContent += changes
		m.latest_ts_synced = latest_sync
	} else {
		tickerViewContent += "Already synced with Cloud\n"
	}

	isChanges, plannedChanges, _ := SyncDryRun(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)
	if isChanges {
		tickerViewContent += fmt.Sprintf("Local changes detected. Syncing up:\n %v \n", plannedChanges)
		Sync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)

		// Update timestamp in table
		update := expression.Set(expression.Name("Timestamp"), expression.Value(time.Now().Format("20060102150405")))
		expr, err := expression.NewBuilder().WithUpdate(update).Build()
		if err != nil {
			log.Printf("Couldn't build expression. Here's why: %v\n", err)
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
			log.Printf("Couldn't update item. Here's why: %v\n", err)
		}
	} else {
		tickerViewContent += "No changes to sync\n"
	}

	return tickerViewContent
}
