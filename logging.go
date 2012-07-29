package main

import (
	"log"
	"net/http"
	"net"
	"bufio"
	"errors"
)

type RequestLogger struct {
	handler http.Handler
	logger  log.Logger
}

type LogResponseWriter struct {
	RW       http.ResponseWriter
	RespCode int
	Size     int
}

func (w *LogResponseWriter) Header() http.Header {
	return w.RW.Header()
}

func (w *LogResponseWriter) Write(data []byte) (s int, err error) {
	s, err = w.RW.Write(data)
	w.Size += s
	return
}

func (w *LogResponseWriter) WriteHeader(r int) {
	w.RW.WriteHeader(r)
	w.RespCode = r
}

func (w *LogResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.RW.(http.Hijacker)
	if ok {
		return hijacker.Hijack()
	}
	return nil, nil, errors.New("webserver doesn't support hijacking")
}

func NewRequestLogger(h http.Handler, l log.Logger) *RequestLogger {
	return &RequestLogger{handler: h, logger: l}
}

func (h *RequestLogger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	request_uri := r.RequestURI
	lrw := &LogResponseWriter{RW: w}
	h.handler.ServeHTTP(lrw, r)
	if lrw.RespCode == 0 {
		lrw.RespCode = 200
	}
	host := "-"
	if r.Host != "" {
		host = r.Host
	}
	h.logger.Printf("%s %s \"%s %s %s\" %d %d", r.RemoteAddr, host, r.Method, request_uri, r.Proto, lrw.RespCode, lrw.Size)
}
