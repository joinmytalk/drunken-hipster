package main

import (
	"bufio"
	"flag"
	"fmt"
	goconf "github.com/akrennmair/goconf"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

type Backend struct {
	Name          string
	ConnectString string
}

type Frontend struct {
	Name         string
	BindString   string
	HTTPS        bool
	AddForwarded bool
	Hosts        []string
	Backends     []string
	//AddHeader    struct { Key string; Value string }
	KeyFile  string
	CertFile string
}

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
	Transport    *http.Transport
	Frontend     *Frontend
	HostBackends map[string]chan *Backend
	Backends     chan *Backend
}

func (h *RequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//log.Printf("incoming request: %#v", *r)
	r.RequestURI = ""
	r.URL.Scheme = "http"

	if h.Frontend.AddForwarded {
		remote_addr := r.RemoteAddr
		idx := strings.LastIndex(remote_addr, ":")
		if idx != -1 {
			remote_addr = remote_addr[0:idx]
			if remote_addr[0] == '[' && remote_addr[len(remote_addr)-1] == ']' {
				remote_addr = remote_addr[1:len(remote_addr)-1]
			}
		}
		r.Header.Add("X-Forwarded-For", remote_addr)
	}

	if len(h.Frontend.Hosts) == 0 {
		backend := <-h.Backends
		r.URL.Host = backend.ConnectString
		h.Backends <- backend
	} else {
		backend_list := h.HostBackends[r.Host]
		if backend_list == nil {
			if len(h.Frontend.Backends) == 0 {
				http.Error(w, "no suitable backend found for request", http.StatusServiceUnavailable)
				return
			} else {
				backend := <-h.Backends
				r.URL.Host = backend.ConnectString
				h.Backends <- backend
			}
		} else {
			backend := <-backend_list
			r.URL.Host = backend.ConnectString
			backend_list <- backend
		}
	}

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
			upgrade_websocket = (strings.ToLower(upgrade_hdrs[0]) == "websocket")
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

		conn2, err := net.Dial("tcp", r.URL.Host)
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

func usage() {
	fmt.Fprintf(os.Stdout, "usage: %s -config=<configfile>\n", os.Args[0])
	os.Exit(1)
}

func main() {
	var cfgfile *string = flag.String("config", "", "configuration file")

	backends := make(map[string]*Backend)
	hosts := make(map[string][]*Backend)
	frontends := make(map[string]*Frontend)

	flag.Parse()

	if *cfgfile == "" {
		usage()
	}

	cfg, err := goconf.ReadConfigFile(*cfgfile)
	if err != nil {
		log.Printf("opening %s failed: %v", *cfgfile, err)
		os.Exit(1)
	}

	// first, extract backends
	for _, section := range cfg.GetSections() {
		if strings.HasPrefix(section, "backend ") {
			tokens := strings.Split(section, " ")
			if len(tokens) < 2 {
				log.Printf("backend section has no name, ignoring.")
				continue
			}
			connect_str, _ := cfg.GetString(section, "connect")
			if connect_str == "" {
				log.Printf("empty connect string for backend %s, ignoring.", tokens[1])
				continue
			}
			b := &Backend{Name: tokens[1], ConnectString: connect_str}
			backends[b.Name] = b
		}
	}

	// then extract hosts
	for _, section := range cfg.GetSections() {
		if strings.HasPrefix(section, "host ") {
			tokens := strings.Split(section, " ")
			if len(tokens) < 2 {
				log.Printf("host section has no name, ignoring.")
				continue
			}
			backends_str, _ := cfg.GetString(section, "backends")
			backends_list := strings.Split(backends_str, " ")
			if len(backends_list) == 0 {
				log.Printf("host %s has no backends, ignoring.", tokens[1])
				continue
			}
			for _, host := range tokens[1:] {
				backends_for_host := []*Backend{}
				for _, backend := range backends_list {
					b := backends[backend]
					if b == nil {
						log.Printf("backend %s doesn't exist, ignoring.", backend)
					}
					backends_for_host = append(backends_for_host, b)
				}
				hosts[host] = backends_for_host
			}
		}
	}

	// and finally, extract frontends
	for _, section := range cfg.GetSections() {
		if strings.HasPrefix(section, "frontend ") {
			tokens := strings.Split(section, " ")
			if len(tokens) < 2 {
				log.Printf("frontend section has no name, ignoring.")
				continue
			}

			frontend_name := tokens[1]

			frontend := &Frontend{}
			frontend.Name = frontend_name
			frontend.BindString, err = cfg.GetString(section, "bind")
			if err != nil {
				log.Printf("error while getting [%s]bind: %v, ignoring.", section, err)
				continue
			}
			if frontend.BindString == "" {
				log.Printf("frontend %s has no bind argument, ignoring.", frontend_name)
				continue
			}

			frontend.HTTPS, err = cfg.GetBool(section, "https")
			if err != nil {
				frontend.HTTPS = false
			}

			if frontend.HTTPS {
				frontend.KeyFile, err = cfg.GetString(section, "keyfile")
				if err != nil {
					log.Printf("error while getting[%s]keyfile: %v, ignoring.", section, err)
					continue
				}
				if frontend.KeyFile == "" {
					log.Printf("frontend %s has HTTPS enabled but no keyfile, ignoring.", frontend_name)
					continue
				}

				frontend.CertFile, err = cfg.GetString(section, "certfile")
				if err != nil {
					log.Printf("error while getting[%s]certfile: %v, ignoring.", section, err)
					continue
				}
				if frontend.CertFile == "" {
					log.Printf("frontend %s has HTTPS enabled but no certfile, ignoring.", frontend_name)
					continue
				}
			}

			frontend_hosts, err := cfg.GetString(section, "hosts")
			if err == nil && frontend_hosts != "" {
				frontend.Hosts = strings.Split(frontend_hosts, " ")
			}

			frontend_backends, err := cfg.GetString(section, "backends")
			if err == nil && frontend_backends != "" {
				frontend.Backends = strings.Split(frontend_backends, " ")
			}

			frontend.AddForwarded, _ = cfg.GetBool(section, "add-x-forwarded-for")

			if len(frontend.Backends) == 0 && len(frontend.Hosts) == 0 {
				log.Printf("frontend %s has neither backends nor hosts configured, ignoring.", frontend_name)
				continue
			}

			frontends[frontend_name] = frontend
		}
	}

	count := 0
	exit_chan := make(chan int)
	for name, frontend := range frontends {
		log.Printf("Starting frontend %s...", name)
		go func(fe *Frontend) {
			fe.Start(hosts, backends)
			exit_chan <- 1
		}(frontend)
		count++
	}

	// this shouldn't return
	for i := 0 ; i < count; i++ {
		<-exit_chan
	}
}

func (f *Frontend) Start(hosts map[string][]*Backend, backends map[string]*Backend) {
	mux := http.NewServeMux()

	hosts_chans := make(map[string]chan *Backend)

	for _, h := range f.Hosts {
		host_chan := make(chan *Backend, len(hosts[h]))
		for _, b := range hosts[h] {
			host_chan <- b
		}
		hosts_chans[h] = host_chan
	}

	backends_chan := make(chan *Backend, len(f.Backends))

	for _, b := range f.Backends {
		backends_chan <- backends[b]
	}

	mux.Handle("/", &RequestHandler{Transport: &http.Transport{DisableKeepAlives: false, DisableCompression: false}, Frontend: f, HostBackends: hosts_chans, Backends: backends_chan})

	srv := &http.Server{Handler: mux, Addr: f.BindString}

	if f.HTTPS {
		if err := srv.ListenAndServeTLS(f.CertFile, f.KeyFile); err != nil {
			log.Printf("Starting HTTPS frontend %s failed: %v", f.Name, err)
		}
	} else {
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("Starting frontend %s failed: %v", f.Name, err)
		}
	}
}
