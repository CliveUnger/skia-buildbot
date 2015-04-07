// logserver_proxy is an application that serves up content from the CT master
// and its 100 workers, giving access to logs w/o needing to SSH into the
// server.
package main

import (
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"strings"

	"go.skia.org/infra/ct/go/util"
)

var (
	port          = flag.String("port", ":10116", "The port that the logserver proxy will run on (e.g., ':10116')")
	logserverPort = flag.String("logserver_port", ":10115", "The port that logserver runs on (e.g., ':10115')")
)

func main() {

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<pre>\n")
		fmt.Fprintf(w, "<h2>Cluster Telemetry Logs</h2>")

		fmt.Fprintf(w, "\n<b>Master Logs</b>\n\n")
		fmt.Fprintf(w, "<a href='%s/'>%s</a>\n\n", util.Master, template.HTMLEscapeString(util.Master))

		fmt.Fprintf(w, "\n<b>Slave Logs</b>\n\n")
		for _, hostname := range util.Slaves {
			fmt.Fprintf(w, "<a href='%s/'>%s</a>\n", hostname, template.HTMLEscapeString(hostname))
		}

		fmt.Fprintf(w, "</pre>\n")
	})

	http.Handle(fmt.Sprintf("/%s/", util.Master), getReverseProxy(util.Master))
	for _, hostname := range util.Slaves {
		http.Handle(fmt.Sprintf("/%s/", hostname), getReverseProxy(hostname))
	}

	if err := http.ListenAndServe(*port, nil); err != nil {
		panic(err)
	}
}

func getReverseProxy(hostname string) *httputil.ReverseProxy {
	director := func(req *http.Request) {
		req.URL.Scheme = "http"
		req.Host = fmt.Sprintf("%s%s", hostname, *logserverPort)
		req.URL.Host = req.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, fmt.Sprintf("/%s", hostname))
	}
	return &httputil.ReverseProxy{Director: director}
}
