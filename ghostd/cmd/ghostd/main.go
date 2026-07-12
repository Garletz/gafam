package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Garletz/gafam/ghostd/internal/adb"
	"github.com/Garletz/gafam/ghostd/internal/api"
	"github.com/Garletz/gafam/ghostd/internal/llm"
	"github.com/Garletz/gafam/ghostd/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("ghostd starting...")

	cfg := loadConfig()

	st, err := store.New(cfg.StorePath)
	if err != nil {
		log.Fatalf("Failed to init store: %v", err)
	}
	defer st.Close()

	llmClient := llm.NewClient(cfg.LLMEndpoint, cfg.LLMModel)
	agent := llm.NewAgent(llmClient, st)

	adbConn := adb.NewConnector(cfg.ADBHost, cfg.ADBPort, cfg.DeviceSerial)
	if err := adbConn.Connect(); err != nil {
		log.Printf("ADB connection failed (will retry): %v", err)
	}
	go adbConn.ReconnectLoop(30 * time.Second)

	go adbConn.StreamLogs(func(line string) {
		st.AppendLog(line)
		agent.Feed(line)
	})

	go agent.Run(context.Background())

	apiHandler := api.NewRouter(st, adbConn, agent)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: apiHandler,
	}

	go func() {
		log.Printf("ghostd API listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server.Shutdown(ctx)
	adbConn.Close()
}

type config struct {
	Port         string
	ADBHost      string
	ADBPort      string
	DeviceSerial string
	StorePath    string
	LLMEndpoint  string
	LLMModel     string
}

func loadConfig() config {
	getEnv := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	return config{
		Port:         getEnv("GHOSTD_PORT", "8080"),
		ADBHost:      getEnv("ADB_HOST", "127.0.0.1"),
		ADBPort:      getEnv("ADB_PORT", "5037"),
		DeviceSerial: getEnv("DEVICE_SERIAL", ""),
		StorePath:    getEnv("STORE_PATH", "/app/data/ghostd.sqlite"),
		LLMEndpoint:  getEnv("LLM_ENDPOINT", "http://127.0.0.1:8081"),
		LLMModel:     getEnv("LLM_MODEL", "Qwen3-0.6B-Q4_K_M"),
	}
}
