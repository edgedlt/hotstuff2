// Demo is a web-based visualization of HotStuff-2 consensus.
//
// Run with:
//
//	go run demo/main.go
//
// Then open http://localhost:8080 in your browser.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/edgedlt/hotstuff2/demo/server"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP server address")
	staticDir := flag.String("static", "", "Static files directory (default: demo/web)")
	flag.Parse()

	// Determine static directory
	webDir := *staticDir
	if webDir == "" {
		// Try to find the web directory relative to the executable or working directory
		candidates := []string{
			"demo/web",
			"web",
			filepath.Join(filepath.Dir(os.Args[0]), "web"),
			filepath.Join(filepath.Dir(os.Args[0]), "../web"),
		}

		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				webDir = candidate
				break
			}
		}

		if webDir == "" {
			webDir = "demo/web"
		}
	}

	log.Printf("Serving static files from: %s", webDir)

	srv := server.New(webDir)
	if err := srv.ListenAndServe(*addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
