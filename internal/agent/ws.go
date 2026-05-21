package agent

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

// connectWS dials the control plane log stream. Must be called with a.wsMu held.
func (a *Agent) connectWS() error {
	nodeID := viper.GetString(config.KeyNodeID)
	controlURL := viper.GetString(config.KeyControlURL)
	wsURL := toWebSocketURL(controlURL) + "/api/infra/logs/stream"

	header := http.Header{}
	header.Add("X-Node-ID", nodeID)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		log.Printf("[logs] ws connection failed: %v", err)
		return err
	}
	log.Printf("[logs] connected to control plane: %s", wsURL)
	a.wsConn = conn
	return nil
}

// toWebSocketURL converts http(s) scheme to ws(s).
func toWebSocketURL(u string) string {
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + u[len("https://"):]
	case strings.HasPrefix(u, "http://"):
		return "ws://" + u[len("http://"):]
	default:
		return u
	}
}
