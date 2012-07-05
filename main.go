package main

import (
	"net/http"
	"net"
	"log"
	"io"
	"os"
	"fmt"
	"bufio"
)

func Copy(dest *bufio.ReadWriter, src *bufio.ReadWriter) {
	buf := make([]byte, 40*1024)
	for {
		n, err := src.Read(buf)
		if err != nil && err != io.EOF {
			log.Printf("Read failed: %v", err)
			return
		}
		if n == 0 {
			return
		}
		dest.Write(buf[0:n])
		dest.Flush()
	}
}

func CopyBidir(conn1 io.ReadWriteCloser, rw1 *bufio.ReadWriter, conn2 io.ReadWriteCloser, rw2 *bufio.ReadWriter) {
	finished := make(chan bool)

	go func() {
		Copy(rw2, rw1)
		conn2.Close()
		finished <- true
	}()
	go func() {
		Copy(rw1, rw2)
		conn1.Close()
		finished <- true
	}()

	<-finished
	<-finished
}

type RequestHandler struct {
	Transport *http.Transport
}

func (h *RequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//log.Printf("incoming request: %#v", *r)

	r.RequestURI = ""
	r.URL.Scheme = "http"
	r.URL.Host = "127.0.0.1:8000"

	conn_hdr := ""
	conn_hdrs := r.Header["Connection"]
	log.Printf("Connection headers: %v", conn_hdrs)
	if len(conn_hdrs) > 0 {
		conn_hdr = conn_hdrs[0]
	}

	upgrade_websocket := false
	if conn_hdr == "Upgrade" {
		log.Printf("got Connection: Upgrade")
		upgrade_hdrs := r.Header["Upgrade"]
		log.Printf("Upgrade headers: %v", upgrade_hdrs)
		if len(upgrade_hdrs) > 0 {
			upgrade_websocket = (upgrade_hdrs[0] == "websocket")
		}
	}

	if upgrade_websocket {
		hj, ok := w.(http.Hijacker)

		if !ok {
			http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
			return
		}

		conn, bufrw, err := hj.Hijack()
		defer conn.Close()

		conn2, err := net.Dial("tcp", "127.0.0.1:8000")
		if err != nil {
			http.Error(w, "couldn't connect to backend server", http.StatusServiceUnavailable)
			return
		}
		defer conn2.Close()

		err = r.Write(conn2)
		if err != nil {
			log.Printf("writing WebSocket request to backend server failed: %v", err)
			return
		}

		CopyBidir(conn, bufrw, conn2, bufio.NewReadWriter(bufio.NewReader(conn2), bufio.NewWriter(conn2)))

	} else {

		resp, err := h.Transport.RoundTrip(r)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "Error: %v", err)
			return
		}

		for k, v := range resp.Header {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}

		w.WriteHeader(resp.StatusCode)

		io.Copy(w, resp.Body)
		resp.Body.Close()
	}
}

func main() {
	listen_addr := ":9000"

	mux := http.NewServeMux()
	mux.Handle("/", &RequestHandler{Transport:&http.Transport{DisableKeepAlives:false,DisableCompression:false}})

	srv := &http.Server{Handler: mux, Addr: listen_addr}

	err := srv.ListenAndServe()

	if err != nil {
		log.Printf("ListenAndServe: %v", err)
		os.Exit(1)
	}
}
