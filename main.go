// Obsyncian
package main

import (
	"errors"
	"fmt"
    "log"
	"os"
    "encoding/json"
    "io"
    "context"
    "time"
    "sort"

    "github.com/google/uuid"
    "github.com/charmbracelet/huh"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"

)

// cmd to configure - AWS creds? s3 bucket to use, dir to use (optional)
// generate uuid for this user and save to config on first startup

// on startup, if configured, check dynamo, if another user last synced, sync from bucket?

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
    fmt.Printf("Path: '%s' \n", path)
    _, err := os.Stat(path)
	exists := !errors.Is(err, os.ErrNotExist)

    fmt.Printf("File exists: '%s' \n", exists)

    if (!exists){
        id := uuid.New()
        fmt.Printf("Creating new config for user %s", id)

        // create dir and file
        err := os.Mkdir(fmt.Sprintf("%s/obsyncian", home_dir), 0755)
        // check(err)

        f, err := os.Create(path)
        check(err)
        defer f.Close() // It’s idiomatic to defer a Close immediately after opening a file.
        n3, err := f.WriteString(fmt.Sprintf("{\n  \"id\" : \"%s\"\n}\n", id))
        check(err)
        fmt.Printf("wrote %d bytes\n", n3)
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
    fmt.Println("Successfully Opened %s", path)
    // defer the closing of our jsonFile so that we can parse it later on
    defer jsonFile.Close()

    // read our opened jsonFile as a byte array.
    byteValue, _ := io.ReadAll(jsonFile)

    // we initialize our Users array
    var obsyncianConfig ObsyncianConfig

    // we unmarshal our byteArray which contains our
    // jsonFile's content into 'users' which we defined above
    json.Unmarshal(byteValue, &obsyncianConfig)

    fmt.Println("Config ID: " + obsyncianConfig.ID)

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
    fmt.Println("Config file updated.")

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

func main() {
    
    obsyncianConfig := configure_local()

    // TODO - we have our config file, we can now start reading from Dynamo, and syncing with S3

    // If cloud has new changes, pull them
    // If I have new changes, and am up to date with cloud, push them

    //* read from Dynamo table

	// 1. Initialize AWS Session (v2 style)
	// Replace "your-aws-region" with your actual AWS region (e.g., "us-east-1")
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("ap-southeast-2"), // TODO
	)
	if err != nil {
		log.Fatalf("failed to load AWS config, %v", err)
	}

	// Create a DynamoDB service client
	svc := dynamodb.NewFromConfig(cfg)

    tableName := "Obsyncian"

    // * Check if the table is empty
    // Perform a Scan with Limit 1 and Select "COUNT"
	// This will tell us if there's at least one item.
	scanInput := &dynamodb.ScanInput{
		TableName:      aws.String(tableName),
		Limit:          aws.Int32(1), // Limit to checking only 1 item
		Select:         types.SelectCount, // We only care about the count, not the data
		ConsistentRead: aws.Bool(true), // Optional: For strong consistency (more RCUs)
	}

	scanResult, err := svc.Scan(context.TODO(), scanInput)
	if err != nil {
		log.Fatalf("failed to scan table: %v", err)
	}

    fmt.Printf("Table scan result '%d'.\n", scanResult.Count)

    // TODO : if 0, sync up, and insert into table (check if s3 bucket is empty?)
    if scanResult.Count == 0 {
        fmt.Printf("Syncing...")
        Sync(obsyncianConfig.Local, fmt.Sprintf("s3://%s", obsyncianConfig.Cloud))
        fmt.Printf("Finished syncing.\n")

        // userEntry := Item{UserId: obsyncianConfig.ID, Timestamp: time.Now().Format("20060102150405")}
        // item, err := attributevalue.MarshalMap(userEntry)
        // fmt.Println(item)
        // putInput := &dynamodb.PutItemInput{
        //     TableName: aws.String(tableName), Item: item,
        // }
        // _, err = svc.PutItem(context.TODO(), putInput)
        // if err != nil {
        //     log.Printf("Couldn't add item to table. Here's why: %v\n", err)
        // }
        create_user(obsyncianConfig.ID, tableName, svc)
        // return err
    }

	// If Count is 0, the table is empty (at least within the 1MB scanned portion).
	// If Count is 1, it has at least one item.
	// if *result.Count == 0 {
	// 	fmt.Printf("Table '%s' is empty.\n", tableName)
	// } else {
	// 	fmt.Printf("Table '%s' contains items. Count: %d\n", tableName, *result.Count)
	// }


    // * Check if our UUID is in the table
    input := &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"UserId": &types.AttributeValueMemberS{Value: obsyncianConfig.ID},
		},
	}

    // 4. Execute the GetItem operation
	// Using context.TODO() for simplicity, in a real application, you might pass a request-scoped context.
	result, err := svc.GetItem(context.TODO(), input)
	if err != nil {
		log.Fatalf("failed to get item from DynamoDB, %v", err)
	}

	// 5. Unmarshal the result into your Go struct
	if result.Item == nil {
		fmt.Printf("No item found with UUID: %s in table: %s\n", obsyncianConfig.ID, tableName)
		
        // TODO : create the item and sync down? 
        create_user(obsyncianConfig.ID, tableName, svc)
        fmt.Printf("Syncing...")
        Sync(fmt.Sprintf("s3://%s", obsyncianConfig.Cloud), obsyncianConfig.Local)
        fmt.Printf("Finished syncing.\n")

	}

	var item Item
	err = attributevalue.UnmarshalMap(result.Item, &item)
	if err != nil {
		log.Fatalf("failed to unmarshal DynamoDB item, %v", err)
	}

	fmt.Printf("Found item:\n")
	fmt.Printf("  UUID: %s\n", item.UserId)
	fmt.Printf("  Timestamp: %s\n", item.Timestamp)

    // * scan table for latest timestamp, and get the UUID, if not ours, sync
    scanInput = &dynamodb.ScanInput{
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

	fmt.Printf("Scanning table '%s' to find the globally latest item (WARNING: Can be costly!)...\n", tableName)

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

	fmt.Printf("Total Consumed Capacity Units: %.2f\n", totalConsumedCapacity)

	if len(allItems) == 0 {
		fmt.Printf("Table '%s' is empty.\n", tableName)
		return
	}

	// --- Find the item with the latest timestamp in memory ---
	// Assuming timestamp is ISO 8601 string, sort them directly.
	// If it's a Unix epoch number, you'd sort int64.
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].Timestamp > allItems[j].Timestamp // Descending sort
	})

	latestItem := allItems[0] // The first item after sorting is the latest

	fmt.Printf("\nGlobally Latest Item Found:\n")
	fmt.Printf("  Partition Key: %s\n", latestItem.UserId)
	fmt.Printf("  Timestamp: %s\n", latestItem.Timestamp)
	// fmt.Printf("  Data: %s\n", latestItem.Data)

	// scanResult, err := svc.Scan(context.TODO(), scanInput)
	// if err != nil {
	// 	log.Fatalf("failed to scan table: %v", err)
	// }

    // if Dynamo table empty -> create first entry, and sync from local to cloud
    // if Dynamo has entry
    //    if last uuid is us, sync up, and update time changed
    //    if last uuid is not us, sync down dry run, if changes sync down and store latest time in memory
    // dry run sync up, if changes, and latest sync in dynamo is not newer than what is stored in memeory, sync up

}