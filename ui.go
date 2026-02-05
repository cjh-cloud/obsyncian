// poc ui
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/timer"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type focusState int

const (
	focusTabs   focusState = iota // 0
	focusTicker                   // 1
)

type tabState int

const (
	tabTable tabState = iota
	tabText
)

// Cloud check interval - fallback polling interval when SQS is not configured
const cloudCheckInterval = 5 * time.Minute

// File watcher debounce time
const fileWatcherDebounce = 500 * time.Millisecond

type mainModel struct {
	tableView        table.Model
	textView         viewport.Model
	tickerView       viewport.Model
	timer            timer.Model
	textInput        textinput.Model
	chatInput        textinput.Model
	chatInputFocused bool // true while typing in chat; Escape blurs, Enter (when empty) can re-focus
	chatMessages     []ChatMessage
	chatLoading      bool
	editing          bool
	focus            focusState
	currentTab       tabState
	config           ObsyncianConfig
	svc              *dynamodb.Client
	bedrockClient    *BedrockClient // nil if KnowledgeBaseID not configured
	latest_ts_synced string
	firstCycle       int // just used to skip the first time cycle
	outputBuffer     *bytes.Buffer
	syncState        SyncState
	fileWatcher      *FileWatcher
	sqsListener      *SQSListener  // SQS listener for event-driven sync (nil if not configured)
	useEventDriven   bool          // true if using SQS notifications, false for polling
	program          *tea.Program  // Reference to send messages from background goroutines
}

func (m *mainModel) appendOutput(text string) {
	m.outputBuffer.WriteString(text + "\n")
	m.tickerView.SetContent(m.outputBuffer.String())
	m.tickerView.GotoBottom()
}

//! All the stuff we need to initialise

func initialModel() mainModel {
	obsyncianConfig := configure_local()
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(obsyncianConfig.Credentials.Key, obsyncianConfig.Credentials.Secret, "")),
		config.WithRegion(obsyncianConfig.AWSRegion()),
	)
	if err != nil {
		log.Fatalf("failed to load AWS config, %v", err)
	}

	svc := dynamodb.NewFromConfig(cfg)

	columns := []table.Column{
		{Title: "Item", Width: 20},
		{Title: "Value", Width: 50},
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
	ti.CharLimit = 80
	ti.Width = 80

	chatTi := textinput.New()
	chatTi.Placeholder = "Ask about your notes..."
	chatTi.CharLimit = 500
	chatTi.Width = 78

	vp := viewport.New(80, 20)
	vp.SetContent("") // Chat messages rendered in View

	tickerVp := viewport.New(80, 10)
	tickerVp.SetContent("Initializing... watching for file changes")

	outputBuffer := &bytes.Buffer{}

	useEventDriven := obsyncianConfig.SNSTopicARN != ""

	var bedrockClient *BedrockClient
	if obsyncianConfig.KnowledgeBaseID != "" {
		var err error
		bedrockClient, err = NewBedrockClient(obsyncianConfig)
		if err != nil {
			log.Printf("Bedrock client not available: %v", err)
			bedrockClient = nil
		}
	}

	return mainModel{
		tableView:        t,
		textView:         vp,
		tickerView:       tickerVp,
		timer:            timer.NewWithInterval(cloudCheckInterval, cloudCheckInterval),
		textInput:        ti,
		chatInput:        chatTi,
		chatMessages:     nil,
		chatLoading:      false,
		editing:          false,
		focus:            focusTabs,
		currentTab:       tabTable,
		config:           obsyncianConfig,
		svc:              svc,
		bedrockClient:    bedrockClient,
		latest_ts_synced: "",
		firstCycle:       0,
		outputBuffer:     outputBuffer,
		syncState:        SyncIdle,
		fileWatcher:      nil,
		sqsListener:      nil,
		useEventDriven:   useEventDriven,
		program:          nil,
	}
}

func (m mainModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		// Trigger initial sync
		func() tea.Msg {
			// Small delay to allow program reference to be set
			time.Sleep(100 * time.Millisecond)
			return CloudCheckMsg{}
		},
	}

	// Only start the timer if not using event-driven sync
	if !m.useEventDriven {
		cmds = append(cmds, m.timer.Init())
	}

	return tea.Batch(cmds...)
}

// runBedrockQueryCmd returns a tea.Cmd that runs RetrieveAndGenerate in a goroutine and sends AgentResponseMsg.
func runBedrockQueryCmd(program *tea.Program, client *BedrockClient, kbID, query, region string) tea.Cmd {
	return func() tea.Msg {
		if program == nil || client == nil {
			return AgentResponseMsg{Err: fmt.Errorf("bedrock not configured")}
		}
		go func() {
			ctx := context.Background()
			text, err := client.RetrieveAndGenerate(ctx, kbID, query, region)
			program.Send(AgentResponseMsg{Text: text, Err: err})
		}()
		return nil
	}
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		// When typing in chat, only Enter and Escape are handled here; all other keys go to chat input
		chatTyping := m.currentTab == tabText && m.chatInputFocused
		if chatTyping && key != "enter" && key != "esc" {
			// Skip app keybindings so s, i, arrows, tab etc. don't fire; key will go to chatInput below
			break
		}
		switch key {
		case "ctrl+c", "q":
			if !m.editing {
				// Clean up resources before quitting
				if m.fileWatcher != nil {
					m.fileWatcher.Stop()
				}
				if m.sqsListener != nil {
					m.sqsListener.Stop()
				}
				return m, tea.Quit
			}
		case "tab", "shift+tab": // switch between top pane (tabs) and bottom pane (ticker)
			if !m.editing {
				m.focus = (m.focus + 1) % 2
				if m.focus == focusTicker {
					m.chatInput.Blur()
					m.chatInputFocused = false
				}
			}
		case "left", "h", "right", "l": // switch between Table and AI tabs (when not typing in chat)
			if !m.editing && m.focus == focusTabs && (!m.chatInputFocused || m.currentTab != tabText) {
				m.currentTab = (m.currentTab + 1) % 2
				if m.currentTab == tabText {
					m.chatInput.Focus()
					m.chatInputFocused = true
				} else {
					m.chatInput.Blur()
					m.chatInputFocused = false
				}
			}
		case "enter":
			if m.editing {
				rows := m.tableView.Rows()
				rows[m.tableView.Cursor()][1] = m.textInput.Value()
				m.tableView.SetRows(rows)
				m.editing = false
				m.textInput.Blur()
				m.textInput.Reset()
			} else if m.focus == focusTabs && m.currentTab == tabTable {
				m.editing = true
				m.textInput.SetValue(m.tableView.SelectedRow()[1])
				m.textInput.Focus()
			} else if m.focus == focusTabs && m.currentTab == tabText && !m.chatLoading {
				if m.chatInputFocused && m.config.KnowledgeBaseID != "" {
					query := m.chatInput.Value()
					if strings.TrimSpace(query) != "" {
						m.chatMessages = append(m.chatMessages, ChatMessage{Role: "user", Text: query})
						m.chatInput.Reset()
						m.chatLoading = true
						m.updateChatViewContent()
						cmd = runBedrockQueryCmd(m.program, m.bedrockClient, m.config.KnowledgeBaseID, query, m.config.AWSRegion())
						return m, cmd
					}
				} else {
					// Re-focus chat so user can type
					m.chatInput.Focus()
					m.chatInputFocused = true
				}
			}
		case "esc":
			if m.editing {
				m.editing = false
				m.textInput.Blur()
				m.textInput.Reset()
			} else if m.currentTab == tabText && m.chatInputFocused {
				// Exit chat typing so s, i, tab, arrows work again; press Enter to type again
				m.chatInput.Blur()
				m.chatInputFocused = false
			}
		case "up", "k":
			if !m.editing && (m.currentTab != tabText || !m.chatInputFocused) {
				switch m.focus {
				case focusTabs:
					if m.currentTab == tabTable {
						// Table handles its own navigation
					} else if m.currentTab == tabText {
						m.textView.LineUp(1)
					}
				case focusTicker:
					m.tickerView.LineUp(1)
				}
			}
		case "down", "j":
			if !m.editing && (m.currentTab != tabText || !m.chatInputFocused) {
				switch m.focus {
				case focusTabs:
					if m.currentTab == tabTable {
						// Table handles its own navigation
					} else if m.currentTab == tabText {
						m.textView.LineDown(1)
					}
				case focusTicker:
					m.tickerView.LineDown(1)
				}
			}
		case "pgup":
			if !m.editing && (m.currentTab != tabText || !m.chatInputFocused) {
				switch m.focus {
				case focusTabs:
					if m.currentTab == tabText {
						m.textView.ViewUp()
					}
				case focusTicker:
					m.tickerView.ViewUp()
				}
			}
		case "pgdown":
			if !m.editing && (m.currentTab != tabText || !m.chatInputFocused) {
				switch m.focus {
				case focusTabs:
					if m.currentTab == tabText {
						m.textView.ViewDown()
					}
				case focusTicker:
					m.tickerView.ViewDown()
				}
			}
		case "s": // Manual sync trigger (not when on chat tab)
			if !m.editing && m.currentTab != tabText && m.syncState == SyncIdle {
				m.appendOutput("⚡ Manual sync triggered")
				return m, func() tea.Msg { return FileChangeMsg{} }
			}
		case "i": // Sync KB (not when on chat tab)
			if !m.editing && m.currentTab != tabText && m.config.KnowledgeBaseID != "" && m.config.DataSourceID != "" && m.program != nil {
				m.appendOutput("📚 Starting knowledge base sync...")
				program := m.program
				cfg := m.config
				return m, func() tea.Msg {
					go func() {
						ctx := context.Background()
						jobID, err := StartKnowledgeBaseIngestion(ctx, cfg)
						program.Send(KBSyncResultMsg{JobID: jobID, Err: err})
					}()
					return nil
				}
			}
		}

	case SyncProgressMsg:
		// Real-time sync progress updates
		m.appendOutput(msg.Text)

	case SyncedDownMsg:
		// Update the timestamp to track which cloud version we've synced
		m.latest_ts_synced = msg.Timestamp

	case SyncCompleteMsg:
		// Sync operation completed
		m.syncState = SyncIdle
		if msg.Success {
			m.appendOutput("─────────────────────────────────")
		} else if msg.Error != nil {
			m.appendOutput(fmt.Sprintf("❗ Sync failed: %v", msg.Error))
			m.appendOutput("─────────────────────────────────")
		}

	case FileChangeMsg:
		// File system change detected - trigger sync if not already in progress
		if m.syncState == SyncIdle && m.program != nil {
			m.syncState = SyncInProgress
			m.appendOutput("📁 File change detected, starting sync...")
			// Pass current lastSyncedTs to avoid reading stale state in goroutine
			lastSynced := m.latest_ts_synced
			go handleSyncAsync(&m, m.program, lastSynced)
		} else if m.syncState == SyncInProgress {
			m.appendOutput("⏳ Sync already in progress, will check again after completion")
		}

	case AgentResponseMsg:
		m.chatLoading = false
		if msg.Err != nil {
			m.chatMessages = append(m.chatMessages, ChatMessage{Role: "assistant", Text: "Error: " + msg.Err.Error()})
		} else {
			m.chatMessages = append(m.chatMessages, ChatMessage{Role: "assistant", Text: msg.Text})
		}
		m.updateChatViewContent()
		return m, nil

	case KBSyncResultMsg:
		if msg.Err != nil {
			m.appendOutput(fmt.Sprintf("❗ KB sync failed: %v", msg.Err))
		} else {
			m.appendOutput("✓ Knowledge base indexing started (job: " + msg.JobID + ")")
		}

	case CloudCheckMsg:
		// Cloud check - triggered by SQS notification or periodic polling
		if m.syncState == SyncIdle && m.program != nil {
			m.syncState = SyncInProgress
			if m.useEventDriven {
				m.appendOutput("📬 S3 change notification received, checking cloud...")
			} else {
				m.appendOutput("☁️  Checking for cloud changes...")
			}
			// Pass current lastSyncedTs to avoid reading stale state in goroutine
			lastSynced := m.latest_ts_synced
			go handleSyncAsync(&m, m.program, lastSynced)
		}

	case timer.TickMsg:
		// Only handle timer events if using polling mode (not event-driven)
		if !m.useEventDriven {
			m.timer, cmd = m.timer.Update(msg)
			cmds = append(cmds, cmd)

			// Skip the first cycle (initialization)
			if m.firstCycle < 1 {
				m.firstCycle = m.firstCycle + 1
				m.tickerView.SetContent("Starting up...")
				return m, tea.Batch(cmds...)
			}

			// Periodic cloud check
			if m.syncState == SyncIdle && m.program != nil {
				m.syncState = SyncInProgress
				m.appendOutput("☁️  Periodic cloud check...")
				lastSynced := m.latest_ts_synced
				go handleSyncAsync(&m, m.program, lastSynced)
			}

			if m.timer.Timedout() {
				m.timer = timer.NewWithInterval(cloudCheckInterval, cloudCheckInterval)
				cmds = append(cmds, m.timer.Init())
			}
		}

	case timer.StartStopMsg:
		if !m.useEventDriven {
			m.timer, cmd = m.timer.Update(msg)
			cmds = append(cmds, m.timer.Init())
		}
	}

	if m.editing {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		switch m.focus {
		case focusTabs:
			if m.currentTab == tabTable {
				m.tableView, cmd = m.tableView.Update(msg)
				cmds = append(cmds, cmd)
			} else if m.currentTab == tabText {
				if m.chatInputFocused {
					m.chatInput, cmd = m.chatInput.Update(msg)
					cmds = append(cmds, cmd)
				}
				m.textView, cmd = m.textView.Update(msg)
				cmds = append(cmds, cmd)
			}
		case focusTicker:
			m.tickerView, cmd = m.tickerView.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	if m.currentTab == tabText {
		m.updateChatViewContent()
	}

	return m, tea.Batch(cmds...)
}

func (m mainModel) View() string {
	viewContainerStyle := lipgloss.NewStyle().Padding(1, 2)

	// Focus styles
	focusedStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(0, 1)
	unfocusedStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)

	// Tab styles
	activeTabStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1).
		MarginRight(1)

	inactiveTabStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1).
		MarginRight(1)

	// Create tab headers
	var tableTab, textTab string
	if m.currentTab == tabTable {
		tableTab = activeTabStyle.Render("Table")
		textTab = inactiveTabStyle.Render("Text")
	} else {
		tableTab = inactiveTabStyle.Render("Table")
		textTab = activeTabStyle.Render("Text")
	}

	tabHeaders := lipgloss.JoinHorizontal(lipgloss.Top, tableTab, textTab)

	// Create tab content
	var tabContent string
	if m.currentTab == tabTable {
		if m.editing {
			tabContent = m.tableView.View() + "\n" + m.textInput.View()
		} else {
			tabContent = m.tableView.View() + "\nPress 'enter' to edit."
		}
	} else {
		tabContent = m.textView.View() + "\n" + m.chatInput.View()
	}

	// Apply focus styling to the entire tab area
	var tabView string
	if m.focus == focusTabs {
		tabView = focusedStyle.Width(82).Render(tabHeaders + "\n" + tabContent)
	} else {
		tabView = unfocusedStyle.Width(82).Render(tabHeaders + "\n" + tabContent)
	}

	// Bottom View (Ticker) with sync status indicator
	var statusIndicator string
	if m.syncState == SyncInProgress {
		statusIndicator = " 🔄 Syncing..."
	} else {
		statusIndicator = " ✓ Ready"
	}

	var bottomView string
	if m.focus == focusTicker {
		bottomView = focusedStyle.Width(82).Render(statusIndicator + "\n" + m.tickerView.View())
	} else {
		bottomView = unfocusedStyle.Width(82).Render(statusIndicator + "\n" + m.tickerView.View())
	}

	// Final Layout
	finalView := lipgloss.JoinVertical(lipgloss.Left, tabView, bottomView)

	// Add help text
	helpText := "\nTab: Switch panes (tabs ↔ log) | ←/→: Switch tabs | ↑/↓: Scroll | Esc: Leave chat (then ←/→ or Tab) | S: Sync | I: Sync KB | Ctrl+C/Q: Quit"

	return viewContainerStyle.Render(finalView + helpText)
}

// updateTextViewContent updates the content of the textView viewport
func (m *mainModel) updateTextViewContent(content string) {
	m.textView.SetContent(content)
}

// chatContentWidth is the wrap width for chat text in the viewport (80-wide viewport minus border/padding).
const chatContentWidth = 76

// wrapChatLine wraps s to chatContentWidth so long lines (e.g. error messages) are visible in the viewport.
func wrapChatLine(s string) string {
	return lipgloss.NewStyle().Width(chatContentWidth).Render(s)
}

// updateChatViewContent builds chat message list into a string and sets it on the text viewport.
func (m *mainModel) updateChatViewContent() {
	if len(m.chatMessages) == 0 && !m.chatLoading {
		if m.config.KnowledgeBaseID == "" {
			m.textView.SetContent("Add knowledgeBaseId to your config to chat with your notes.")
		} else {
			m.textView.SetContent("Ask a question about your notes below.")
		}
		return
	}
	var b bytes.Buffer
	for _, msg := range m.chatMessages {
		prefix := "You: "
		if msg.Role == "assistant" {
			prefix = "Agent: "
		}
		b.WriteString(wrapChatLine(prefix + msg.Text))
		b.WriteString("\n\n")
	}
	if m.chatLoading {
		b.WriteString("Agent: ... thinking ...\n")
	}
	m.textView.SetContent(b.String())
	m.textView.GotoBottom()
}

// updateTickerViewContent updates the content of the tickerView viewport
func (m *mainModel) updateTickerViewContent(content string) {
	m.tickerView.SetContent(content)
}

func main() {
	model := initialModel()

	p := tea.NewProgram(&model, tea.WithAltScreen())

	// Store program reference in model for background goroutines
	model.program = p

	// Initialize file watcher after we have the program reference
	watcher, err := NewFileWatcher(model.config.Local, fileWatcherDebounce, func() {
		// Send file change message to the Bubble Tea program
		if model.program != nil {
			model.program.Send(FileChangeMsg{})
		}
	})
	if err != nil {
		log.Printf("Warning: Could not start file watcher: %v", err)
	} else {
		model.fileWatcher = watcher
		watcher.Start()
	}

	// Initialize SQS listener if SNS topic is configured (event-driven sync)
	if model.useEventDriven {
		sqsListener, err := NewSQSListener(
			model.config.Credentials,
			model.config.SNSTopicARN,
			model.config.ID,
			p,
		)
		if err != nil {
			log.Printf("Warning: Could not start SQS listener: %v. Falling back to polling.", err)
			model.useEventDriven = false
		} else {
			model.sqsListener = sqsListener
			sqsListener.Start()
			log.Printf("Event-driven sync enabled via SQS")
		}
	}

	if _, err := p.Run(); err != nil {
		// Clean up resources on error
		if model.fileWatcher != nil {
			model.fileWatcher.Stop()
		}
		if model.sqsListener != nil {
			model.sqsListener.Stop()
		}
		log.Fatal(err)
	}

	// Clean up resources on normal exit
	if model.fileWatcher != nil {
		model.fileWatcher.Stop()
	}
	if model.sqsListener != nil {
		model.sqsListener.Stop()
	}
}
