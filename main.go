package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

var (
	sourcePort = flag.String("from", "8080", "The port of the original server to proxy requests to")
	proxyPort   = flag.String("to", "3000", "The port to run the proxy server on")
	viaPort  = flag.String("via", "3001", "The port to run the WebSocket server on")
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleWebsocket(clients map[*websocket.Conn]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		clients[conn] = true
	}
}

// injectScript injects the WebSocket JavaScript snippet into the HTML content
func injectScript(html string, port string) string {
    snippet := fmt.Sprintf(`
    <script>
        var ws = new WebSocket("ws://localhost:%s/ws");
        ws.onmessage = function(event) {
            if (event.data === "reload") {
                window.location.reload();
            }
        };
    </script>
    `, port)
    // Inject the script before the closing </body> tag
    return strings.Replace(html, "</body>", snippet+"</body>", 1)
}



func main() {
	flag.Parse()

    snippet := fmt.Sprintf(`
    <script>
        var ws = new WebSocket("ws://localhost:%s/ws");
        ws.onmessage = function(event) {
            if (event.data === "reload") {
                window.location.reload();
            }
        };
    </script>
    `, *viaPort)

	clients := make(map[*websocket.Conn]bool)
	reloadMux := http.NewServeMux()

	reloadMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil { return }
		clients[conn] = true
	})

	reloadMux.HandleFunc("/reload", func(w http.ResponseWriter, r *http.Request) {
		for client := range clients {
			err := client.WriteMessage(websocket.TextMessage, []byte("reload"))
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
	})

	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/", func (w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequest(r.Method, fmt.Sprintf("http://localhost:%s%s", *sourcePort, r.URL.Path), r.Body)
		if err != nil {
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}

		req.Header = r.Header.Clone()

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		// read response bodyBytes
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}

		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			bodyStr := strings.Replace(string(bodyBytes), "</body>", snippet+"</body>", 1)
			bodyBytes = []byte(bodyStr)
		}

		for k, v := range resp.Header {
			w.Header()[k] = v
		}

		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))

		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
	})

	go func() {
		log.Printf("WebSocket server running on :%s\n", *viaPort)
		log.Fatal(http.ListenAndServe(":"+*viaPort, reloadMux))
	}()
    
	go func() {
		log.Printf("Proxy server running on :%s\n", *proxyPort)
		log.Fatal(http.ListenAndServe(":"+*proxyPort, proxyMux))
	}()
    
	<-make(chan struct{})
}
