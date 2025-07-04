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
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/huh"

	"github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
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

type focusState int

const (
	focusTable focusState = iota // 0
	focusText // 1
	focusTicker // 2
)

type mainModel struct {
	tableView table.Model
	textView viewport.Model
	tickerView viewport.Model
	timer timer.Model
	textInput textinput.Model
	editing bool
	focus focusState
	config ObsyncianConfig
	svc *dynamodb.Client
	latest_ts_synced string
	firstCycle int // just used to skip the first time cycle
}

//! All the stuff we need to initialise

func initialModel() mainModel {

	//!
	obsyncianConfig := configure_local()
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(obsyncianConfig.Credentials.Key, obsyncianConfig.Credentials.Secret, "")),
		config.WithRegion("ap-southeast-2"), // TODO should be in config file
	)
	if err != nil {
		log.Fatalf("failed to load AWS config, %v", err)
	}

	svc := dynamodb.NewFromConfig(cfg)
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

	vp := viewport.New(43, 20) // Width 43, Height 20 lines
	vp.SetContent("This is a static text view.\n\nDisplay last 3 months of cost from Cost Explorer.")

	tickerVp := viewport.New(80, 10) // Width 80, Height 10 lines
	tickerVp.SetContent("Ticker view - sync status will appear here")

	return mainModel{
		tableView: t,
		textView: vp,
		tickerView: tickerVp,
		timer: timer.NewWithInterval(time.Second, time.Second), // TODO TIME!!!! time.Minute, time.Minute - (time.Minute * 10, time.Second * 10)
		textInput: ti,
		focus: focusTable,
		config: obsyncianConfig,
		svc: svc,
		latest_ts_synced: "",
		firstCycle: 0,
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
		case "tab":
			if !m.editing {
				m.focus = (m.focus + 1) % 3
			}
		case "shift+tab":
			if !m.editing {
				m.focus = (m.focus + 2) % 3 // Go backwards
			}
		case "enter":
			if m.editing {
				rows := m.tableView.Rows()
				rows[m.tableView.Cursor()][1] = m.textInput.Value()
				m.tableView.SetRows(rows)
				m.editing = false
				m.textInput.Blur()
				m.textInput.Reset()
			} else if m.focus == focusTable {
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
		case "up", "k":
			if !m.editing {
				switch m.focus {
				case focusTable:
					// Table handles its own navigation
				case focusText:
					m.textView.LineUp(1)
				case focusTicker:
					m.tickerView.LineUp(1)
				}
			}
		case "down", "j":
			if !m.editing {
				switch m.focus {
				case focusTable:
					// Table handles its own navigation
				case focusText:
					m.textView.LineDown(1)
				case focusTicker:
					m.tickerView.LineDown(1)
				}
			}
		case "pgup":
			if !m.editing {
				switch m.focus {
				case focusText:
					m.textView.ViewUp()
				case focusTicker:
					m.tickerView.ViewUp()
				}
			}
		case "pgdown":
			if !m.editing {
				switch m.focus {
				case focusText:
					m.textView.ViewDown()
				case focusTicker:
					m.tickerView.ViewDown()
				}
			}
		}

	case timer.TickMsg:
		m.timer, cmd = m.timer.Update(msg)

		// working out the first load that says syncing down
		if m.firstCycle < 1 {
			m.firstCycle = m.firstCycle + 1
			m.tickerView.SetContent(fmt.Sprintf("cycle: %v", m.firstCycle)) // TODO loader
			cmds = append(cmds, cmd)
			m.timer = timer.NewWithInterval(time.Minute * 60, time.Minute) // tick ever minute, for an hour
			cmds = append(cmds, m.timer.Init())
			return m, tea.Batch(cmds...)
		}

		tickerContent := handleSync(&m)
		m.tickerView.SetContent(tickerContent)
		cmds = append(cmds, cmd)

		if m.timer.Timedout() {
			m.timer = timer.NewWithInterval(time.Minute * 60, time.Minute) // tick ever minute, for an hour
			cmds = append(cmds, m.timer.Init())
		}

	case timer.StartStopMsg:
		m.timer, cmd = m.timer.Update(msg)
		cmds = append(cmds, m.timer.Init())
	}

	if m.editing {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		// Only update the focused view for navigation
		switch m.focus {
		case focusTable:
			m.tableView, cmd = m.tableView.Update(msg)
			cmds = append(cmds, cmd)
		case focusText:
			m.textView, cmd = m.textView.Update(msg)
			cmds = append(cmds, cmd)
		case focusTicker:
			m.tickerView, cmd = m.tickerView.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m mainModel) View() string {
	viewContainerStyle := lipgloss.NewStyle().Padding(1, 2)
	// topViewStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	// bottomViewStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).MarginTop(1)
	
	// Focus styles
	focusedStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(0, 1)
	unfocusedStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)

	// Top Views (Table and Text)
	var tableViewContent string
	if m.editing {
		tableViewContent = m.tableView.View() + "\n" + m.textInput.View()
	} else {
		tableViewContent = m.tableView.View() + "\nPress 'enter' to edit."
	}

	// Apply focus styling
	var leftView, rightView string
	if m.focus == focusTable {
		leftView = focusedStyle.Width(35).Render(tableViewContent)
	} else {
		leftView = unfocusedStyle.Width(35).Render(tableViewContent)
	}
	
	if m.focus == focusText {
		rightView = focusedStyle.Width(45).Render(m.textView.View())
	} else {
		rightView = unfocusedStyle.Width(45).Render(m.textView.View())
	}
	
	// TODO : make them all vertically aligned
	topViews := lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)

	// Bottom View
	var bottomView string
	if m.focus == focusTicker {
		bottomView = focusedStyle.Width(82).Render(m.tickerView.View())
	} else {
		bottomView = unfocusedStyle.Width(82).Render(m.tickerView.View())
	}

	// Final Layout
	finalView := lipgloss.JoinVertical(lipgloss.Left, topViews, bottomView)

	// Add help text
	helpText := "\nTab: Switch focus | ↑/↓: Scroll | PgUp/PgDn: Page scroll | Enter: Edit (table) | Esc: Cancel edit | Ctrl+C/Q: Quit"
	
	return viewContainerStyle.Render(finalView + helpText)
}

// updateTextViewContent updates the content of the textView viewport
func (m *mainModel) updateTextViewContent(content string) {
	m.textView.SetContent(content)
}

// updateTickerViewContent updates the content of the tickerView viewport
func (m *mainModel) updateTickerViewContent(content string) {
	m.tickerView.SetContent(content)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
