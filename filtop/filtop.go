package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	defaultHost     = "localhost"
	defaultPort     = 5066
	defaultInterval = 5
	historySize     = 30
)

var (
	app          *tview.Application
	pages        *tview.Pages
	pageMap      map[string]tview.Primitive
	lastStats    *FilebeatStats
	history      []*FilebeatStats
	refresh      time.Duration
	currentFocus int
)

// Estructuras de datos mejoradas para mapear correctamente la respuesta JSON
type FilebeatStats struct {
	Timestamp time.Time `json:"timestamp"`
	Beat      struct {
		CPU struct {
			System struct {
				Ticks uint64 `json:"ticks"`
				Time  struct {
					MS uint64 `json:"ms"`
				} `json:"time"`
			} `json:"system"`
			Total struct {
				Ticks uint64 `json:"ticks"`
				Time  struct {
					MS uint64 `json:"ms"`
				} `json:"time"`
				Value uint64 `json:"value"`
			} `json:"total"`
			User struct {
				Ticks uint64 `json:"ticks"`
				Time  struct {
					MS uint64 `json:"ms"`
				} `json:"time"`
			} `json:"user"`
		} `json:"cpu"`
		Memstats struct {
			MemoryAlloc uint64 `json:"memory_alloc"`
			RSS         uint64 `json:"rss"`
		} `json:"memstats"`
		Info struct {
			Uptime struct {
				MS uint64 `json:"ms"`
			} `json:"uptime"`
		} `json:"info"`
	} `json:"beat"`
	Libbeat struct {
		Pipeline struct {
			Queue struct {
				Filled struct {
					Events uint64 `json:"events"`
				} `json:"filled"`
				MaxEvents uint64 `json:"max_events"`
			} `json:"queue"`
			Events struct {
				Total    uint64 `json:"total"`
				Dropped  uint64 `json:"dropped"`
				Failed   uint64 `json:"failed"`
				Filtered uint64 `json:"filtered"`
			} `json:"events"`
		} `json:"pipeline"`
	} `json:"libbeat"`
	Filebeat struct {
		Harvester struct {
			Running    uint64 `json:"running"`
			Open       uint64 `json:"open_files"`
			Closed     uint64 `json:"closed"`
			Started    uint64 `json:"started"`
			Terminated uint64 `json:"skipped"`
		} `json:"harvester"`
		Inputs  []Input `json:"inputs"`
		Modules struct {
			List []struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
				Errors  int    `json:"errors"`
			} `json:"list"`
		} `json:"modules"`
	} `json:"filebeat"`
	System struct {
		Load struct {
			Norm struct {
				Load1  float64 `json:"1"`
				Load5  float64 `json:"5"`
				Load15 float64 `json:"15"`
			} `json:"norm"`
		} `json:"load"`
	} `json:"system"`
}

type Input struct {
	ID            string `json:"id"`
	Type          string `json:"input"`
	Device        string `json:"device"`
	Packets       uint64 `json:"packets"`
	Bytes         uint64 `json:"bytes"`
	Events        uint64 `json:"events"`
	Active        bool   `json:"active"`
	ArrivalPeriod struct {
		Histogram map[string]interface{} `json:"histogram"`
	} `json:"arrival_period"`
	ProcessingTime struct {
		Histogram map[string]interface{} `json:"histogram"`
	} `json:"processing_time"`
	Throughput struct {
		Bytes  float64 `json:"bytes"`
		Events float64 `json:"events"`
	} `json:"throughput"`
	Files uint64 `json:"files"`
}

func main() {
	host := flag.String("host", defaultHost, "Host de Filebeat")
	port := flag.Int("port", defaultPort, "Puerto de Filebeat")
	interval := flag.Int("interval", defaultInterval, "Intervalo de refresco en segundos")
	flag.Parse()

	refresh = time.Duration(*interval) * time.Second

	app = tview.NewApplication()
	pages = tview.NewPages()
	pageMap = make(map[string]tview.Primitive)

	initUI()
	go dataWorker(*host, *port)
	setupSignalHandler()

	if err := app.Run(); err != nil {
		log.Fatalf("Error ejecutando la aplicación: %v", err)
	}
}

func setupSignalHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Apagando la aplicación...")
		app.Stop()
		os.Exit(0)
	}()
}

func initUI() {
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow)

	header := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("[::b]FILTOP[::-] v2.0")

	body := tview.NewFlex()
	leftPanel := tview.NewFlex().SetDirection(tview.FlexRow)
	rightPanel := tview.NewFlex().SetDirection(tview.FlexRow)

	leftPanel.AddItem(createSystemPanel(), 8, 1, false)
	leftPanel.AddItem(createQueuePanel(), 6, 1, false)
	leftPanel.AddItem(createHarvesterChart(), 8, 1, false)

	rightPanel.AddItem(createInputsTable(), 0, 2, false)
	rightPanel.AddItem(createModulesWidget(), 0, 1, false)

	body.AddItem(leftPanel, 0, 1, false)
	body.AddItem(rightPanel, 0, 2, false)

	mainFlex.AddItem(header, 1, 1, false)
	mainFlex.AddItem(body, 0, 1, false)

	pages.AddPage("main", mainFlex, true, true)
	pageMap["main"] = mainFlex
	app.SetRoot(pages, true)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			pages.SwitchToPage("main")
		case tcell.KeyTab:
			currentFocus = (currentFocus + 1) % 2
			app.SetFocus(getFocusableComponent(currentFocus))
		case tcell.KeyBacktab:
			currentFocus = (currentFocus - 1 + 2) % 2
			app.SetFocus(getFocusableComponent(currentFocus))
		case tcell.KeyEnter:
			if currentFocus == 1 {
				showInputDetails()
			}
		}
		return event
	})
}

func getFocusableComponent(index int) tview.Primitive {
	if mainPage := getPrimitiveFromPage("main"); mainPage != nil {
		if flex, ok := mainPage.(*tview.Flex); ok {
			body := flex.GetItem(1).(*tview.Flex)
			switch index {
			case 0:
				return body.GetItem(0).(*tview.Flex).GetItem(0)
			case 1:
				return body.GetItem(1).(*tview.Flex).GetItem(0)
			}
		}
	}
	return nil
}

func showInputDetails() {
	if lastStats == nil || len(lastStats.Filebeat.Inputs) == 0 {
		return
	}

	list := tview.NewList().ShowSecondaryText(false)
	list.SetTitle(" Detalles de Inputs ").SetBorder(true)

	for _, input := range lastStats.Filebeat.Inputs {
		list.AddItem(fmt.Sprintf("%s (%s)", input.Type, input.Device), "", 0, func() {
			showInputMetrics(input)
		})
	}

	list.AddItem("Regresar", "", 'b', func() {
		pages.SwitchToPage("main")
	})

	pages.AddPage("input_details", list, true, true)
	pages.SwitchToPage("input_details")
}

func showInputMetrics(input Input) {
	textView := tview.NewTextView().SetDynamicColors(true)
	textView.SetBorder(true).SetTitle(fmt.Sprintf(" Métricas: %s ", input.ID))

	var builder strings.Builder
	fmt.Fprintf(&builder, "[yellow]Tipo:[-] %s\n", input.Type)
	fmt.Fprintf(&builder, "[yellow]Dispositivo:[-] %s\n", input.Device)
	fmt.Fprintf(&builder, "[yellow]Paquetes:[-] %d\n", input.Packets)
	fmt.Fprintf(&builder, "[yellow]Bytes:[-] %s\n", formatBytes(input.Bytes))
	fmt.Fprintf(&builder, "[yellow]Eventos:[-] %d\n", input.Events)
	fmt.Fprintf(&builder, "[yellow]Activo:[-] %t\n", input.Active)
	fmt.Fprintf(&builder, "\n[yellow]Histogramas:[-]\n")
	fmt.Fprintf(&builder, "Arrival Period:\n%s", formatHistogram(input.ArrivalPeriod.Histogram))
	fmt.Fprintf(&builder, "\nProcessing Time:\n%s", formatHistogram(input.ProcessingTime.Histogram))

	textView.SetText(builder.String())

	modal := tview.NewModal().
		SetText(textView.GetText(true)).
		AddButtons([]string{"Regresar"}).
		SetDoneFunc(func(_ int, _ string) {
			pages.SwitchToPage("input_details")
		})

	pages.AddPage("input_metrics", modal, true, true)
	pages.SwitchToPage("input_metrics")
}

func formatHistogram(histo map[string]interface{}) string {
	var builder strings.Builder
	for k, v := range histo {
		if val, ok := v.(float64); ok {
			fmt.Fprintf(&builder, "  %-15s: %.2f\n", k, val)
		}
	}
	return builder.String()
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func createSystemPanel() *tview.Table {
	table := tview.NewTable().SetBorders(false)
	table.SetTitle(" Sistema ").SetBorder(true)
	addMetricRow(table, 0, "CPU Total:", "0.0%", tcell.ColorOrange)
	addMetricRow(table, 1, "Memoria RSS:", "0.0 MB", tcell.ColorGreen)
	addMetricRow(table, 2, "Uptime:", "0h 0m", tcell.ColorBlue)
	addMetricRow(table, 3, "Load Avg:", "0.00 0.00 0.00", tcell.ColorYellow)
	return table
}

func dataWorker(host string, port int) {
	statsURL := fmt.Sprintf("http://%s:%d/stats", host, port)
	inputsURL := fmt.Sprintf("http://%s:%d/inputs", host, port)

	client := &http.Client{Timeout: 10 * time.Second}

	for {
		stats, err := fetchStats(client, statsURL)
		if err != nil {
			log.Printf("Error obteniendo estadísticas: %v", err)
			time.Sleep(refresh)
			continue
		}

		inputs, err := fetchInputs(client, inputsURL)
		if err != nil {
			log.Printf("Error obteniendo inputs: %v", err)
		} else {
			stats.Filebeat.Inputs = inputs
		}

		history = append(history, stats)
		if len(history) > historySize {
			history = history[1:]
		}

		lastStats = stats
		app.QueueUpdateDraw(updateUI)
		time.Sleep(refresh)
	}

}

func fetchStats(client *http.Client, url string) (*FilebeatStats, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error: código de estado %d", resp.StatusCode)
	}

	var stats FilebeatStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}
	stats.Timestamp = time.Now()
	return &stats, nil
}

func fetchInputs(client *http.Client, url string) ([]Input, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error: código de estado %d", resp.StatusCode)
	}

	var inputs []Input // Ahora decodifica directamente a []Input
	if err := json.NewDecoder(resp.Body).Decode(&inputs); err != nil {
		return nil, err
	}

	return inputs, nil
}

func updateUI() {
	if lastStats == nil {
		return
	}
	updateSystemMetrics()
	updateQueue()
	updateHarvesters()
	updateInputs()
	updateModules()
}

func addMetricRow(table *tview.Table, row int, label, value string, color tcell.Color) {
	table.SetCell(row, 0, tview.NewTableCell(label).SetTextColor(tcell.ColorWhite))
	table.SetCell(row, 1, tview.NewTableCell(value).SetTextColor(color))
}

func createQueuePanel() *tview.TextView {
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetTitle(" Pipeline Queue ").SetBorder(true)
	view.SetText("[green]0/0 [white]| [gray]....................")
	return view
}

func createHarvesterChart() *tview.TextView {
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetTitle(" Harvesters ").SetBorder(true)
	view.SetText("Active: 0 | Open Files: 0")
	return view
}
func createInputsTable() *tview.Table {
	table := tview.NewTable().SetBorders(true)
	table.SetTitle(" Inputs ").SetBorder(true)
	headers := []string{"Type", "Active", "Events", "Throughput", "Files"}
	for col, h := range headers {
		table.SetCell(0, col, tview.NewTableCell(h).SetTextColor(tcell.ColorYellow).SetAlign(tview.AlignCenter))
	}
	return table
}

func createModulesWidget() *tview.List {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetTitle(" Modules ").SetBorder(true)
	list.AddItem("Loading...", "", 0, nil)
	return list
}

func getPrimitiveFromPage(pageName string) tview.Primitive {
	if primitive, exists := pageMap[pageName]; exists {
		return primitive
	}
	return nil
}

func updateSystemMetrics() {
	if mainPage := getPrimitiveFromPage("main"); mainPage != nil {
		if flex, ok := mainPage.(*tview.Flex); ok {
			panel := flex.GetItem(1).(*tview.Flex).GetItem(0).(*tview.Flex).GetItem(0).(*tview.Table)
			if lastStats != nil {
				// CPU
				totalMs := lastStats.Beat.CPU.Total.Time.MS
				cpuPercent := float64(totalMs) / float64(lastStats.Beat.Info.Uptime.MS) * 100

				// Memoria
				rssMB := float64(lastStats.Beat.Memstats.RSS) / 1024 / 1024

				// Uptime
				uptime := time.Duration(lastStats.Beat.Info.Uptime.MS) * time.Millisecond

				// Load Average
				load1 := lastStats.System.Load.Norm.Load1
				load5 := lastStats.System.Load.Norm.Load5
				load15 := lastStats.System.Load.Norm.Load15

				panel.GetCell(0, 1).SetText(fmt.Sprintf("%.1f%%", cpuPercent))
				panel.GetCell(1, 1).SetText(fmt.Sprintf("%.1f MB", rssMB))
				panel.GetCell(2, 1).SetText(fmt.Sprintf("%v", uptime.Truncate(time.Minute)))
				panel.GetCell(3, 1).SetText(fmt.Sprintf("%.2f %.2f %.2f", load1, load5, load15))
			}
		}
	}
}

func updateHarvesters() {
	if mainPage := getPrimitiveFromPage("main"); mainPage != nil {
		if flex, ok := mainPage.(*tview.Flex); ok {
			view := flex.GetItem(1).(*tview.Flex).GetItem(0).(*tview.Flex).GetItem(2).(*tview.TextView)

			if lastStats != nil {
				harvester := lastStats.Filebeat.Harvester // Correcto: Harvester (singular)
				view.SetText(fmt.Sprintf("Active: %d | Open Files: %d", harvester.Running, harvester.Open))
			} else {
				view.SetText("Active: 0 | Open Files: 0")
			}
		}
	}
}
func updateQueue() {
	if mainPage := getPrimitiveFromPage("main"); mainPage != nil {
		if flex, ok := mainPage.(*tview.Flex); ok {
			view := flex.GetItem(1).(*tview.Flex).GetItem(0).(*tview.Flex).GetItem(1).(*tview.TextView)

			if lastStats != nil {
				queue := lastStats.Libbeat.Pipeline
				percent := 0.0
				if queue.Queue.MaxEvents > 0 { // Correcto: MaxEvents
					percent = float64(queue.Queue.Filled.Events) / float64(queue.Queue.MaxEvents) * 100 // Correcto: Filled.Events
				}

				bars := int(percent / 5)
				if bars < 0 {
					bars = 0
				}

				view.Clear()
				fmt.Fprintf(view, "[green]%d/%d [white]| %s", queue.Queue.Filled.Events, queue.Queue.MaxEvents, strings.Repeat("█", bars)) // Correcto
			} else {
				view.SetText("[green]0/0 [white]| [gray]....................")
			}
		}
	}
}

func updateInputs() {
	if mainPage := getPrimitiveFromPage("main"); mainPage != nil {
		if flex, ok := mainPage.(*tview.Flex); ok {
			// Accede a la tabla a través de la jerarquía conocida
			if table, ok := flex.GetItem(1).(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(0).(*tview.Table); ok {

				// Limpia las filas previas
				for row := 1; row < table.GetRowCount(); row++ {
					table.RemoveRow(row)
				}

				// Actualiza los inputs
				if lastStats != nil {
					for i, input := range lastStats.Filebeat.Inputs {
						table.SetCell(i+1, 0, tview.NewTableCell(input.Type).SetTextColor(tcell.ColorWhite))
						table.SetCell(i+1, 1, tview.NewTableCell(fmt.Sprintf("%t", input.Active)).SetTextColor(tcell.ColorWhite))
						table.SetCell(i+1, 2, tview.NewTableCell(fmt.Sprintf("%d", input.Events)).SetTextColor(tcell.ColorWhite))
						table.SetCell(i+1, 3, tview.NewTableCell(fmt.Sprintf("%.2f", input.Throughput.Bytes)).SetTextColor(tcell.ColorWhite))
						table.SetCell(i+1, 4, tview.NewTableCell(fmt.Sprintf("%d", input.Files)).SetTextColor(tcell.ColorWhite))
					}
				}
			}
		}
	}
}

func updateModules() {
	if mainPage := getPrimitiveFromPage("main"); mainPage != nil {
		if flex, ok := mainPage.(*tview.Flex); ok {
			list := flex.GetItem(1).(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.List)

			list.Clear()
			if lastStats != nil {
				for _, module := range lastStats.Filebeat.Modules.List {
					status := "[red]✗"
					if module.Enabled {
						status = "[green]✓"
					}
					list.AddItem(fmt.Sprintf("%s %s (%d errors)", status, module.Name, module.Errors), "", 0, nil)
				}
			}
		}
	}
}
