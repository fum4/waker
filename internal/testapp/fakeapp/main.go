// Command fakeapp serves testapp's endpoints; the end-to-end test starts it
// as an app's start command.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/fum4/waker/internal/testapp"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:3000", "address to serve on")
	flag.Parse()
	log.Fatal(http.ListenAndServe(*listen, testapp.Handler()))
}
