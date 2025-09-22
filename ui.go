// poc ui
package main

import (
	"fmt"
    "log"
    "context"
    "time"
    "bytes"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/timer"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

    "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type focusState int

const (
	focusTabs focusState = iota // 0
	focusTicker // 1
)

type tabState int

const (
	tabTable tabState = iota
	tabText
)

type mainModel struct {
	tableView table.Model
	textView viewport.Model
	tickerView viewport.Model
	timer timer.Model
	textInput textinput.Model
	editing bool
	focus focusState
	currentTab tabState
	config ObsyncianConfig
	svc *dynamodb.Client
	latest_ts_synced string
	firstCycle int // just used to skip the first time cycle
	outputBuffer  *bytes.Buffer
}

func (m *mainModel) appendOutput(text string) {
	m.outputBuffer.WriteString(text + "\n")
	m.tickerView.SetContent(m.outputBuffer.String())
	m.tickerView.GotoBottom()
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
	tickerVp.SetContent("Ticker view - sync status will appear here")

	outputBuffer := &bytes.Buffer{}

	return mainModel{
		tableView: t,
		textView: vp,
		tickerView: tickerVp,
		timer: timer.NewWithInterval(time.Second, time.Second), // TODO TIME!!!! time.Minute, time.Minute - (time.Minute * 10, time.Second * 10)
		textInput: ti,
		focus: focusTabs,
		currentTab: tabTable,
		config: obsyncianConfig,
		svc: svc,
		latest_ts_synced: "",
		firstCycle: 0,
		outputBuffer: outputBuffer,
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
		case "tab", "shift+tab": // these are the same because we only have two things to switch between
			if !m.editing {
				m.focus = (m.focus + 1) % 2 // switch between tabs and ticker
			}
		case "left", "h", "right", "l": //same because we only have two things to switch between (split out if tabs added)
			if !m.editing  && m.focus == focusTabs {
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
					m.tickerView.LineUp(1) // TODO what does this do?
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

		handleSync(&m)
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

	// Bottom View (Ticker)
	var bottomView string
	if m.focus == focusTicker {
		bottomView = focusedStyle.Width(82).Render(m.tickerView.View())
	} else {
		bottomView = unfocusedStyle.Width(82).Render(m.tickerView.View())
	}

	// Final Layout
	finalView := lipgloss.JoinVertical(lipgloss.Left, tabView, bottomView)

	// Add help text
	helpText := "\nTab: Switch panes | ←/→: Switch tabs | ↑/↓: Scroll | PgUp/PgDn: Page scroll | Enter: Edit (table) | Esc: Cancel edit | Ctrl+C/Q: Quit"

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
