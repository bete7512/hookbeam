// Command certs generates the local CA, server and client certificates the two planes use for mTLS.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bete7512/hookbeam/pkg/certs"
)

func main() {
	dir := flag.String("dir", "deploy/certs", "output directory")
	hosts := flag.String("hosts", "localhost,127.0.0.1,delivery", "comma-separated names the server certificate is valid for")
	validity := flag.Duration("validity", 365*24*time.Hour, "certificate lifetime")
	force := flag.Bool("force", false, "regenerate even when every file exists")
	flag.Parse()

	if !*force && certs.Paths(*dir).Exist() {
		fmt.Printf("certs: %s already holds a CA, server and client certificate (use -force to regenerate)\n", *dir)
		return
	}
	files, err := certs.Generate(*dir, strings.Split(*hosts, ","), *validity)
	if err != nil {
		fmt.Fprintln(os.Stderr, "certs:", err)
		os.Exit(1)
	}
	fmt.Printf("certs: wrote %s, %s, %s, %s, %s\n", files.CA, files.ServerCert, files.ServerKey, files.ClientCert, files.ClientKey)
}
