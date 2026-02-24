package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

var (
	addr       = flag.String("addr", ":5000", "http service address")
	mqttBroker = flag.String("mqtt", "tcp://localhost:1883", "MQTT broker URL")
	dbPath     = flag.String("db", "data.db", "SQLite DB path")
)

type Reading struct {
	Topic        string   `json:"topic"`
	Payload      string   `json:"payload,omitempty"`
	Device       string   `json:"device,omitempty"`
	Temp         *float64 `json:"temp,omitempty"`
	Hum          *float64 `json:"hum,omitempty"`
	Fire         *bool    `json:"fire,omitempty"`
	Motion       *bool    `json:"motion,omitempty"`
	Relay        *bool    `json:"relay,omitempty"`
	Door         *bool    `json:"door,omitempty"`
	Led1         *bool    `json:"led1,omitempty"`
	Led2         *bool    `json:"led2,omitempty"`
	Led3         *bool    `json:"led3,omitempty"`
	Led4         *bool    `json:"led4,omitempty"`
	AlertType    string   `json:"alertType,omitempty"`
	AlertMessage string   `json:"alertMessage,omitempty"`
	Ts           string   `json:"ts"`
}

type MotionLog struct {
	ID        int    `json:"id"`
	Device    string `json:"device"`
	EventType string `json:"event_type"`
	Message   string `json:"message"`
	Ts        string `json:"ts"`
}

type DeviceStatus struct {
	Device     string   `json:"device"`
	Online     bool     `json:"online"`
	LastUpdate string   `json:"last_update"`
	Temp       *float64 `json:"temp,omitempty"`
	Hum        *float64 `json:"hum,omitempty"`
	Relay      *bool    `json:"relay,omitempty"`
	Door       *bool    `json:"door,omitempty"`
	Led1       *bool    `json:"led1,omitempty"`
	Led2       *bool    `json:"led2,omitempty"`
	Led3       *bool    `json:"led3,omitempty"`
	Led4       *bool    `json:"led4,omitempty"`
}

type StatusMessage struct {
	Type           string         `json:"type"`
	ServerOnline   bool           `json:"server_online,omitempty"`
	MQTTConnected  bool           `json:"mqtt_connected,omitempty"`
	DeviceStatuses []DeviceStatus `json:"device_statuses,omitempty"`
	Timestamp      string         `json:"timestamp"`
}

type Server struct {
	db             *sql.DB
	mqttCli        mqtt.Client
	mu             sync.Mutex
	clients        map[*websocket.Conn]bool
	upgr           websocket.Upgrader
	rateLimiter    *RateLimiter
	deviceStatuses map[string]*DeviceStatus
	deviceMu       sync.Mutex
}

func NewServer() *Server {
	s := &Server{
		clients:        make(map[*websocket.Conn]bool),
		deviceStatuses: make(map[string]*DeviceStatus),
		rateLimiter:    NewRateLimiter(),
		upgr: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	return s
}

func (s *Server) initDB(path string) error {
	// If DATABASE_URL is provided use Postgres, otherwise fallback to sqlite
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err := sql.Open("postgres", url)
		if err != nil {
			return err
		}
		s.db = db
		// Create tables for Postgres (separate statements for proper ordering)
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			is_verified BOOLEAN DEFAULT FALSE,
			verify_token TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT now()
		)`)
		if err != nil {
			return fmt.Errorf("create users: %w", err)
		}
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
			id SERIAL PRIMARY KEY,
			user_id INTEGER REFERENCES users(id) NOT NULL,
			token TEXT UNIQUE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL
		)`)
		if err != nil {
			return fmt.Errorf("create sessions: %w", err)
		}
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS readings (
			id SERIAL PRIMARY KEY,
			topic TEXT NOT NULL,
			payload TEXT,
			ts TIMESTAMP WITH TIME ZONE NOT NULL,
			device TEXT,
			temp REAL,
			hum REAL,
			user_id INTEGER REFERENCES users(id)
		)`)
		if err != nil {
			return fmt.Errorf("create readings: %w", err)
		}
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS motion_logs (
			id SERIAL PRIMARY KEY,
			device TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT,
			ts TIMESTAMP WITH TIME ZONE NOT NULL
		)`)
		if err != nil {
			return fmt.Errorf("create motion_logs: %w", err)
		}
		return nil
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	s.db = db
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		is_verified INTEGER DEFAULT 0,
		verify_token TEXT,
		created_at TEXT
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		token TEXT UNIQUE NOT NULL,
		created_at TEXT,
		expires_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS readings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		topic TEXT NOT NULL,
		payload TEXT,
		ts TEXT NOT NULL,
		device TEXT,
		temp REAL,
		hum REAL,
		user_id INTEGER
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS motion_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device TEXT NOT NULL,
		event_type TEXT NOT NULL,
		message TEXT,
		ts TEXT NOT NULL
	)`)
	return err
}

func (s *Server) saveReading(topic, payload string) (*Reading, error) {
	ts := time.Now().UTC().Format(time.RFC3339)
	var device string
	var temp *float64
	var hum *float64
	var fire *bool
	var motion *bool
	var relay *bool
	var door *bool
	var led1 *bool
	var led2 *bool
	var led3 *bool
	var led4 *bool
	var alertType string
	var alertMessage string

	// try parse JSON payload for structured fields
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &m); err == nil {
		if v, ok := m["device"].(string); ok {
			device = v
		}
		if v, ok := m["temp"].(float64); ok {
			temp = &v
		}
		if v, ok := m["hum"].(float64); ok {
			hum = &v
		}
		if v, ok := m["fire"].(bool); ok {
			fire = &v
		}
		if v, ok := m["motion"].(bool); ok {
			motion = &v
		}
		if v, ok := m["relay"].(bool); ok {
			relay = &v
		}
		if v, ok := m["door"].(bool); ok {
			door = &v
		}
		if v, ok := m["led1"].(bool); ok {
			led1 = &v
		}
		if v, ok := m["led2"].(bool); ok {
			led2 = &v
		}
		if v, ok := m["led3"].(bool); ok {
			led3 = &v
		}
		if v, ok := m["led4"].(bool); ok {
			led4 = &v
		}
		if v, ok := m["type"].(string); ok {
			alertType = v
		}
		if v, ok := m["message"].(string); ok {
			alertMessage = v
		}
	}

	var err error
	if os.Getenv("DATABASE_URL") != "" {
		_, err = s.db.Exec("INSERT INTO readings (topic, payload, ts, device, temp, hum) VALUES ($1, $2, $3, $4, $5, $6)", topic, payload, ts, device, temp, hum)
	} else {
		_, err = s.db.Exec("INSERT INTO readings (topic, payload, ts, device, temp, hum) VALUES (?, ?, ?, ?, ?, ?)", topic, payload, ts, device, temp, hum)
	}
	if err != nil {
		return nil, err
	}
	return &Reading{
		Topic:        topic,
		Payload:      payload,
		Device:       device,
		Temp:         temp,
		Hum:          hum,
		Fire:         fire,
		Motion:       motion,
		Relay:        relay,
		Door:         door,
		Led1:         led1,
		Led2:         led2,
		Led3:         led3,
		Led4:         led4,
		AlertType:    alertType,
		AlertMessage: alertMessage,
		Ts:           ts,
	}, nil
}

func (s *Server) broadcast(r *Reading) {
	b, _ := json.Marshal(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
			c.Close()
			delete(s.clients, c)
		}
	}
}

func (s *Server) broadcastStatus() {
	s.deviceMu.Lock()
	statuses := make([]DeviceStatus, 0)
	for _, ds := range s.deviceStatuses {
		statuses = append(statuses, *ds)
	}
	s.deviceMu.Unlock()

	msg := StatusMessage{
		Type:           "status",
		ServerOnline:   true,
		MQTTConnected:  s.mqttCli != nil && s.mqttCli.IsConnected(),
		DeviceStatuses: statuses,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
			c.Close()
			delete(s.clients, c)
		}
	}
}

// sendTelegram sends a simple message to the configured Telegram chat if env vars are set.
func (s *Server) sendTelegram(text string) error {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chat := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chat == "" {
		// not configured
		return nil
	}
	urlStr := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	data := url.Values{}
	data.Set("chat_id", chat)
	data.Set("text", text)
	resp, err := http.PostForm(urlStr, data)
	if err != nil {
		log.Println("telegram post error:", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("telegram send non-200:", resp.Status)
	}
	return nil
}

// sendEmail sends a basic plain-text email using SMTP (MailHog compatible).
func (s *Server) sendEmail(subject, body string) error {
	smtpHost := os.Getenv("SMTP_HOST") // e.g. mailhog:1025
	from := os.Getenv("SMTP_FROM")
	to := os.Getenv("ALERT_EMAIL")
	if to == "" {
		to = from
	}
	if smtpHost == "" || from == "" || to == "" {
		return nil
	}
	msg := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
		"\r\n" + body
	if err := smtp.SendMail(smtpHost, nil, from, []string{to}, []byte(msg)); err != nil {
		log.Println("sendEmail error:", err)
		return err
	}
	return nil
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgr.Upgrade(w, r, nil)
	if err != nil {
		log.Println("ws upgrade error:", err)
		return
	}
	log.Printf("ws connected: %s", conn.RemoteAddr())
	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	// keep connection open until closed by client
	for {
		if _, _, err := conn.NextReader(); err != nil {
			s.mu.Lock()
			delete(s.clients, conn)
			s.mu.Unlock()
			log.Printf("ws disconnected: %s", conn.RemoteAddr())
			conn.Close()
			break
		}
	}
}

func (s *Server) serveStatic() {
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
}

func (s *Server) apiPublish(w http.ResponseWriter, r *http.Request) {
	// 🔐 Verify Bearer token
	authToken := r.Header.Get("Authorization")
	if len(authToken) > 7 && authToken[:7] == "Bearer " {
		authToken = authToken[7:]
	}
	userID := s.getUserFromToken(authToken)
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var p struct {
		Topic   string `json:"topic"`
		Payload string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if s.mqttCli == nil || !s.mqttCli.IsConnected() {
		http.Error(w, "mqtt client not connected", http.StatusServiceUnavailable)
		return
	}
	token := s.mqttCli.Publish(p.Topic, 0, false, p.Payload)
	token.Wait()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "topic": p.Topic})
}

func (s *Server) apiReadings(w http.ResponseWriter, r *http.Request) {
	// 🔐 Verify Bearer token
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}
	userID := s.getUserFromToken(token)
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query().Get("limit")
	limit := 100
	if q != "" {
		fmt.Sscan(q, &limit)
	}
	query := fmt.Sprintf("SELECT topic, payload, ts, device, temp, hum FROM readings ORDER BY id DESC LIMIT %d", limit)
	rows, err := s.db.Query(query)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := make([]Reading, 0)
	for rows.Next() {
		var t, p, ts, device string
		var temp sql.NullFloat64
		var hum sql.NullFloat64
		if err := rows.Scan(&t, &p, &ts, &device, &temp, &hum); err == nil {
			var tf *float64
			var hf *float64
			if temp.Valid {
				tf = &temp.Float64
			}
			if hum.Valid {
				hf = &hum.Float64
			}
			out = append(out, Reading{Topic: t, Payload: p, Device: device, Temp: tf, Hum: hf, Ts: ts})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) saveMotionLog(device, eventType, message string) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	var err error
	if os.Getenv("DATABASE_URL") != "" {
		_, err = s.db.Exec("INSERT INTO motion_logs (device, event_type, message, ts) VALUES ($1, $2, $3, $4)", device, eventType, message, ts)
	} else {
		_, err = s.db.Exec("INSERT INTO motion_logs (device, event_type, message, ts) VALUES (?, ?, ?, ?)", device, eventType, message, ts)
	}
	return err
}

func (s *Server) apiMotionLogs(w http.ResponseWriter, r *http.Request) {
	// 🔐 Verify Bearer token
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}
	userID := s.getUserFromToken(token)
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query().Get("limit")
	limit := 100
	if q != "" {
		fmt.Sscan(q, &limit)
	}
	query := fmt.Sprintf("SELECT id, device, event_type, message, ts FROM motion_logs ORDER BY id DESC LIMIT %d", limit)
	rows, err := s.db.Query(query)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := make([]MotionLog, 0)
	for rows.Next() {
		var ml MotionLog
		if err := rows.Scan(&ml.ID, &ml.Device, &ml.EventType, &ml.Message, &ml.Ts); err == nil {
			out = append(out, ml)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) mqttOnMessage(client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := string(msg.Payload())
	log.Printf("mqtt received topic=%s payload=%s", topic, payload)
	rd, err := s.saveReading(topic, payload)
	if err != nil {
		log.Println("save reading error:", err)
		return
	}

	// Update device status and notify on relay/door changes
	if rd.Device != "" {
		s.deviceMu.Lock()
		prev := s.deviceStatuses[rd.Device]

		// set new status
		s.deviceStatuses[rd.Device] = &DeviceStatus{
			Device:     rd.Device,
			Online:     true,
			LastUpdate: rd.Ts,
			Temp:       rd.Temp,
			Hum:        rd.Hum,
			Relay:      rd.Relay,
			Door:       rd.Door,
			Led1:       rd.Led1,
			Led2:       rd.Led2,
			Led3:       rd.Led3,
			Led4:       rd.Led4,
		}
		s.deviceMu.Unlock()

		// Compare previous state and send Telegram notifications for changes
		if prev != nil {
			// Relay change
			if prev.Relay == nil && rd.Relay != nil {
				if *rd.Relay {
					s.sendTelegram(fmt.Sprintf("[%s] Relay turned ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Relay ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: Relay turned ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] Relay turned OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Relay OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: Relay turned OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			} else if prev.Relay != nil && rd.Relay != nil && *prev.Relay != *rd.Relay {
				if *rd.Relay {
					s.sendTelegram(fmt.Sprintf("[%s] Relay turned ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Relay ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: Relay turned ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] Relay turned OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Relay OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: Relay turned OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			}

			// Door change
			if prev.Door == nil && rd.Door != nil {
				if *rd.Door {
					s.sendTelegram(fmt.Sprintf("[%s] Door OPENED", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Door OPENED", rd.Device), fmt.Sprintf("Device: %s\nEvent: Door OPENED\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] Door CLOSED", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Door CLOSED", rd.Device), fmt.Sprintf("Device: %s\nEvent: Door CLOSED\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			} else if prev.Door != nil && rd.Door != nil && *prev.Door != *rd.Door {
				if *rd.Door {
					s.sendTelegram(fmt.Sprintf("[%s] Door OPENED", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Door OPENED", rd.Device), fmt.Sprintf("Device: %s\nEvent: Door OPENED\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] Door CLOSED", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] Door CLOSED", rd.Device), fmt.Sprintf("Device: %s\nEvent: Door CLOSED\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			}

			// LED changes (1-4)
			if prev.Led1 == nil && rd.Led1 != nil {
				if *rd.Led1 {
					s.sendTelegram(fmt.Sprintf("[%s] LED1 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED1 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED1 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED1 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED1 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED1 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			} else if prev.Led1 != nil && rd.Led1 != nil && *prev.Led1 != *rd.Led1 {
				if *rd.Led1 {
					s.sendTelegram(fmt.Sprintf("[%s] LED1 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED1 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED1 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED1 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED1 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED1 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			}
			if prev.Led2 == nil && rd.Led2 != nil {
				if *rd.Led2 {
					s.sendTelegram(fmt.Sprintf("[%s] LED2 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED2 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED2 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED2 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED2 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED2 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			} else if prev.Led2 != nil && rd.Led2 != nil && *prev.Led2 != *rd.Led2 {
				if *rd.Led2 {
					s.sendTelegram(fmt.Sprintf("[%s] LED2 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED2 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED2 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED2 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED2 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED2 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			}
			if prev.Led3 == nil && rd.Led3 != nil {
				if *rd.Led3 {
					s.sendTelegram(fmt.Sprintf("[%s] LED3 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED3 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED3 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED3 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED3 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED3 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			} else if prev.Led3 != nil && rd.Led3 != nil && *prev.Led3 != *rd.Led3 {
				if *rd.Led3 {
					s.sendTelegram(fmt.Sprintf("[%s] LED3 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED3 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED3 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED3 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED3 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED3 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			}
			if prev.Led4 == nil && rd.Led4 != nil {
				if *rd.Led4 {
					s.sendTelegram(fmt.Sprintf("[%s] LED4 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED4 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED4 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED4 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED4 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED4 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			} else if prev.Led4 != nil && rd.Led4 != nil && *prev.Led4 != *rd.Led4 {
				if *rd.Led4 {
					s.sendTelegram(fmt.Sprintf("[%s] LED4 ON", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED4 ON", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED4 ON\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] LED4 OFF", rd.Device))
					s.sendEmail(fmt.Sprintf("[%s] LED4 OFF", rd.Device), fmt.Sprintf("Device: %s\nEvent: LED4 OFF\nTime: %s\nPayload: %s", rd.Device, rd.Ts, payload))
				}
			}
		} else {
			// No previous status: optionally announce initial states
			if rd.Relay != nil {
				if *rd.Relay {
					s.sendTelegram(fmt.Sprintf("[%s] Relay is ON", rd.Device))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] Relay is OFF", rd.Device))
				}
			}
			if rd.Door != nil {
				if *rd.Door {
					s.sendTelegram(fmt.Sprintf("[%s] Door is OPEN", rd.Device))
				} else {
					s.sendTelegram(fmt.Sprintf("[%s] Door is CLOSED", rd.Device))
				}
			}
		}
	}

	// Save motion log if motion detected
	if rd.Motion != nil && *rd.Motion {
		if err := s.saveMotionLog(rd.Device, "motion", "Motion detected - possible intruder"); err != nil {
			log.Println("save motion log error:", err)
		}
	}
	// Save fire log if fire detected
	if rd.Fire != nil && *rd.Fire {
		if err := s.saveMotionLog(rd.Device, "fire", "Fire detected - emergency!"); err != nil {
			log.Println("save fire log error:", err)
		}
	}
	s.broadcast(rd)
	s.broadcastStatus()
}

func (s *Server) startMQTT(brokerURL string) error {
	opts := mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID("iot_demo_server")

	// Set MQTT credentials if provided
	if mqttUser := os.Getenv("MQTT_USER"); mqttUser != "" {
		opts.SetUsername(mqttUser)
	}
	if mqttPass := os.Getenv("MQTT_PASS"); mqttPass != "" {
		opts.SetPassword(mqttPass)
	}

	opts.SetDefaultPublishHandler(s.mqttOnMessage)

	// Retry connecting a few times with backoff to allow broker to become ready
	var cli mqtt.Client
	var err error
	maxAttempts := 10
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cli = mqtt.NewClient(opts)
		token := cli.Connect()
		token.Wait()
		if token.Error() == nil {
			err = nil
			break
		}
		err = token.Error()
		wait := time.Duration(attempt) * time.Second
		log.Printf("mqtt connect attempt %d/%d failed: %v; retrying in %s", attempt, maxAttempts, err, wait)
		time.Sleep(wait)
	}
	if err != nil {
		return err
	}
	s.mqttCli = cli
	if token := cli.Subscribe("sensors/#", 0, nil); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	if token := cli.Subscribe("alerts/#", 0, nil); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	if token := cli.Subscribe("status", 0, nil); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	log.Println("mqtt subscribed to sensors/#, alerts/#, status")
	return nil
}

func main() {
	flag.Parse()
	s := NewServer()
	if err := s.initDB(*dbPath); err != nil {
		log.Fatal("db init:", err)
	}
	if err := s.startMQTT(*mqttBroker); err != nil {
		log.Println("mqtt start failed:", err)
	} else {
		log.Println("connected to mqtt", *mqttBroker)
	}

	s.serveStatic()
	http.HandleFunc("/ws", s.handleWS)
	http.HandleFunc("/api/publish", s.apiPublish)
	// Test endpoint to send a Telegram message (no auth) - use only for quick testing
	http.HandleFunc("/api/test-telegram", func(w http.ResponseWriter, r *http.Request) {
		msg := r.URL.Query().Get("text")
		if msg == "" && r.Method == "POST" {
			var p struct {
				Text string `json:"text"`
			}
			_ = json.NewDecoder(r.Body).Decode(&p)
			msg = p.Text
		}
		if msg == "" {
			http.Error(w, "text required", http.StatusBadRequest)
			return
		}
		if err := s.sendTelegram(msg); err != nil {
			log.Println("test telegram send failed:", err)
			http.Error(w, "send failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	http.HandleFunc("/api/readings", s.apiReadings)
	http.HandleFunc("/api/logs", s.apiMotionLogs)
	// Auth endpoints
	http.HandleFunc("/api/register", s.apiRegister)
	http.HandleFunc("/api/verify", s.apiVerify)
	http.HandleFunc("/api/login", s.apiLogin)
	http.HandleFunc("/api/me", s.apiMe)
	http.HandleFunc("/api/logout", s.apiLogout)

	abs, _ := filepath.Abs("./static")
	log.Printf("serving static from %s on %s\n", abs, *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
