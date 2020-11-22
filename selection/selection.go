package selection

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/reflow/wrap"
	"github.com/muesli/termenv"
)

const (
	DefaultSelectTemplate = `
{{- if .Label -}}
  {{ Bold .Label }}
{{ end -}}
{{ if .Filter }}
  {{- print "Filter: " .FilterInput }}
{{ end }}

{{- range  $i, $choice := .Choices }}
  {{- if IsScrollUpHintPosition $i }}
    {{- "⇡ " -}}
  {{- else if IsScrollDownHintPosition $i -}}
    {{- "⇣ " -}} 
  {{- else -}}
    {{- "  " -}}
  {{- end -}} 

  {{- if eq $.SelectedIndex $i }}
   {{- Foreground "32" (Bold (print "▸ " $choice.String "\n")) }}
  {{- else }}
    {{- print "  " $choice.String "\n"}}
  {{- end }}
{{- end}}`
	DefaultFilterPlaceholder = "Type to filter choices"
)

type Prompt struct {
	Choices           []*Choice
	Label             string
	Filter            func(filterText string, choice *Choice) bool
	FilterPlaceholder string
	Template          string
	PageSize          int

	Err error

	filterInput      textinput.Model
	currentChoices   []*Choice
	availableChoices int
	currentIdx       int
	scrollOffset     int
	width            int
	tmpl             *template.Template
}

func (sp *Prompt) Init() tea.Cmd {
	sp.reindexChoices()

	if sp.FilterPlaceholder == "" {
		sp.FilterPlaceholder = DefaultFilterPlaceholder
	}

	if sp.Template == "" {
		sp.Template = DefaultSelectTemplate
	}

	sp.tmpl = template.New("")
	sp.tmpl.Funcs(termenv.TemplateFuncs(termenv.ColorProfile()))
	sp.tmpl.Funcs(template.FuncMap{
		"IsScrollDownHintPosition": func(idx int) bool {
			return sp.canScrollDown() && (idx == len(sp.currentChoices)-1)
		},
		"IsScrollUpHintPosition": func(idx int) bool {
			return sp.canScrollUp() && idx == 0 && sp.scrollOffset > 0
		},
	})

	sp.tmpl, sp.Err = sp.tmpl.Parse(sp.Template)
	if sp.Err != nil {
		return tea.Quit
	}

	sp.filterInput = textinput.NewModel()
	sp.filterInput.Placeholder = sp.FilterPlaceholder
	sp.filterInput.Prompt = ""
	sp.filterInput.Focus()
	sp.width = 70
	sp.currentChoices, sp.availableChoices = sp.filteredAndPagedChoices()

	return textinput.Blink
}

func (sp *Prompt) Choice() (interface{}, error) {
	if sp.Err != nil {
		return nil, sp.Err
	}

	if len(sp.currentChoices) == 0 {
		return nil, fmt.Errorf("no choices")
	}

	if sp.currentIdx < 0 || sp.currentIdx >= len(sp.currentChoices) {
		return nil, fmt.Errorf("choice index out of bounds")
	}

	return sp.currentChoices[sp.currentIdx].Value, nil
}

func (sp *Prompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sp.Err != nil {
		return sp, tea.Quit
	}

	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			sp.Err = fmt.Errorf("selection was aborted")

			return sp, tea.Quit
		case tea.KeyEsc:
			sp.filterInput.SetValue("")

			return sp, nil
		case tea.KeyEnter:
			if len(sp.currentChoices) == 0 {
				return sp, nil
			}

			return sp, tea.Quit
		case tea.KeyDown:
			sp.cursorDown()

			return sp, nil
		case tea.KeyUp:
			sp.cursorUp()

			return sp, nil
		case tea.KeyPgDown, tea.KeyRight:
			sp.scrollDown()

			return sp, nil
		case tea.KeyPgUp, tea.KeyLeft:
			sp.scrollUp()

			return sp, nil
		}
	case tea.WindowSizeMsg:
		sp.width = msg.Width - 1
	case error:
		sp.Err = msg

		return sp, tea.Quit
	}

	if sp.Filter == nil {
		return sp, cmd
	}

	previousFilter := sp.filterInput.Value()

	sp.filterInput, cmd = sp.filterInput.Update(msg)

	if sp.filterInput.Value() != previousFilter {
		sp.currentIdx = 0
		sp.scrollOffset = 0
		sp.currentChoices, sp.availableChoices = sp.filteredAndPagedChoices()
	}

	return sp, cmd
}

func (sp *Prompt) View() string {
	viewBuffer := &bytes.Buffer{}

	err := sp.tmpl.Execute(viewBuffer, map[string]interface{}{
		"Label":         sp.Label,
		"Filter":        sp.Filter != nil,
		"FilterInput":   sp.filterInput.View(),
		"Choices":       sp.currentChoices,
		"NChoices":      len(sp.currentChoices),
		"SelectedIndex": sp.currentIdx,
		"PageSize":      sp.PageSize,
		"IsPaged":       sp.PageSize > 0 && len(sp.currentChoices) > sp.PageSize,
		"AllChoices":    sp.Choices,
		"NAllChoices":   len(sp.Choices),
	})
	if err != nil {
		sp.Err = err

		return "Template Error: " + err.Error()
	}

	w := wrap.NewWriter(sp.width)
	w.PreserveSpace = true
	_, _ = w.Write([]byte(wordwrap.String(viewBuffer.String(), sp.width)))

	return w.String()
}

func (sp Prompt) filteredAndPagedChoices() ([]*Choice, int) {
	choices := []*Choice{}

	var available, ignored int

	for _, choice := range sp.Choices {
		if sp.Filter != nil && !sp.Filter(sp.filterInput.Value(), choice) {
			continue
		}

		available++

		if sp.PageSize > 0 && len(choices) >= sp.PageSize {
			break
		}

		if (sp.PageSize > 0) && (ignored < sp.scrollOffset) {
			ignored++

			continue
		}

		choices = append(choices, choice)
	}

	return choices, available
}

func (sp *Prompt) canScrollDown() bool {
	if sp.PageSize <= 0 || sp.availableChoices <= sp.PageSize {
		return false
	}

	if sp.scrollOffset+sp.PageSize >= len(sp.Choices) {
		return false
	}

	return true
}

func (sp *Prompt) canScrollUp() bool {
	return sp.scrollOffset > 0
}

func (sp *Prompt) cursorDown() {
	if sp.currentIdx == len(sp.currentChoices)-1 && sp.canScrollDown() {
		sp.scrollDown()
	}

	sp.currentIdx = min(len(sp.currentChoices)-1, sp.currentIdx+1)
}

func (sp *Prompt) cursorUp() {
	if sp.currentIdx == 0 && sp.canScrollUp() {
		sp.scrollUp()
	}

	sp.currentIdx = max(0, sp.currentIdx-1)
}

func (sp *Prompt) scrollDown() {
	if sp.PageSize <= 0 || sp.scrollOffset+sp.PageSize >= sp.availableChoices {
		return
	}

	sp.currentIdx = max(0, sp.currentIdx-1)
	sp.scrollOffset++
	sp.currentChoices, sp.availableChoices = sp.filteredAndPagedChoices()
}

func (sp *Prompt) scrollUp() {
	if sp.PageSize <= 0 || sp.scrollOffset <= 0 {
		return
	}

	sp.currentIdx = min(len(sp.currentChoices)-1, sp.currentIdx+1)
	sp.scrollOffset--
	sp.currentChoices, sp.availableChoices = sp.filteredAndPagedChoices()
}

func (sp *Prompt) reindexChoices() {
	for i, choice := range sp.Choices {
		choice.Index = i
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}