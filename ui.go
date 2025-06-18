// poc ui
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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/timer"
	"github.com/charmbracelet/lipgloss"
	// "github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/huh"
	// "github.com/fogleman/ease"
	// "github.com/lucasb-eyer/go-colorful"

	"github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

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
func get_latest_sync(tableName string, svc *dynamodb.Client) (string, error) {
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
		return "", nil
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

    return latestItem.Timestamp, nil
}

type mainModel struct {
	tableView table.Model
	textView string
	tickerView string
	timer timer.Model
	textInput textinput.Model
	editing bool
	config ObsyncianConfig
	svc *dynamodb.Client
	latest_ts_synced string
}

//! All the stuff we need to initialise


func initialModel() mainModel {

	//!
	obsyncianConfig := configure_local()
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		// config.WithSharedConfigProfile("test-account")),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(obsyncianConfig.Credentials.Key, obsyncianConfig.Credentials.Secret, "")),
		config.WithRegion("ap-southeast-2"), // TODO should be in config file
	)
	if err != nil {
		log.Fatalf("failed to load AWS config, %v", err)
	}

	svc := dynamodb.NewFromConfig(cfg)

	// tableName := "Obsyncian"
	//!

	columns := []table.Column{
		{Title: "Item", Width: 10},
		{Title: "Value", Width: 15},
	}

	rows := []table.Row{
		{"ID", obsyncianConfig.ID},
        {"Local Path", obsyncianConfig.Local},
        {"Cloud Path", obsyncianConfig.Cloud},
        {"Cloud Provider", obsyncianConfig.Provider},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(7),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	ti := textinput.New()
	ti.Placeholder = "New Value"
	ti.CharLimit = 20
	ti.Width = 20

	return mainModel{
		tableView: t,
		textView: "This is a static text view.\n\nDisplay last 3 months of cost from Cost Explorer.",
		timer: timer.NewWithInterval(time.Minute * 10, time.Second * 10), // TODO TIME!!!! time.Minute, time.Minute
		textInput: ti,
		config: obsyncianConfig,
		svc: svc,
		latest_ts_synced: "",
	}
}

func (m mainModel) Init() tea.Cmd {
	return m.timer.Init()
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if !m.editing {
				return m, tea.Quit
			}
		case "enter":
			if m.editing {
				rows := m.tableView.Rows()
				rows[m.tableView.Cursor()][1] = m.textInput.Value()
				m.tableView.SetRows(rows)
				m.editing = false
				m.textInput.Blur()
				m.textInput.Reset()
			} else {
				m.editing = true
				m.textInput.SetValue(m.tableView.SelectedRow()[1])
				m.textInput.Focus()
			}
		case "esc":
			if m.editing {
				m.editing = false
				m.textInput.Blur()
				m.textInput.Reset()
			}
		}

	case timer.TickMsg:
		m.timer, cmd = m.timer.Update(msg)
		// m.tickerView = fmt.Sprint("Last updated: %v", time.Now().Format(time.RFC1123))

		// TODO : spinner
		// time.Sleep(5 * time.Second) // This blocks
		tableName := "Obsyncian"
		// * Get latest timestamp / Check if the table is empty
		tickerViewContent := fmt.Sprintf("Last updated: %v", time.Now().Format(time.RFC1123)) // TODO, which is better? fmt.Sprint
        latest_sync, err := get_latest_sync(tableName, m.svc)
		if err != nil {
            log.Fatalf("failed to get latest sync from DynamoDB, %v", err)
        }
		tickerViewContent = tickerViewContent + fmt.Sprintf("\n LATEST CLOUD SYNC: %v \n", latest_sync)
		m.tickerView = tickerViewContent

		// If table is empty, and we are the first machine to connect, sync our local copy to cloud, and insert into DB
        if latest_sync == "" {
            tickerViewContent = tickerViewContent + fmt.Sprintf("Syncing up...\n")
			m.tickerView = tickerViewContent
            Sync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)
            tickerViewContent = tickerViewContent + fmt.Sprintf("Finished syncing.\n")
			m.tickerView = tickerViewContent

            create_user(m.config.ID, tableName, m.svc)
        }

        // * Check if our UUID is in the table - and latest time we have synced up (first time creation may not count as a sync up, but down)
        input := &dynamodb.GetItemInput{
            TableName: aws.String(tableName),
            Key: map[string]types.AttributeValue{
                "UserId": &types.AttributeValueMemberS{Value: m.config.ID},
            },
        }
        // Using context.TODO() for simplicity, in a real application, you might pass a request-scoped context.
        result, err := m.svc.GetItem(context.TODO(), input)
        if err != nil {
            log.Fatalf("failed to get item from DynamoDB, %v", err)
        }
        //! if we don't exist, sync down - OVERWRITING DIR!!!
        if result.Item == nil {
            tickerViewContent = tickerViewContent + fmt.Sprintf("No item found with UUID: %s in table: %s\n", m.config.ID, tableName)
			m.tickerView = tickerViewContent

            // create the user and sync down?
            create_user(m.config.ID, tableName, m.svc)
            tickerViewContent = tickerViewContent + fmt.Sprintf("Syncing...\n")
			m.tickerView = tickerViewContent
			// TODO : how do we get this to update... pass in m.ticketView pointer?
			// TODO: for starters, just return the file changes,
            Sync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials)
            tickerViewContent = tickerViewContent + fmt.Sprintf("Finished syncing.\n")
			m.tickerView = tickerViewContent
        }

        var item Item
        err = attributevalue.UnmarshalMap(result.Item, &item)
        if err != nil {
            log.Fatalf("failed to unmarshal DynamoDB item, %v", err)
        }

        // if our timestamp is less than latest timestamp, plus not ours, sync down
        if m.config.ID != item.UserId && latest_sync >= item.Timestamp && m.latest_ts_synced < latest_sync {
            tickerViewContent = tickerViewContent + fmt.Sprintf("Sync down\n")
			m.tickerView = tickerViewContent
            Sync(fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Local, m.config.Credentials)
            // TODO : update dynamo with the same timestamp? or just track latest timestamp we've synced with locally?
            m.latest_ts_synced = latest_sync
        } else {
            tickerViewContent = tickerViewContent + fmt.Sprintf("Already synced with Cloud\n")
			m.tickerView = tickerViewContent
        }

        // TODO : should this be in the else statement above?
        //* Check if there are new local changes with a dry run, if there are, sync to cloud
        // fmt.Println("CHECKING IF THINGS ARE SYNCED")
        isChanges, plannedChanges, _ := SyncDryRun(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)
        if (isChanges) {
            tickerViewContent = tickerViewContent + fmt.Sprintf("Sync up:\n %v \n", plannedChanges)
			m.tickerView = tickerViewContent
            Sync(m.config.Local, fmt.Sprintf("s3://%s", m.config.Cloud), m.config.Credentials)

            // Update timestamp in table
            update := expression.Set(expression.Name("Timestamp"), expression.Value(time.Now().Format("20060102150405")))
            expr, err := expression.NewBuilder().WithUpdate(update).Build()
            putInput := &dynamodb.UpdateItemInput{
                TableName: aws.String(tableName),
                Key: map[string]types.AttributeValue{
                    "UserId": &types.AttributeValueMemberS{Value: m.config.ID},
                },
                ExpressionAttributeNames:  expr.Names(),
                ExpressionAttributeValues: expr.Values(),
                UpdateExpression:          expr.Update(),
                ReturnValues: types.ReturnValueUpdatedNew, // Return the item's attributes after they are updated
            }
            _, err = m.svc.UpdateItem(context.TODO(), putInput)
            if err != nil {
                log.Printf("Couldn't add item to table. Here's why: %v\n", err)
            }
        } else {
            tickerViewContent = tickerViewContent + fmt.Sprintf("No changes to sync\n")
			m.tickerView = tickerViewContent
        }

		m.tickerView = tickerViewContent
		//!

		cmds = append(cmds, cmd)

		if m.timer.Timedout() {
			m.timer = timer.NewWithInterval(time.Minute, time.Minute) // TODO TIME!!!
			cmds = append(cmds, m.timer.Init())
		}
	
	case timer.StartStopMsg:
		m.timer, cmd = m.timer.Update(msg)
		cmds = append(cmds, m.timer.Init())

		// m.timer = timer.NewWithInterval(time.Minute, time.Second)
		// cmds = append(cmds, m.timer.Init())
	}

	if m.editing {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.tableView, cmd = m.tableView.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m mainModel) View() string {
	viewContainerStyle := lipgloss.NewStyle().Padding(1, 2)
	topViewStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	bottomViewStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).MarginTop(1)

	// Top Views (Table and Text)
	var tableViewContent string
	if m.editing {
		tableViewContent = m.tableView.View() + "\n" + m.textInput.View()
	} else {
		tableViewContent = m.tableView.View() + "\nPress 'enter' to edit."
	}

	leftView := topViewStyle.Width(35).Render(tableViewContent)
	rightView := topViewStyle.Width(45).Render(m.textView)
	// TODO : make them all vertically aligned
	topViews := lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)

	// Bottom View
	bottomView := bottomViewStyle.Width(82).Render(m.tickerView)

	// Final Layout
	finalView := lipgloss.JoinVertical(lipgloss.Left, topViews, bottomView)

	return viewContainerStyle.Render(finalView)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
