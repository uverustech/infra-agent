package agent

import (
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
	"github.com/uverustech/infra-agent/internal/config"
)

// configDir is the path the gateway agent manages via git + Caddy.
const configDir = "/etc/caddy"
const caddyfile = "/etc/caddy/Caddyfile"

// Agent holds all runtime state for the main heartbeat/log loop.
type Agent struct {
	version string

	// Protects lastReloadOK and lastError — written by applyConfig, read by sendHeartbeat.
	reloadMu     sync.RWMutex
	lastReloadOK bool
	lastError    string

	// Protects the WebSocket connection to the control plane.
	wsMu   sync.Mutex
	wsConn *websocket.Conn

	// Prevents concurrent self-update attempts.
	updateRunning atomic.Bool

	httpClient *http.Client
}

func New(v string) *Agent {
	return &Agent{
		version:    v,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Run is the package-level entry point called from the CLI root command.
func Run(v string) {
	New(v).run()
}

func (a *Agent) run() {
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Printf("Config file changed: %s. Re-applying settings...", e.Name)
	})
	viper.WatchConfig()

	nodeID := viper.GetString(config.KeyNodeID)
	if nodeID == "" {
		log.Fatal("node-id is required. Set it permanently with: infra-agent config set node-id <name>\nOr use --node-id once, or set INFRA_NODE_ID environment variable.")
	}

	log.Printf("infra-agent %s starting — node: %s", a.version, nodeID)

	if viper.GetString(config.KeyNodeType) == "gateway" {
		GitPull()
		a.applyConfig()
	}

	go a.streamLogs()

	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		if viper.GetString(config.KeyNodeType) == "gateway" && viper.GetBool(config.KeyAutoPull) {
			GitPull()
			a.applyConfig()
		}
		a.sendHeartbeat()
	}
}
