package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// GopeeEncPrefix - encoding prefix used while encoding urls
const GopeeEncPrefix = "xox"

// Pre-compile RegEx
var reBase = regexp.MustCompile(`base +href="(.*?)"`)
var reHTML = regexp.MustCompile(`\saction=["']?(.*?)["'\s]|\shref=["']?(.*?)["'\s]|\ssrc=["']?(.*?)["'\s]`)
var reCSS = regexp.MustCompile(`url\(["']?(.*?)["']?\)`)

var reBase64 = regexp.MustCompile("^(?:[A-Za-z0-9-_]{4})*(?:[A-Za-z0-9-_]{2}==|[A-Za-z0-9-_]{3}=)?$")

// Hop-by-hop headers
var hopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                true,
	"trailers":          true,
	"transfer-encoding": true,
	"upgrade":           true,
}

// Headers that create problem handling response
// TODO: support gzip compressed response in future
var skipHeaders = map[string]bool{
	"content-security-policy":             true, // sent in response
	"content-security-policy-report-only": true, // sent in response
	"accept-encoding":                     true, // sent in request
	"cookie":                              true, // sent in request
}

type proxyManager struct {
	req  *http.Request
	uri  *url.URL
	resp *http.Response
}

var sessionManager *Manager

func encodeURL(plainURL []byte) string {
	return GopeeEncPrefix + base64.URLEncoding.EncodeToString(plainURL)
}

func decodeURL(encodedURL string) (decodedURI *url.URL, err error) {
	encodedURL = strings.TrimSpace(encodedURL)
	if encodedURL == "" || !strings.HasPrefix(encodedURL, GopeeEncPrefix) {
		err = errors.New("invalid path supplied to decode")
		return nil, err
	}
	encodedURL = strings.TrimLeft(encodedURL, GopeeEncPrefix)
	decoded, err := base64.URLEncoding.DecodeString(encodedURL)
	if err != nil {
		log.Println(err.Error(), " for url ", encodedURL)
		return nil, err
	}
	decodedURI, err = url.Parse(string(decoded))
	if err != nil {
		log.Println("error parsing", string(decoded))
		return nil, err
	}
	return decodedURI, err
}

// ProxyRequest creates a new request to be proxied
func ProxyRequest(r *http.Request, w http.ResponseWriter) {
	var uri *url.URL

	// Path can be of the form
	// /base64-encoded-string
	// /home/ajax.php
	// http://thirdparty.com/resource.js
	path := r.URL.Path[1:]

	if decodedURL, err := decodeURL(path); err == nil {
		// URL rewritten by GoPee
		uri = decodedURL
	} else {
		// Might be a plain-text URL which was not rewritten
		// AJAX request ?
		path += "?" + r.URL.RawQuery
		pathURI, err := url.Parse(path)
		if err != nil {
			log.Println("error parsing", path)
		}
		if pathURI.IsAbs() {
			uri = pathURI
		} else {
			referer, err := url.Parse(r.Referer())
			if err != nil {
				log.Println("error parsing", r.Referer())
			}
			if baseURL, err := decodeURL(referer.Path[1:]); err == nil {
				uri = baseURL.ResolveReference(pathURI)
			}
		}
	}
	// log.Println(uri.String())
	if uri == nil {
		// return a 404
		http.NotFound(w, r)
	} else {
		// try fetching the url
		proxyMan := &proxyManager{r, uri, nil}
		proxyMan.Fetch(w)
	}
}

// Fetch makes the actual request to server and writes data with rewritten URLs to response
func (pm *proxyManager) Fetch(w http.ResponseWriter) {
	if pm.uri == nil {
		http.Error(w, "No URI specified to fetch", http.StatusBadRequest)
		return
	}

	// Get the http client assigned to this session
	// If a session does not exist or is expired, create a new session
	httpClient, err := sessionManager.Start(w, pm.req)

	if err != nil {
		http.Error(w, "Unable to start session", http.StatusInternalServerError)
		return
	}
	req, _ := http.NewRequest(pm.req.Method, pm.uri.String(), pm.req.Body)
	// Forward request headers to server
	copyHeader(req.Header, pm.req.Header)

	// Set http client protocol version
	req.Proto = "HTTP/1.1"
	req.ProtoMajor = 1
	req.ProtoMinor = 1

	pm.resp, err = httpClient.Do(req)
	if err != nil {
		log.Println("error fetching", pm.uri.String(), err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer pm.resp.Body.Close()

	// In case there was a url redirect
	// http -> https, non-www -> www, login page
	if pm.uri.String() != pm.resp.Request.URL.String() {
		pm.uri = pm.resp.Request.URL
		http.Redirect(w, pm.req, "/"+encodeURL([]byte(pm.uri.String())), 302)
		return
	}

	contentType := pm.resp.Header.Get("Content-Type")

	// Forward response headers to client
	copyHeader(w.Header(), pm.resp.Header)

	w.WriteHeader(pm.resp.StatusCode)

	// Rewrite all urls
	if strings.Contains(contentType, "text/html") {
		pm.rewriteHTML(w)
	} else if strings.Contains(contentType, "text/css") {
		pm.rewriteCSS(w)
	} else {
		io.Copy(w, pm.resp.Body)
	}
}

func copyHeader(dst, src http.Header) {
	// Copy Headers from src to dst
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	// Remove hop-by-hop headers and problem headers
	for h, _ := range hopHeaders {
		dst.Del(h)
	}
	for h, _ := range skipHeaders {
		dst.Del(h)
	}
}

func (pm *proxyManager) rewriteHTML(w http.ResponseWriter) {
	body, _ := ioutil.ReadAll(pm.resp.Body)
	// if there's a <base href> specified in the document
	// use that as base to encode all URLs in the page
	baseHrefMatch := reBase.FindSubmatch(body)
	if len(baseHrefMatch) > 0 {
		var err error
		pm.uri, err = url.Parse(string(baseHrefMatch[1][:]))
		if err != nil {
			log.Println("Error Parsing " + string(baseHrefMatch[1][:]))
		}
	}
	encodedBody := reHTML.ReplaceAllFunc(body, func(s []byte) []byte {
		parts := reHTML.FindSubmatchIndex(s)
		if parts != nil {
			// replace src attribute
			srcIndex := parts[2:4]
			if srcIndex[0] != -1 {
				return pm.rewriteURI(s, srcIndex[0], srcIndex[1])
			}

			// replace href attribute
			hrefIndex := parts[4:6]
			if hrefIndex[0] != -1 {
				return pm.rewriteURI(s, hrefIndex[0], hrefIndex[1])
			}

			// replace form action attribute
			actionIndex := parts[6:8]
			if actionIndex[0] != -1 {
				return pm.rewriteURI(s, actionIndex[0], actionIndex[1])
			}
		}
		return s
	})
	w.Write(encodedBody)
}

func (pm *proxyManager) rewriteCSS(w http.ResponseWriter) {
	body, _ := ioutil.ReadAll(pm.resp.Body)
	encodedBody := reCSS.ReplaceAllFunc(body, func(s []byte) []byte {
		parts := reCSS.FindSubmatchIndex(s)
		if parts != nil {
			// replace url attribute in css
			pathIndex := parts[2:4]
			if pathIndex[0] != -1 {
				return pm.rewriteURI(s, pathIndex[0], pathIndex[1])
			}
		}
		return s
	})
	w.Write(encodedBody)

}

func (pm *proxyManager) rewriteURI(src []byte, start int, end int) []byte {
	relURL := string(src[start:end])
	// keep anchor and javascript links intact
	if relURL == "" || strings.HasPrefix(relURL, "#") || strings.HasPrefix(relURL, "javascript") || strings.HasPrefix(relURL, "data") {
		return src
	}
	// Check if url is relative and make it absolute
	if strings.Index(relURL, "http") != 0 {
		relPath, err := url.Parse(relURL)
		if err != nil {
			return src
		}
		absURL := pm.uri.ResolveReference(relPath).String()
		src = bytes.Replace(src, []byte(relURL), []byte(absURL), -1)
		end = start + len(absURL)
	}
	encodedString := encodeURL(src[start:end])
	return bytes.Replace(src, src[start:end], []byte(encodedString), -1)
}

// Cache templates
var templates = template.Must(template.ParseFiles("home.html"))

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path[1:] == "" {
		r.ParseForm()
		enteredURL := r.FormValue("url")
		if enteredURL != "" {
			// Check if url attribute is set in GET / POST
			uri, _ := url.Parse(enteredURL)
			// prepend http if not specified
			if uri.Scheme == "" {
				uri.Scheme = "http"
			}
			http.Redirect(w, r, "/"+encodeURL([]byte(uri.String())), 302)
			return
		}
		templates.ExecuteTemplate(w, "home.html", nil)
	} else {
		ProxyRequest(r, w)
	}
}

func main() {
	httpHost := os.Getenv("HOST")
	httpPort := os.Getenv("PORT")
	if httpPort == "" {
		httpPort = "8080"
	}

	sessionManager = NewManager("gopee", 600) // client session expiry set to 600s (10mins)
	go sessionManager.GC()

	http.HandleFunc("/", homeHandler)

	http.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, r.URL.Path[1:])
	})

	log.Printf("web proxy listening on %s:%s\n", httpHost, httpPort)

	http.ListenAndServe(httpHost+":"+httpPort, nil)
}
