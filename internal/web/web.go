// Package web serves the local operator dashboard (no CDN).
package web

import (
	"context"
	"crypto/subtle"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
)

const pageTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Nighthawk</title>
<style>
body{font-family:system-ui,sans-serif;margin:1.5rem;background:#111;color:#eee}
h1{font-size:1.4rem}
canvas{background:#222;border:1px solid #444}
form{margin-top:1rem}
</style>
</head>
<body>
<h1>Nighthawk</h1>
<p>socket={{.Socket}}</p>
<canvas id="bars" width="400" height="120"></canvas>
<form method="POST" action="/approve">
<input type="hidden" name="csrf" value="{{.CSRF}}">
<input type="hidden" name="id" value="">
<button type="submit">Approve</button>
</form>
<script>
function drawBars(data){
  var c=document.getElementById('bars');
  if(!c)return;
  var ctx=c.getContext('2d');
  ctx.clearRect(0,0,c.width,c.height);
  if(!data||!data.length)return;
  var max=0;for(var i=0;i<data.length;i++){if(data[i]>max)max=data[i];}
  if(max<=0)max=1;
  var w=c.width/data.length;
  for(var i=0;i<data.length;i++){
    var h=(data[i]/max)*(c.height-4);
    ctx.fillStyle='#6cf';
    ctx.fillRect(i*w+2,c.height-h,w-4,h);
  }
}
drawBars([1,3,2,5,4]);
</script>
</body>
</html>`

// NewMux returns the HTTP handler. token gates GET; csrf gates POST /approve.
func NewMux(socket, token, csrf string) http.Handler {
	tmpl := template.Must(template.New("page").Parse(pageTmpl))
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if !tokenOK(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tmpl.Execute(w, map[string]string{"Socket": socket, "CSRF": csrf})
	})
	mux.HandleFunc("/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if !tokenOK(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Form.Get("csrf")), []byte(csrf)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// CSRF passed; IPC to daemon may fail.
		http.Error(w, "ipc unavailable", http.StatusBadGateway)
	})
	return mux
}

func tokenOK(r *http.Request, token string) bool {
	q := r.URL.Query().Get("token")
	if q == "" {
		q = r.Header.Get("X-Nighthawk-Token")
	}
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(q), []byte(token)) == 1
}

// Serve listens on addr. Non-loopback addresses require allowRemote.
func Serve(ctx context.Context, addr, socket, token string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if !allowRemote && !isLoopback(host) {
		return fmt.Errorf("refusing non-loopback addr %q without allowRemote", addr)
	}
	csrf := token // same secret material is fine for local dashboard
	srv := &http.Server{Addr: addr, Handler: NewMux(socket, token, csrf)}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err = srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func isLoopback(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
