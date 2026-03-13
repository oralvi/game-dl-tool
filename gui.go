package main

import (
	"fmt"
	"image/color"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type guiState struct {
	app    fyne.App
	window fyne.Window

	baseConfig  config
	aliases     map[string][]string
	allRows     []resultRow
	bestRows    []resultRow
	logLines    []string
	lastDomains []string
	selected    map[string]bool
	running     bool

	gameChecks   map[string]*widget.Check
	familyRadio  *widget.RadioGroup
	traceCheck   *widget.Check
	cacheCheck   *widget.Check
	portEntry    *widget.Entry
	roundsEntry  *widget.Entry
	workersEntry *widget.Entry

	progressBar   *widget.ProgressBar
	progressLabel *widget.Label
	statusLabel   *widget.Label
	summaryLabel  *widget.Label
	configLabel   *widget.Label

	startButton  *widget.Button
	exportButton *widget.Button
	hostsButton  *widget.Button

	bestBox    *fyne.Container
	resultsBox *fyne.Container
	logGrid    *widget.TextGrid
	logScroll  *container.Scroll
}

func runGUI() error {
	baseCfg := defaultRuntimeConfig()
	fileCfg, _, created, err := loadFileConfig(baseCfg.ConfigPath, false)
	if err != nil {
		return err
	}
	if err := applyFileConfig(&baseCfg, fileCfg, false); err != nil {
		return err
	}

	application := app.NewWithID("github.com/oralvi.game-dl-tool")
	application.SetIcon(appIconResource())
	application.Settings().SetTheme(dlTheme{})

	state := newGUIState(application, baseCfg, selectedGamesFromConfig(fileCfg))
	if created {
		state.appendLog(fmt.Sprintf("Initialized default config at %s", baseCfg.ConfigPath))
		state.setStatus(fmt.Sprintf("Initialized default config at %s", baseCfg.ConfigPath))
	}

	state.window.ShowAndRun()
	return nil
}

func newGUIState(application fyne.App, baseCfg config, selectedGames map[string]bool) *guiState {
	state := &guiState{
		app:        application,
		baseConfig: baseCfg,
		selected:   make(map[string]bool),
	}

	state.window = application.NewWindow("game-dl-tool")
	state.window.Resize(fyne.NewSize(1320, 860))
	state.window.SetIcon(appIconResource())

	state.gameChecks = make(map[string]*widget.Check, len(knownGames))
	gameList := container.NewVBox()
	for _, game := range knownGames {
		game := game
		check := widget.NewCheck(displayGameName(game), nil)
		check.SetChecked(selectedGames[game.ID])
		state.gameChecks[game.ID] = check
		gameList.Add(check)
	}

	state.familyRadio = widget.NewRadioGroup([]string{"IPv6", "IPv4", "Dual Stack"}, nil)
	state.familyRadio.Horizontal = true
	switch baseCfg.Family {
	case family4:
		state.familyRadio.SetSelected("IPv4")
	case familyAll:
		state.familyRadio.SetSelected("Dual Stack")
	default:
		state.familyRadio.SetSelected("IPv6")
	}

	state.traceCheck = widget.NewCheck("Enable trace probing", nil)
	state.traceCheck.SetChecked(baseCfg.TraceEnabled)
	state.cacheCheck = widget.NewCheck("Prefer cached results when available", nil)
	state.cacheCheck.SetChecked(baseCfg.UseCache)

	state.portEntry = widget.NewEntry()
	state.portEntry.SetText(strconv.Itoa(baseCfg.Port))
	state.roundsEntry = widget.NewEntry()
	state.roundsEntry.SetText(strconv.Itoa(baseCfg.Rounds))
	state.workersEntry = widget.NewEntry()
	state.workersEntry.SetText(strconv.Itoa(baseCfg.Workers))

	state.configLabel = widget.NewLabel(fmt.Sprintf("Config: %s", filepath.Clean(baseCfg.ConfigPath)))
	state.configLabel.Wrapping = fyne.TextWrapWord

	state.progressBar = widget.NewProgressBar()
	state.progressLabel = widget.NewLabel("Ready to scan")
	state.statusLabel = widget.NewLabel("Select the game endpoints you want to compare, then start a scan.")
	state.statusLabel.Wrapping = fyne.TextWrapWord
	state.summaryLabel = widget.NewLabel("No scan results yet.")
	state.summaryLabel.Wrapping = fyne.TextWrapWord

	state.bestBox = container.NewVBox(widget.NewLabel("Best candidates will appear here after a scan."))
	state.resultsBox = container.NewVBox(widget.NewLabel("Run a scan to populate the candidate list."))
	state.logGrid = widget.NewTextGrid()
	state.logGrid.SetText("Logs will stream here during a run.")
	state.logScroll = container.NewScroll(state.logGrid)

	state.startButton = widget.NewButtonWithIcon("Start Scan", theme.MediaPlayIcon(), state.startScan)
	state.startButton.Importance = widget.HighImportance
	state.exportButton = widget.NewButtonWithIcon("Save CSV", theme.DocumentSaveIcon(), state.saveCSV)
	state.exportButton.Disable()
	state.hostsButton = widget.NewButtonWithIcon("Write Selected to hosts", theme.DownloadIcon(), state.writeSelectedHosts)
	state.hostsButton.Disable()

	content := container.NewBorder(
		container.NewVBox(
			buildHero(),
			widget.NewCard(
				"Run State",
				"",
				container.NewVBox(
					state.progressLabel,
					state.progressBar,
					state.statusLabel,
				),
			),
			state.buildMetricsRow(),
		),
		nil,
		state.buildSidebar(gameList),
		nil,
		container.NewAppTabs(
			container.NewTabItem("Best Picks", container.NewScroll(state.bestBox)),
			container.NewTabItem("Candidates", container.NewScroll(state.resultsBox)),
			container.NewTabItem("Run Log", state.logScroll),
		),
	)

	state.window.SetContent(container.NewPadded(content))
	return state
}

func buildHero() fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: 0x17, G: 0x67, B: 0x68, A: 0xFF})
	bg.SetMinSize(fyne.NewSize(0, 120))

	icon := canvas.NewImageFromResource(appIconResource())
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(54, 54))

	title := widget.NewLabelWithStyle("game-dl-tool", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	subtitle := widget.NewLabel("Scan CN game CDN domains, compare IPv4 and IPv6 candidates, and write tagged hosts entries without leaving the desktop app.")
	subtitle.Wrapping = fyne.TextWrapWord

	badge := widget.NewLabel("Fyne desktop preview")
	body := container.NewBorder(
		nil,
		nil,
		icon,
		nil,
		container.NewVBox(title, subtitle, badge),
	)

	return container.NewStack(bg, container.NewPadded(body))
}

func (s *guiState) buildSidebar(gameList fyne.CanvasObject) fyne.CanvasObject {
	form := widget.NewForm(
		widget.NewFormItem("Port", s.portEntry),
		widget.NewFormItem("Rounds", s.roundsEntry),
		widget.NewFormItem("Workers", s.workersEntry),
	)

	advanced := widget.NewAccordion(
		widget.NewAccordionItem("Advanced", form),
	)

	sidebar := widget.NewCard(
		"Scan Setup",
		"Minimal UI, full backend logic",
		container.NewVBox(
			widget.NewLabelWithStyle("Games", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			gameList,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Address Family", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			s.familyRadio,
			s.traceCheck,
			s.cacheCheck,
			advanced,
			s.configLabel,
			widget.NewSeparator(),
			container.NewGridWithColumns(1, s.startButton, s.exportButton, s.hostsButton),
		),
	)
	sidebar.Resize(fyne.NewSize(320, 0))
	return sidebar
}

func (s *guiState) buildMetricsRow() fyne.CanvasObject {
	return container.NewGridWithColumns(
		3,
		widget.NewCard("Selection", "", widget.NewLabel("Choose one or more games.")),
		widget.NewCard("Candidates", "", s.summaryLabel),
		widget.NewCard("Hosts", "", widget.NewLabel("Select rows in Candidates, then write tagged #DLTOOL entries.")),
	)
}

func (s *guiState) startScan() {
	if s.running {
		return
	}

	cfg, domains, aliases, err := s.buildRunConfig()
	if err != nil {
		dialog.ShowError(err, s.window)
		return
	}

	logger, err := newRunLoggerWithHook(cfg.LogFile, s.appendLog)
	if err != nil {
		dialog.ShowError(err, s.window)
		return
	}
	cfg.Logger = logger
	cfg.ProgressSink = s.updateProgress

	s.running = true
	s.aliases = aliases
	s.lastDomains = append([]string(nil), domains...)
	s.selected = make(map[string]bool)
	s.progressBar.SetValue(0)
	s.progressLabel.SetText("Preparing scan...")
	s.setStatus(fmt.Sprintf("Scanning %d domains with %d resolvers. Trace: %t", len(domains), len(cfg.ResolverSpecs), cfg.TraceEnabled))
	s.startButton.Disable()
	s.exportButton.Disable()
	s.hostsButton.Disable()
	s.logLines = nil
	s.logGrid.SetText("Starting scan...")

	go func() {
		defer logger.Close()

		logger.Printf("session started")
		logger.Printf("selected domains: %s", strings.Join(domains, ", "))

		rows, usedCache, err := executeScan(cfg, domains)
		if err != nil {
			fyne.Do(func() {
				s.running = false
				s.startButton.Enable()
				s.updateHostsButton()
				dialog.ShowError(err, s.window)
				s.setStatus(err.Error())
			})
			return
		}

		fyne.Do(func() {
			s.running = false
			s.startButton.Enable()
			s.allRows = append([]resultRow(nil), rows...)
			s.bestRows = bestRowsByDomainAndFamily(rows)
			s.selected = make(map[string]bool, len(s.bestRows))
			for _, row := range s.bestRows {
				s.selected[rowSelectionKey(row)] = true
			}
			s.refreshResults()
			s.refreshBestCards()
			s.exportButton.Enable()
			s.updateHostsButton()
			if usedCache {
				s.setStatus(fmt.Sprintf("Loaded %d candidates from cache.", len(rows)))
			} else {
				s.setStatus(fmt.Sprintf("Live scan finished with %d candidates.", len(rows)))
			}
			s.progressBar.SetValue(1)
			s.progressLabel.SetText("Scan complete")
			s.summaryLabel.SetText(s.summaryText())
		})
	}()
}

func (s *guiState) buildRunConfig() (config, []string, map[string][]string, error) {
	selectedIDs := s.selectedGameIDs()
	if len(selectedIDs) == 0 {
		return config{}, nil, nil, fmt.Errorf("select at least one game target")
	}

	cfg := s.baseConfig
	cfg.Interactive = false
	cfg.ManualInput = false
	cfg.ShowProgress = false
	cfg.GamesSelection = strings.Join(selectedIDs, "")
	cfg.Family = familyFromLabel(s.familyRadio.Selected)
	cfg.TraceEnabled = s.traceCheck.Checked
	cfg.UseCache = s.cacheCheck.Checked
	cfg.Port = parsePositiveIntOrDefault(s.portEntry.Text, cfg.Port)
	cfg.Rounds = parsePositiveIntOrDefault(s.roundsEntry.Text, cfg.Rounds)
	cfg.Workers = parsePositiveIntOrDefault(s.workersEntry.Text, cfg.Workers)

	domains, aliases, err := targetsFromGameSelection(cfg.GamesSelection)
	if err != nil {
		return config{}, nil, nil, err
	}
	return cfg, domains, aliases, nil
}

func (s *guiState) selectedGameIDs() []string {
	var ids []string
	for _, game := range knownGames {
		check := s.gameChecks[game.ID]
		if check != nil && check.Checked {
			ids = append(ids, game.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func familyFromLabel(label string) ipFamily {
	switch label {
	case "IPv4":
		return family4
	case "Dual Stack":
		return familyAll
	default:
		return family6
	}
}

func parsePositiveIntOrDefault(input string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func (s *guiState) updateProgress(snapshot progressSnapshot) {
	fyne.Do(func() {
		if snapshot.Total <= 0 {
			s.progressBar.SetValue(0)
			s.progressLabel.SetText("Preparing scan...")
			return
		}

		s.progressBar.SetValue(float64(snapshot.Completed) / float64(snapshot.Total))
		detail := fmt.Sprintf(
			"%d/%d complete • active %d • elapsed %s",
			snapshot.Completed,
			snapshot.Total,
			snapshot.ActiveCount,
			formatElapsed(snapshot.Elapsed),
		)
		if snapshot.ActiveSummary != "" {
			detail += " • " + snapshot.ActiveSummary
		} else if snapshot.LastCompleted != "" {
			detail += " • last " + truncateLabel(snapshot.LastCompleted, 32)
		}
		if snapshot.Final {
			detail = "Scan complete"
		}
		s.progressLabel.SetText(detail)
	})
}

func (s *guiState) appendLog(line string) {
	fyne.Do(func() {
		entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line)
		s.logLines = append(s.logLines, entry)
		if len(s.logLines) > 500 {
			s.logLines = append([]string(nil), s.logLines[len(s.logLines)-500:]...)
		}
		s.logGrid.SetText(strings.Join(s.logLines, "\n"))
	})
}

func (s *guiState) refreshBestCards() {
	objects := make([]fyne.CanvasObject, 0, len(s.bestRows))
	for _, row := range s.bestRows {
		title := widget.NewLabelWithStyle(row.Domain, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		title.Wrapping = fyne.TextWrapWord
		body := container.NewGridWithColumns(
			4,
			metricBlock("Family", strings.ToUpper(string(row.Family))),
			metricBlock("Address", valueOrDash(row.Address)),
			metricBlock("TCP", millisLabel(row.ConnectLatency, row.ConnectOK)),
			metricBlock("Trace", row.TraceStatus),
		)
		objects = append(objects, widget.NewCard("", "", container.NewVBox(title, body)))
	}
	if len(objects) == 0 {
		objects = append(objects, widget.NewLabel("No best candidates available yet."))
	}
	s.bestBox.Objects = objects
	s.bestBox.Refresh()
}

func (s *guiState) refreshResults() {
	objects := make([]fyne.CanvasObject, 0, len(s.allRows))
	for _, row := range s.allRows {
		row := row
		check := widget.NewCheck("", func(checked bool) {
			key := rowSelectionKey(row)
			if checked {
				s.selected[key] = true
			} else {
				delete(s.selected, key)
			}
			s.updateHostsButton()
		})
		check.SetChecked(s.selected[rowSelectionKey(row)])
		check.DisableableWidget.Enable()
		if strings.TrimSpace(row.Address) == "" {
			check.Disable()
		}

		title := widget.NewLabelWithStyle(row.Domain, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		title.Wrapping = fyne.TextWrapWord
		subtitle := widget.NewLabel(fmt.Sprintf("%s • %s", strings.ToUpper(string(row.Family)), valueOrDash(row.Address)))
		metrics := container.NewGridWithColumns(
			4,
			metricBlock("TCP", millisLabel(row.ConnectLatency, row.ConnectOK)),
			metricBlock("DNS", millisLabel(row.ResolveLatency, row.ResolveLatency > 0)),
			metricBlock("Trace", row.TraceStatus),
			metricBlock("Resolvers", valueOrDash(row.ResolverList)),
		)
		note := widget.NewLabel(valueOrDash(row.Note))
		note.Wrapping = fyne.TextWrapWord

		card := widget.NewCard("", "", container.NewVBox(title, subtitle, metrics, note))
		objects = append(objects, container.NewBorder(nil, nil, check, nil, card))
	}
	if len(objects) == 0 {
		objects = append(objects, widget.NewLabel("No candidates available yet."))
	}
	s.resultsBox.Objects = objects
	s.resultsBox.Refresh()
}

func metricBlock(label string, value string) fyne.CanvasObject {
	return container.NewVBox(
		widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel(value),
	)
}

func millisLabel(latency time.Duration, enabled bool) string {
	if !enabled {
		return "-"
	}
	return formatMillis(latency) + " ms"
}

func rowSelectionKey(row resultRow) string {
	return row.Domain + "|" + string(row.Family) + "|" + row.Address
}

func (s *guiState) updateHostsButton() {
	count := 0
	for _, row := range s.allRows {
		if s.selected[rowSelectionKey(row)] {
			count++
		}
	}

	if s.running || count == 0 {
		s.hostsButton.SetText("Write Selected to hosts")
		s.hostsButton.Disable()
		return
	}

	s.hostsButton.SetText(fmt.Sprintf("Write %d Selected to hosts", count))
	s.hostsButton.Enable()
}

func (s *guiState) selectedRows() []resultRow {
	rows := make([]resultRow, 0, len(s.selected))
	for _, row := range s.allRows {
		if s.selected[rowSelectionKey(row)] {
			rows = append(rows, row)
		}
	}
	sortRows(rows)
	return rows
}

func (s *guiState) writeSelectedHosts() {
	rows := s.selectedRows()
	if len(rows) == 0 {
		dialog.ShowInformation("No Selection", "Select one or more address rows before writing hosts entries.", s.window)
		return
	}

	dialog.NewConfirm(
		"Write hosts entries",
		fmt.Sprintf("Write %d tagged #DLTOOL entries into the system hosts file?", len(rows)),
		func(ok bool) {
			if !ok {
				return
			}
			path, err := upsertHostsFile("system", rows, s.aliases)
			if err != nil {
				dialog.ShowError(err, s.window)
				return
			}
			s.appendLog(fmt.Sprintf("hosts written to %s", path))
			s.setStatus(fmt.Sprintf("Hosts updated at %s", path))
			dialog.ShowInformation("Hosts Updated", fmt.Sprintf("Tagged entries were written to:\n%s", path), s.window)
		},
		s.window,
	).Show()
}

func (s *guiState) saveCSV() {
	if len(s.allRows) == 0 {
		dialog.ShowInformation("Nothing to Export", "Run a scan first so there is something to save.", s.window)
		return
	}

	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, s.window)
			return
		}
		if writer == nil {
			return
		}

		path := writer.URI().Path()
		_ = writer.Close()
		if path == "" {
			dialog.ShowInformation("Save Cancelled", "No output file was selected.", s.window)
			return
		}

		if err := writeCSV(path, s.allRows); err != nil {
			dialog.ShowError(err, s.window)
			return
		}
		s.appendLog(fmt.Sprintf("csv written to %s", path))
		s.setStatus(fmt.Sprintf("CSV exported to %s", path))
	}, s.window)
	saveDialog.SetFileName("scan-results.csv")
	saveDialog.Show()
}

func (s *guiState) summaryText() string {
	if len(s.allRows) == 0 {
		return "No scan results yet."
	}

	parts := []string{
		fmt.Sprintf("%d domains", len(s.lastDomains)),
		fmt.Sprintf("%d candidates", len(s.allRows)),
		fmt.Sprintf("%d best picks", len(s.bestRows)),
	}
	if bestV6, ok := fastestSuccessfulRow(s.bestRows, family6); ok {
		parts = append(parts, fmt.Sprintf("fastest IPv6 %s ms", formatMillis(bestV6.ConnectLatency)))
	}
	return strings.Join(parts, " • ")
}

func (s *guiState) setStatus(text string) {
	s.statusLabel.SetText(text)
}

func displayGameName(game gameTarget) string {
	switch game.Key {
	case "genshin":
		return "Genshin Impact"
	case "hsr":
		return "Honkai: Star Rail"
	case "bh3":
		return "Honkai Impact 3rd"
	case "zzz":
		return "Zenless Zone Zero"
	case "wuwa-cn":
		return "Wuthering Waves CN"
	default:
		return game.Name
	}
}
