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

// URLCookie - cookie which stores page url
const URLCookie = "X-GoPee-URL"

// GopeeEncPrefix - encoding prefix used while encoding urls
const GopeeEncPrefix = "xox"

// Pre-compile RegEx
var reBase = regexp.MustCompile(`base +href="(.*?)"`)
var reHTML = regexp.MustCompile(`\saction=["']?(.*?)["'\s]|\shref=["']?(.*?)["'\s]|\ssrc=["']?(.*?)["'\s]`)
var reCSS = regexp.MustCompile(`url(["']?(.*?)["']?)`)

var reBase64 = regexp.MustCompile("^(?:[A-Za-z0-9-_]{4})*(?:[A-Za-z0-9-_]{2}==|[A-Za-z0-9-_]{3}=)?$")

var httpClient = &http.Client{}

type proxyManager struct {
	req  *http.Request
	uri  *url.URL
	resp *http.Response
}

func encodeURL(plainURL string) string {
	return GopeeEncPrefix + base64.URLEncoding.EncodeToString([]byte(plainURL))
}

// ProxyRequest creates a new request to be proxied
func ProxyRequest(r *http.Request, w http.ResponseWriter) {
	var uri *url.URL

	// Path can be of the form
	// /base64-encoded-string
	// /home/ajax.php

	path := strings.TrimSpace(r.URL.Path[1:])

	if strings.HasPrefix(path, GopeeEncPrefix) {
		// URL rewritten by GoPee
		path = strings.TrimLeft(path, GopeeEncPrefix)
		components := strings.Split(path, "/")
		decodedURL, err := base64.URLEncoding.DecodeString(components[0])
		if err != nil {
			log.Println(err.Error(), " for url ", path)
			return
		}
		uri, err = url.Parse(string(decodedURL[:]))
		if err != nil {
			log.Println("Error Parsing " + string(decodedURL[:]))
		}
	} else {
		// Might be a plain-text URL which was not rewritten
		urlCookie, _ := r.Cookie(URLCookie)
		// check if a cookie is set
		if urlCookie != nil {
			baseURL, err := url.Parse(urlCookie.Value)
			if err != nil {
				log.Println("Error Parsing " + urlCookie.Value)
			}
			path += "?" + r.URL.RawQuery
			pathURI, err := url.Parse(path)
			if err != nil {
				log.Println("Error Parsing " + path)
			}
			if pathURI.IsAbs() {
				uri = pathURI
			} else {
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
func (pm *proxyManager) Fetch(w http.ResponseWriter) (err error) {
	if pm.uri == nil {
		return errors.New("No URI specified to fetch")
	}
	// log.Println("Fetch: " + pm.uri.String())
	req, _ := http.NewRequest(pm.req.Method, pm.uri.String(), pm.req.Body)
	req.Header.Set("Content-Type", pm.req.Header.Get("Content-Type"))
	// Set proxy's user agent to that of user's
	req.Header.Set("User-Agent", pm.req.Header.Get("User-Agent"))

	pm.resp, err = httpClient.Do(req)
	if err != nil {
		log.Println("Error Fetching " + pm.uri.String())
		return err
	}
	defer pm.resp.Body.Close()

	contentType := pm.resp.Header.Get("Content-Type")
	pm.forwardHeaders(w)

	// Rewrite all urls
	if strings.Contains(contentType, "text/html") {
		// HTTP is stateless, store the url in cookie to handle
		// AJAX requests for which the URLs were not rewritten
		// And don't set cookies for responses to AJAX requests
		if strings.ToLower(pm.req.Header.Get("X-Requested-With")) != "xmlhttprequest" {
			pm.uri.Fragment = ""
			// log.Println("Cookie URI ", pm.uri)
			cookie := &http.Cookie{Name: URLCookie, Value: pm.uri.String()}
			http.SetCookie(w, cookie)
		}
		pm.rewriteHTML(w)
	} else if strings.Contains(contentType, "text/css") {
		pm.rewriteCSS(w)
	} else {
		io.Copy(w, pm.resp.Body)
	}
	return nil
}

func (pm *proxyManager) forwardHeaders(w http.ResponseWriter) {
	// Write all remote response headers to client
	for headerKey := range pm.resp.Header {
		headerVal := pm.resp.Header.Get(headerKey)
		w.Header().Set(headerKey, headerVal)
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
				return pm.encodeURL(s, srcIndex[0], srcIndex[1])
			}

			// replace href attribute
			hrefIndex := parts[4:6]
			if hrefIndex[0] != -1 {
				return pm.encodeURL(s, hrefIndex[0], hrefIndex[1])
			}

			// replace form action attribute
			actionIndex := parts[6:8]
			if actionIndex[0] != -1 {
				return pm.encodeURL(s, actionIndex[0], actionIndex[1])
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
				return pm.encodeURL(s, pathIndex[0], pathIndex[1])
			}
		}
		return s
	})
	w.Write(encodedBody)

}

func (pm *proxyManager) encodeURL(src []byte, start int, end int) []byte {
	relURL := string(src[start:end])
	// keep anchor and javascript links intact
	if relURL == "" || strings.Index(relURL, "#") == 0 || strings.Index(relURL, "javascript") == 0 {
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
	encodedPath := make([]byte, base64.URLEncoding.EncodedLen(end-start))
	base64.URLEncoding.Encode(encodedPath, src[start:end])
	// add some identifier to encoded urls
	encodedString := GopeeEncPrefix + string(encodedPath)
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
			http.Redirect(w, r, "/"+encodeURL(uri.String()), 302)
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
	http.HandleFunc("/", homeHandler)

	http.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, r.URL.Path[1:])
	})

	log.Printf("web proxy listening on %s:%s\n", httpHost, httpPort)

	http.ListenAndServe(httpHost+":"+httpPort, nil)
}
