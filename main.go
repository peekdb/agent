package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
)

var (
	hubURL      = "wss://connect.peekdb.com/agent"
	token       string
	databaseURL string
	connName    string
	db          *sql.DB
)

type Message struct {
	Type   string `json:"type"`
	ID     string `json:"id,omitempty"`
	Token  string `json:"token,omitempty"`
	SQL    string `json:"sql,omitempty"`
	Params []any  `json:"params,omitempty"`
}

type AuthResponse struct {
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type QueryResponse struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Columns []string `json:"columns,omitempty"`
	Rows    [][]any  `json:"rows,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func connectDB() error {
	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return db.Ping()
}

func executeQuery(id, sqlQuery string, params []any) QueryResponse {
	log.Printf("[query:%s] Executing: %s", id, truncate(sqlQuery, 100))
	start := time.Now()

	rows, err := db.Query(sqlQuery, params...)
	if err != nil {
		log.Printf("[query:%s] Error: %v", id, err)
		return QueryResponse{ID: id, Type: "result", Error: err.Error()}
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return QueryResponse{ID: id, Type: "result", Error: err.Error()}
	}

	var results [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return QueryResponse{ID: id, Type: "result", Error: err.Error()}
		}

		// Convert values for JSON serialization
		row := make([]any, len(columns))
		for i, v := range values {
			switch val := v.(type) {
			case []byte:
				row[i] = string(val)
			case time.Time:
				row[i] = val.Format(time.RFC3339)
			default:
				row[i] = val
			}
		}
		results = append(results, row)
	}

	log.Printf("[query:%s] Completed in %v, %d rows", id, time.Since(start), len(results))

	return QueryResponse{
		ID:      id,
		Type:    "result",
		Columns: columns,
		Rows:    results,
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func connect() error {
	log.Printf("Connecting to hub: %s", hubURL)

	conn, _, err := websocket.DefaultDialer.Dial(hubURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	// Send auth
	log.Println("Authenticating...")
	if err := conn.WriteJSON(Message{Type: "auth", Token: token}); err != nil {
		return fmt.Errorf("auth send failed: %w", err)
	}

	// Wait for auth response
	var authResp AuthResponse
	if err := conn.ReadJSON(&authResp); err != nil {
		return fmt.Errorf("auth read failed: %w", err)
	}
	if !authResp.Success {
		return fmt.Errorf("authentication failed: %s", authResp.Error)
	}
	log.Println("✓ Authenticated successfully")
	log.Println("Ready and waiting for queries...")

	// Main loop
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("read failed: %w", err)
		}

		if msg.Type == "query" {
			resp := executeQuery(msg.ID, msg.SQL, msg.Params)
			if err := conn.WriteJSON(resp); err != nil {
				return fmt.Errorf("write failed: %w", err)
			}
		}
	}
}

func main() {
	flag.StringVar(&token, "token", os.Getenv("PEEKDB_TOKEN"), "PeekDB connection token")
	flag.StringVar(&databaseURL, "db", os.Getenv("DATABASE_URL"), "Database connection URL")
	flag.StringVar(&hubURL, "hub", hubURL, "Hub WebSocket URL")
	flag.StringVar(&connName, "name", "", "Connection name (optional)")
	flag.Parse()

	if token == "" {
		log.Fatal("Token required: --token or PEEKDB_TOKEN env")
	}
	if databaseURL == "" {
		log.Fatal("Database URL required: --db or DATABASE_URL env")
	}

	log.Println("PeekDB Agent starting...")
	log.Printf("Hub: %s", hubURL)

	// Connect to database
	log.Println("Connecting to database...")
	if err := connectDB(); err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("✓ Database connected")

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		if db != nil {
			db.Close()
		}
		os.Exit(0)
	}()

	// Connect with reconnect loop
	backoff := time.Second
	for {
		if err := connect(); err != nil {
			log.Printf("Connection error: %v", err)
			log.Printf("Reconnecting in %v...", backoff)
			time.Sleep(backoff)
			// Exponential backoff capped at 60s
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
		} else {
			backoff = time.Second // Reset on successful connection
		}
	}
}
