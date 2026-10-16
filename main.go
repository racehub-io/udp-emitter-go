// main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"udp-emitter-go/pkg/proto/racehub" // Import the generated package

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

// Add this at the beginning of the file, after the imports and before the ProtoParser struct

type Emitter struct {
	upgrader    websocket.Upgrader
	clients     map[*websocket.Conn]bool
	message     *racehub.Telemetry
	messageType string
	frequency   time.Duration
	stopChan    chan struct{}
	mutex       sync.Mutex
	isRunning   bool
	server      *http.Server
}

func NewEmitter() *Emitter {
	return &Emitter{
		stopChan:  make(chan struct{}),
		frequency: 100 * time.Millisecond, // default frequency
		isRunning: false,
		clients:   make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all connections for testing
			},
			EnableCompression: true,
		},
	}
}

func (e *Emitter) Start(address string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.isRunning {
		return fmt.Errorf("emitter is already running")
	}

	// Parse the address
	u, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("invalid address: %v", err)
	}

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", e.handleWebSocket)

	e.server = &http.Server{
		Addr:    u.Host,
		Handler: mux,
	}

	e.isRunning = true
	e.stopChan = make(chan struct{})

	// Start the server in a goroutine
	go func() {
		if err := e.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("WebSocket server error: %v", err)
		}
	}()

	// Start the emission loop
	go e.emitLoop()
	return nil
}

func (e *Emitter) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := e.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	e.mutex.Lock()
	e.clients[conn] = true
	e.mutex.Unlock()

	// Remove client when connection closes
	go func() {
		<-r.Context().Done()
		e.mutex.Lock()
		delete(e.clients, conn)
		e.mutex.Unlock()
		conn.Close()
	}()
}

func (e *Emitter) Stop() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if !e.isRunning {
		return
	}

	close(e.stopChan)

	// Close all client connections
	for client := range e.clients {
		client.Close()
	}
	e.clients = make(map[*websocket.Conn]bool)

	// Shutdown the server
	if e.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e.server.Shutdown(ctx)
	}

	e.isRunning = false
}

func (e *Emitter) emitLoop() {
	ticker := time.NewTicker(e.frequency)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.mutex.Lock()
			if e.message != nil {
				fmt.Printf("Message: %v", e.message)
				data, err := proto.Marshal(e.message)
				if err != nil {
					log.Printf("Error marshaling message: %v", err)
					e.mutex.Unlock()
					continue
				}

				// Broadcast to all connected clients
				for client := range e.clients {
					err = client.WriteMessage(websocket.BinaryMessage, data)
					if err != nil {
						log.Printf("Error sending WebSocket message: %v", err)
						client.Close()
						delete(e.clients, client)
					}
				}
			}
			e.mutex.Unlock()
		}
	}
}

func (e *Emitter) SetFrequency(freq time.Duration) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.frequency = freq
}

func (e *Emitter) SetMessage(msg *racehub.Telemetry) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.message = msg
}

func (e *Emitter) IsRunning() bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.isRunning
}

// Add this struct to store parameter selections
type Parameter struct {
	name  string
	value float32
}

// Helper function to get all fields from any struct
func getStructFields(structType reflect.Type) []string {
	var fields []string

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields and protobuf internal fields
		if !field.IsExported() || field.Name == "state" || field.Name == "sizeCache" || field.Name == "unknownFields" {
			continue
		}

		fields = append(fields, field.Name)
	}

	sort.Strings(fields)
	return fields
}

// Helper function to get all fields from Car struct
func getCarFields() []string {
	return getStructFields(reflect.TypeOf(racehub.Car{}))
}

// Helper function to get all fields from Participant struct
func getParticipantFields() []string {
	return getStructFields(reflect.TypeOf(racehub.Participant{}))
}

// Helper function to get all fields from Session struct
func getSessionFields() []string {
	return getStructFields(reflect.TypeOf(racehub.Session{}))
}

// Helper function to create parameter controls
func createParameterControls(
	paramNames []string,
	activeParams map[string]interface{},
	orderedParams []string,
	parameterSetters map[string]func(interface{}),
	fieldTypes map[string]reflect.Kind,
	window fyne.Window,
) (*widget.Select, *widget.Entry, *widget.Button, *widget.Table, *fyne.Container) {
	// Parameter selection
	paramSelect := widget.NewSelect(paramNames, nil)
	paramSelect.PlaceHolder = "Type or select parameter"

	valueEntry := widget.NewEntry()
	valueEntry.SetPlaceHolder("Enter value")

	// Create table for showing active parameters
	table := widget.NewTable(
		func() (int, int) {
			rows := len(activeParams)
			if rows < 10 {
				rows = 10
			}
			return rows, 3
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				container.NewStack(widget.NewLabel("Parameter")),
				container.NewStack(widget.NewLabel("Value")),
				container.NewStack(widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)),
			)
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {})

	table.UpdateCell = func(i widget.TableCellID, o fyne.CanvasObject) {
		container := o.(*fyne.Container)
		stackParameter := container.Objects[0].(*fyne.Container)
		stackValue := container.Objects[1].(*fyne.Container)
		stackButtons := container.Objects[2].(*fyne.Container)

		labelParameter := stackParameter.Objects[0].(*widget.Label)
		labelValue := stackValue.Objects[0].(*widget.Label)
		removeButton := stackButtons.Objects[0].(*widget.Button)

		labelParameter.Hide()
		labelValue.Hide()
		removeButton.Hide()

		stackParameter.Resize(fyne.NewSize(3000, 10))

		if i.Row < len(orderedParams) {
			paramName := orderedParams[i.Row]
			switch i.Col {
			case 0:
				labelParameter.Show()
				labelParameter.SetText(paramName)
				labelParameter.Resize(fyne.NewSize(0, 0))
			case 1:
				labelValue.Show()
				// Format value based on type
				switch v := activeParams[paramName].(type) {
				case float32, float64:
					labelValue.SetText(fmt.Sprintf("%.2f", v))
				case int, int32, int64, uint32, uint64:
					labelValue.SetText(fmt.Sprintf("%d", v))
				case string:
					labelValue.SetText(v)
				case bool:
					labelValue.SetText(fmt.Sprintf("%v", v))
				default:
					labelValue.SetText(fmt.Sprintf("%v", v))
				}
				labelValue.Resize(fyne.NewSize(0, 0))
			case 2:
				removeButton.Show()
				removeButton.OnTapped = func() {
					for i, param := range orderedParams {
						if param == paramName {
							orderedParams = append(orderedParams[:i], orderedParams[i+1:]...)
							break
						}
					}
					delete(activeParams, paramName)
					parameterSetters[paramName](nil) // Reset value
					table.Refresh()
				}
			}
		}
	}

	table.UpdateHeader = func(id widget.TableCellID, template fyne.CanvasObject) {
		container := template.(*fyne.Container)
		label := container.Objects[0].(*widget.Label)
		button := container.Objects[1].(*widget.Button)

		label.Show()
		button.Hide()

		text := ""
		switch id.Col {
		case 0:
			text = "Parameter"
		case 1:
			text = "Value"
		case 2:
			text = "Actions"
		}
		label.SetText(text)
	}

	tableContainer := container.NewVScroll(table)
	tableContainer.SetMinSize(fyne.NewSize(600, 400))

	// Add parameter button
	addParam := widget.NewButton("Add Parameter", func() {
		selected := paramSelect.Selected
		if selected == "" {
			dialog.ShowError(fmt.Errorf("please select a parameter"), window)
			return
		}
		if _, exists := parameterSetters[selected]; !exists {
			dialog.ShowError(fmt.Errorf("invalid parameter: %s", selected), window)
			return
		}

		// Parse value based on field type
		var value interface{}
		var err error
		
		switch fieldTypes[selected] {
		case reflect.Float32, reflect.Float64:
			value, err = strconv.ParseFloat(valueEntry.Text, 64)
			if err == nil {
				value = float32(value.(float64))
			}
		case reflect.Int32, reflect.Int64, reflect.Int:
			value, err = strconv.ParseInt(valueEntry.Text, 10, 64)
			if err == nil {
				value = int32(value.(int64))
			}
		case reflect.Uint32, reflect.Uint64:
			value, err = strconv.ParseUint(valueEntry.Text, 10, 64)
		case reflect.String:
			value = valueEntry.Text
			err = nil
		case reflect.Bool:
			value, err = strconv.ParseBool(valueEntry.Text)
		default:
			err = fmt.Errorf("unsupported type for parameter %s", selected)
		}

		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid value for %s: %v", selected, err), window)
			return
		}

		if _, exists := activeParams[selected]; !exists {
			orderedParams = append(orderedParams, selected)
		}
		activeParams[selected] = value
		parameterSetters[selected](value)
		table.Refresh()

		paramSelect.SetSelected("")
		valueEntry.SetText("")
	})

	paramControls := container.NewVBox(
		paramSelect,
		valueEntry,
		container.NewHBox(addParam),
		widget.NewLabel("Active Parameters"),
		tableContainer,
	)

	return paramSelect, valueEntry, addParam, table, paramControls
}

func setParameter(field reflect.Value, v interface{}) {
    if v == nil {
        field.Set(reflect.Zero(field.Type()))
        return
    }
    switch field.Kind() {
    case reflect.Float32:
        field.SetFloat(float64(v.(float32)))
    case reflect.Int32:
        field.SetInt(int64(v.(int32)))
    case reflect.Uint64:
        field.SetUint(v.(uint64))
    case reflect.String:
        field.SetString(v.(string))
    case reflect.Bool:
        field.SetBool(v.(bool))
    }
}

func main() {
	myApp := app.New()
	window := myApp.NewWindow("UDP Car Telemetry Emitter")

	// Create emitter and data structures
	emitter := NewEmitter()
	packet := &racehub.Telemetry{}
	car := &racehub.Car{}
	session := &racehub.Session{}
	participant := &racehub.Participant{}

	packet.Cars = append(packet.Cars, car)
	packet.Participants = append(packet.Participants, participant)
	packet.Session = session

	// Create maps and slices for each struct type
	carParams := make(map[string]interface{})
	participantParams := make(map[string]interface{})
	sessionParams := make(map[string]interface{})

	// Create maps to store field types
	carFieldTypes := make(map[string]reflect.Kind)
	participantFieldTypes := make(map[string]reflect.Kind)
	sessionFieldTypes := make(map[string]reflect.Kind)

	var orderedCarParams []string
	var orderedParticipantParams []string
	var orderedSessionParams []string

	// Create parameter setters for each struct
	setCarParameters := make(map[string]func(interface{}))
	carValue := reflect.ValueOf(car).Elem()
	for _, name := range getCarFields() {
		field := carValue.FieldByName(name)
		if field.IsValid() && field.CanSet() {
			carFieldTypes[name] = field.Kind()
			setCarParameters[name] = func(v interface{}) {
				setParameter(field, v)
			}
		}
	}

	setParticipantParameters := make(map[string]func(interface{}))
	participantValue := reflect.ValueOf(participant).Elem()
	for _, name := range getParticipantFields() {
		field := participantValue.FieldByName(name)
		if field.IsValid() && field.CanSet() {
			participantFieldTypes[name] = field.Kind()
			setParticipantParameters[name] = func(v interface{}) {
				setParameter(field, v)
			}
		}
	}

	setSessionParameters := make(map[string]func(interface{}))
	sessionValue := reflect.ValueOf(session).Elem()
	for _, name := range getSessionFields() {
		field := sessionValue.FieldByName(name)
		if field.IsValid() && field.CanSet() {
			sessionFieldTypes[name] = field.Kind()
			setSessionParameters[name] = func(v interface{}) {
				setParameter(field, v)
			}
		}
	}

	// Create parameter controls for each type
	_, _, _, _, carControls := createParameterControls(
		getCarFields(),
		carParams,
		orderedCarParams,
		setCarParameters,
		carFieldTypes,
		window,
	)

	_, _, _, _, participantControls := createParameterControls(
		getParticipantFields(),
		participantParams,
		orderedParticipantParams,
		setParticipantParameters,
		participantFieldTypes,
		window,
	)

	_, _, _, _, sessionControls := createParameterControls(
		getSessionFields(),
		sessionParams,
		orderedSessionParams,
		setSessionParameters,
		sessionFieldTypes,
		window,
	)

	// Create tabs
	tabs := container.NewAppTabs(
		container.NewTabItem("Car", carControls),
		container.NewTabItem("Participant", participantControls),
		container.NewTabItem("Session", sessionControls),
	)

	// UDP Configuration
	addressEntry := widget.NewEntry()
	addressEntry.SetText("ws://localhost:8082")

	frequencyEntry := widget.NewEntry()
	frequencyEntry.SetText("1000")

	// Control buttons
	var startButton *widget.Button
	var stopButton *widget.Button

	startButton = widget.NewButton("Start", func() {
		freq, err := strconv.Atoi(frequencyEntry.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid frequency: %v", err), window)
			return
		}

		car.Timestamp = uint64(time.Now().UnixNano() / int64(time.Millisecond))
		emitter.SetMessage(packet)
		emitter.SetFrequency(time.Duration(freq) * time.Millisecond)

		if err := emitter.Start(addressEntry.Text); err != nil {
			dialog.ShowError(err, window)
			return
		}

		log.Printf("Connection started at %s.", addressEntry.Text)
		startButton.Disable()
		stopButton.Enable()
	})

	stopButton = widget.NewButton("Stop", func() {
		emitter.Stop()
		log.Printf("Connection stopped.")
		startButton.Enable()
		stopButton.Disable()
	})
	stopButton.Disable()

	configBox := container.NewVBox(
		widget.NewLabel("UDP Configuration"),
		&widget.Form{
			Items: []*widget.FormItem{
				{Text: "Address:", Widget: addressEntry},
				{Text: "Frequency (ms):", Widget: frequencyEntry},
			},
		},
		container.NewHBox(startButton, stopButton),
	)

	content := container.NewVBox(
		tabs,
		configBox,
	)

	window.SetContent(content)
	window.Resize(fyne.NewSize(600, 900))
	window.ShowAndRun()
}
