// poc ui
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
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

// Cloud check interval - check for remote changes every 5 minutes
const cloudCheckInterval = 5 * time.Minute

// File watcher debounce time
const fileWatcherDebounce = 500 * time.Millisecond

type mainModel struct {
	tableView        table.Model
	textView         viewport.Model
	tickerView       viewport.Model
	timer            timer.Model
	textInput        textinput.Model
	editing          bool
	focus            focusState
	currentTab       tabState
	config           ObsyncianConfig
	svc              *dynamodb.Client
	latest_ts_synced string
	firstCycle       int // just used to skip the first time cycle
	outputBuffer     *bytes.Buffer
	syncState        SyncState
	fileWatcher      *FileWatcher
	program          *tea.Program // Reference to send messages from background goroutines
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
		config.WithRegion("ap-southeast-2"), // TODO should be in config file
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

	vp := viewport.New(80, 20) // Width 43, Height 20 lines
	vp.SetContent("This is a static text view.\n\nDisplay last 3 months of cost from Cost Explorer.")

	tickerVp := viewport.New(80, 10) // Width 80, Height 10 lines
	tickerVp.SetContent("Initializing... watching for file changes")

	outputBuffer := &bytes.Buffer{}

	return mainModel{
		tableView:        t,
		textView:         vp,
		tickerView:       tickerVp,
		timer:            timer.NewWithInterval(cloudCheckInterval, cloudCheckInterval), // Cloud check timer
		textInput:        ti,
		focus:            focusTabs,
		currentTab:       tabTable,
		config:           obsyncianConfig,
		svc:              svc,
		latest_ts_synced: "",
		firstCycle:       0,
		outputBuffer:     outputBuffer,
		syncState:        SyncIdle,
		fileWatcher:      nil, // Will be initialized after program starts
		program:          nil, // Will be set after program starts
	}
}

func (m mainModel) Init() tea.Cmd {
	// Return a batch of commands: start timer and trigger initial sync
	return tea.Batch(
		m.timer.Init(),
		func() tea.Msg {
			// Small delay to allow program reference to be set
			time.Sleep(100 * time.Millisecond)
			return CloudCheckMsg{}
		},
	)
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if !m.editing {
				// Clean up file watcher before quitting
				if m.fileWatcher != nil {
					m.fileWatcher.Stop()
				}
				return m, tea.Quit
			}
		case "tab", "shift+tab": // these are the same because we only have two things to switch between
			if !m.editing {
				m.focus = (m.focus + 1) % 2 // switch between tabs and ticker
			}
		case "left", "h", "right", "l": //same because we only have two things to switch between (split out if tabs added)
			if !m.editing && m.focus == focusTabs {
				m.currentTab = (m.currentTab + 1) % 2 // switch between tabs
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
			if !m.editing {
				switch m.focus {
				case focusTabs:
					if m.currentTab == tabTable {
						// Table handles its own navigation
					} else if m.currentTab == tabText {
						m.textView.LineUp(1)
					}
				case focusTicker:
					m.tickerView.LineDown(1)
				}
			}
		case "pgup":
			if !m.editing {
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
			if !m.editing {
				switch m.focus {
				case focusTabs:
					if m.currentTab == tabText {
						m.textView.ViewUp()
					}
				case focusTicker:
					m.tickerView.ViewDown()
				}
			}
		case "s": // Manual sync trigger
			if !m.editing && m.syncState == SyncIdle {
				m.appendOutput("⚡ Manual sync triggered")
				return m, func() tea.Msg { return FileChangeMsg{} }
			}
		}

	case SyncProgressMsg:
		// Real-time sync progress updates
		m.appendOutput(msg.Text)

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
			go handleSyncAsync(&m, m.program)
		} else if m.syncState == SyncInProgress {
			m.appendOutput("⏳ Sync already in progress, will check again after completion")
		}

	case CloudCheckMsg:
		// Periodic cloud check - check for remote changes
		if m.syncState == SyncIdle && m.program != nil {
			m.syncState = SyncInProgress
			m.appendOutput("☁️  Checking for cloud changes...")
			go handleSyncAsync(&m, m.program)
		}

	case timer.TickMsg:
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
			go handleSyncAsync(&m, m.program)
		}

		if m.timer.Timedout() {
			m.timer = timer.NewWithInterval(cloudCheckInterval, cloudCheckInterval)
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
		case focusTabs:
			if m.currentTab == tabTable {
				m.tableView, cmd = m.tableView.Update(msg)
				cmds = append(cmds, cmd)
			} else if m.currentTab == tabText {
				m.textView, cmd = m.textView.Update(msg)
				cmds = append(cmds, cmd)
			}
		case focusTicker:
			m.tickerView, cmd = m.tickerView.Update(msg)
			cmds = append(cmds, cmd)
		}
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
		tabContent = m.textView.View()
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
	helpText := "\nTab: Switch panes | ←/→: Switch tabs | ↑/↓: Scroll | PgUp/PgDn: Page scroll | S: Manual sync | Enter: Edit (table) | Esc: Cancel edit | Ctrl+C/Q: Quit"

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

	if _, err := p.Run(); err != nil {
		// Clean up file watcher on error
		if model.fileWatcher != nil {
			model.fileWatcher.Stop()
		}
		log.Fatal(err)
	}

	// Clean up file watcher on normal exit
	if model.fileWatcher != nil {
		model.fileWatcher.Stop()
	}
}
