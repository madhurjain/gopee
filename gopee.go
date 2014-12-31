package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Cache templates
var templates = template.Must(template.ParseFiles("home.html"))

// Pre-compile RegEx
var reBase = regexp.MustCompile("base +href=\"(.*?)\"")
var reHTML = regexp.MustCompile("src=[\"\\'](.*?)[\"\\']|href=[\"\\'](.*?)[\"\\']")
var reCSS = regexp.MustCompile("url\\([\"\\']?(.*?)[\"\\']?\\)")

func homeHandler(w http.ResponseWriter, r *http.Request) {
	// 404 for all other url path
	if r.URL.Path[1:] != "" {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	url := r.FormValue("url")
	if url != "" {
		encodedUrl := base64.StdEncoding.EncodeToString([]byte(url))
		http.Redirect(w, r, "/p/"+encodedUrl, 302)
		return
	}
	templates.ExecuteTemplate(w, "home.html", nil)
}

func encodeURL(src []byte, baseHref string, urlString string, start int, end int) []byte {
	relURL := string(src[start:end])
	// keep anchor and javascript links intact
	if strings.Index(relURL, "#") == 0 || strings.Index(relURL, "javascript") == 0 {
		return src
	}
	// Check if url is relative and make it absolute
	if strings.Index(relURL, "http") != 0 {
		var basePath *url.URL
		if baseHref == "" {
			basePath, _ = url.Parse(urlString)
		} else {
			basePath, _ = url.Parse(baseHref)
		}
		relPath, err := url.Parse(relURL)
		if err != nil {
			return src
		}
		absURL := basePath.ResolveReference(relPath).String()
		src = bytes.Replace(src, []byte(relURL), []byte(absURL), -1)
		end = start + len(absURL)
	}
	var encodedPath []byte = make([]byte, base64.StdEncoding.EncodedLen(end-start))
	base64.StdEncoding.Encode(encodedPath, src[start:end])
	return bytes.Replace(src, src[start:end], encodedPath, -1)
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	encodedUrl := r.URL.Path[len("/p/"):]
	url, err := base64.StdEncoding.DecodeString(encodedUrl)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	urlString := string(url[:])
	fmt.Println(urlString)
	resp, err := http.Get(urlString)
	if err != nil {
		fmt.Println("Error Fetching " + urlString)
		http.NotFound(w, r)
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	w.Header().Set("Content-Type", contentType)

	// Rewrite all urls
	baseHref := ""
	if strings.Contains(contentType, "text/html") {
		body, _ := ioutil.ReadAll(resp.Body)
		baseHrefMatch := reBase.FindSubmatch(body)
		if len(baseHrefMatch) > 0 {
			baseHref = string(baseHrefMatch[1][:])
		}
		encodedBody := reHTML.ReplaceAllFunc(body, func(s []byte) []byte {
			parts := reHTML.FindSubmatchIndex(s)
			if parts != nil {
				// replace src attribute
				srcIndex := parts[2:4]
				if srcIndex[0] != -1 {
					return encodeURL(s, baseHref, urlString, srcIndex[0], srcIndex[1])
				}

				// replace href attribute
				hrefIndex := parts[4:6]
				if hrefIndex[0] != -1 {
					return encodeURL(s, baseHref, urlString, hrefIndex[0], hrefIndex[1])
				}
			}
			return s
		})
		w.Write(encodedBody)
	} else if strings.Contains(contentType, "text/css") {
		body, _ := ioutil.ReadAll(resp.Body)
		encodedBody := reCSS.ReplaceAllFunc(body, func(s []byte) []byte {
			parts := reCSS.FindSubmatchIndex(s)
			if parts != nil {
				// replace url attribute in css
				pathIndex := parts[2:4]
				if pathIndex[0] != -1 {
					return encodeURL(s, baseHref, urlString, pathIndex[0], pathIndex[1])
				}
			}
			return s
		})
		w.Write(encodedBody)
	} else {
		io.Copy(w, resp.Body)
	}
}

func main() {

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/p/", proxyHandler)

	http.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, r.URL.Path[1:])
	})

	fmt.Println("Server listening on 8080")
  
  http.ListenAndServe(":8080", nil)

}
