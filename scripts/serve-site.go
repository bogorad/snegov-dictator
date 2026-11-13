// A local, static preview server. No application backend is needed.
// Run from the repository root: go run scripts/serve-site.go
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	root := flag.String("root", "site", "static website directory")
	addr := flag.String("addr", "127.0.0.1:4173", "loopback address and port")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatal(err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		log.Fatal("preview address must be a loopback IP address")
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(absolute, "index.html")); err != nil {
		log.Fatal("build the website first: ", err)
	}
	files := http.FileServer(http.Dir(absolute))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, component := range strings.Split(r.URL.Path, "/") {
			if strings.HasPrefix(component, ".") {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
	fmt.Printf("Static preview: http://%s/ (serving %s)\n", *addr, absolute)
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
