package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// streamLogs runs forever, restarting journalctl after each exit.
// Uses a for-loop rather than recursive goroutine spawning to prevent accumulation.
func (a *Agent) streamLogs() {
	for {
		if err := a.runJournalStream(); err != nil {
			log.Printf("[logs] stream ended: %v, restarting in 5s", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func (a *Agent) runJournalStream() error {
	cmd := exec.Command("journalctl", "-f", "-o", "json", "-n", "0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start journalctl: %w", err)
	}

	log.Println("[logs] started streaming from system journal")

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		rawMsg, ok := entry["MESSAGE"].(string)
		if !ok {
			continue
		}
		payload := make(map[string]interface{})
		if err := json.Unmarshal([]byte(rawMsg), &payload); err != nil {
			payload["message"] = rawMsg
		}
		if unit, ok := entry["_SYSTEMD_UNIT"].(string); ok {
			payload["unit"] = unit
			if _, exists := payload["logger"]; !exists {
				payload["logger"] = strings.TrimSuffix(unit, ".service")
			}
		}
		if priority, ok := entry["PRIORITY"].(string); ok {
			levels := map[string]string{
				"0": "emergency", "1": "alert", "2": "critical", "3": "error",
				"4": "warning", "5": "notice", "6": "info", "7": "debug",
			}
			if level, exists := levels[priority]; exists && payload["level"] == nil {
				payload["level"] = level
			}
		}
		a.sendToControl(payload)
	}

	cmd.Wait()
	return fmt.Errorf("journalctl exited")
}

func (a *Agent) sendToControl(logData interface{}) {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()

	if a.wsConn == nil {
		if err := a.connectWS(); err != nil {
			return
		}
	}

	msgJSON, _ := json.Marshal(logData)
	if err := a.wsConn.WriteMessage(websocket.TextMessage, msgJSON); err != nil {
		log.Printf("[logs] ws write error: %v, reconnecting...", err)
		a.wsConn.Close()
		a.wsConn = nil
	}
}
