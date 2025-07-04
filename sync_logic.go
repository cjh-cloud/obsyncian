package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const tableName = "Obsyncian"

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
