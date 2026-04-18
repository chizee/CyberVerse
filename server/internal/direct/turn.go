package direct

import (
	"fmt"
	"net"

	"github.com/pion/turn/v4"
)

// TURNServer is an embedded TURN server that listens on TCP.
// Browser clients connect via turn:<host>:<port>?transport=tcp.
type TURNServer struct {
	server   *turn.Server
	listener net.Listener
	port     int
	publicIP string
	realm    string
	username string
	password string
}

// NewTURNServer creates an embedded TURN server listening on the given TCP port.
// publicIP is the relay address advertised to clients.
func NewTURNServer(port int, publicIP, realm, username, password string) (*TURNServer, error) {
	listener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen TURN tcp :%d: %w", port, err)
	}

	authKey := turn.GenerateAuthKey(username, realm, password)
	authHandler := func(u string, r string, srcAddr net.Addr) ([]byte, bool) {
		if u == username {
			return authKey, true
		}
		return nil, false
	}

	s, err := turn.NewServer(turn.ServerConfig{
		Realm:       realm,
		AuthHandler: authHandler,
		ListenerConfigs: []turn.ListenerConfig{
			{
				Listener: listener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: net.ParseIP(publicIP),
					Address:      "0.0.0.0",
				},
			},
		},
	})
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("create TURN server: %w", err)
	}

	return &TURNServer{
		server: s, listener: listener, port: port,
		publicIP: publicIP, realm: realm,
		username: username, password: password,
	}, nil
}

// Close shuts down the TURN server.
func (ts *TURNServer) Close() error {
	if ts.server != nil {
		return ts.server.Close()
	}
	return nil
}

// ICEServerConfig returns the ICE server configuration for browser clients.
// host is the address the browser uses to reach this TURN server (e.g. 127.0.0.1 via SSH tunnel).
func (ts *TURNServer) ICEServerConfig(host string) map[string]any {
	return map[string]any{
		"urls":       []string{fmt.Sprintf("turn:%s:%d?transport=tcp", host, ts.port)},
		"username":   ts.username,
		"credential": ts.password,
	}
}
