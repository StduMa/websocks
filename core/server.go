package core

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/juju/loggo"
)

type Server struct {
	LogLevel   loggo.Level
	Pattern    string
	ListenAddr string
	TLS        bool
	CertPath   string
	KeyPath    string
	Proxy      string

	Upgrader *websocket.Upgrader

	CreatedAt time.Time

	Opened     uint64
	Closed     uint64
	Uploaded   uint64
	Downloaded uint64
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	c, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Debugf(err.Error())
		return
	}

	ws := &WebSocket{
		conn: c,
	}

	atomic.AddUint64(&server.Opened, 1)
	defer atomic.AddUint64(&server.Closed, 1)

	host := r.Header.Get("WebSocks-Host")
	logger.Debugf("Dial %s", host)

	conn, err := net.Dial("tcp", host)
	if err != nil {
		if err != nil {
			logger.Debugf(err.Error())
		}
		return
	}
	defer conn.Close()

	go func() {
		downloaded, err := io.Copy(conn, ws)
		atomic.AddUint64(&server.Downloaded, uint64(downloaded))
		if err != nil {
			logger.Debugf(err.Error())
			return
		}
	}()

	uploaded, err := io.Copy(ws, conn)
	atomic.AddUint64(&server.Uploaded, uint64(uploaded))
	if err != nil {
		logger.Debugf(err.Error())
		return
	}
	return
}

func (server *Server) Status(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf("%ds: opened %d, closed %d, uploaded %d bytes, downloaded %d bytes", int(time.Since(server.CreatedAt).Seconds()), server.Opened, server.Closed, server.Uploaded, server.Downloaded)))
}

func (server *Server) Listen() (err error) {
	logger.SetLogLevel(server.LogLevel)

	go func() {
		for {
			time.Sleep(time.Second)
			logger.Debugf("%ds: opened %d, closed %d, uploaded %d bytes, downloaded %d bytes", int(time.Since(server.CreatedAt).Seconds()), server.Opened, server.Closed, server.Uploaded, server.Downloaded)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc(server.Pattern, server.HandleWebSocket)
	mux.HandleFunc("/status", server.Status)
	if server.Proxy != "" {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			remote, err := url.Parse(server.Proxy)
			if err != nil {
				panic(err)
			}
			proxy := httputil.NewSingleHostReverseProxy(remote)
			proxy.ServeHTTP(w, r)
		})
	}

	s := http.Server{
		Addr:    server.ListenAddr,
		Handler: mux,
	}

	if !server.TLS {
		err = s.ListenAndServe()
		if err != nil {
			return err
		}
		return
	} else {
		err = s.ListenAndServeTLS(server.CertPath, server.KeyPath)
		if err != nil {
			return err
		}
	}

	return
}
