/*
Creates a new session for every user / browser
Each session has a http client assigned with its own cookie jar
*/

package main

import (
	"container/list"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

// ClientStore holds the sessionId for the session and the related http client
// lastAccessed time is used for expiry
type ClientStore struct {
	sessionId    string
	lastAccessed time.Time
	httpClient   *http.Client
}

type Manager struct {
	cookieName  string
	clients     map[string]*list.Element
	list        *list.List
	maxLifetime int64
	lock        sync.RWMutex
}

func redirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if len(via) == 0 {
		return nil
	}
	// copy all headers
	for attr, val := range via[0].Header {
		if _, ok := req.Header[attr]; !ok {
			req.Header[attr] = val
		}
	}
	return nil
}

func NewManager(cookieName string, maxLifetime int64) *Manager {
	clients := make(map[string]*list.Element)
	return &Manager{cookieName: cookieName, clients: clients, list: list.New(), maxLifetime: maxLifetime}
}

// Start will read the session cookie if it exists and retrieve the http client assigned,
// if a session does not exist or is expired, a new session will be created
func (manager *Manager) Start(w http.ResponseWriter, r *http.Request) (httpClient *http.Client, err error) {
	var clientStore *ClientStore
	cookie, err := r.Cookie(manager.cookieName)
	if err != nil || cookie.Value == "" {
		// session cookie not found
		clientStore = manager.Create()
		manager.setCookie(clientStore.sessionId, w)
		return clientStore.httpClient, nil
	} else {
		// session cookie found
		sid, errs := url.QueryUnescape(cookie.Value)
		if errs != nil {
			return nil, errs
		}
		clientStore = manager.Get(sid)
		if clientStore == nil {
			// session expired
			clientStore = manager.Create()
			manager.setCookie(clientStore.sessionId, w)
		}
		return clientStore.httpClient, nil
	}
}

func (manager *Manager) setCookie(sid string, w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     manager.cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
	}
	http.SetCookie(w, cookie)
}

// Create will create a new client store
func (manager *Manager) Create() *ClientStore {
	log.Println("SESSION::CREATE")
	manager.lock.Lock()
	defer manager.lock.Unlock()
	cookieJar, _ := cookiejar.New(nil)
	client := &http.Client{CheckRedirect: redirectPolicy, Jar: cookieJar}
	sid := generateSessionId(32)
	clientStore := &ClientStore{sessionId: sid, lastAccessed: time.Now(), httpClient: client}
	element := manager.list.PushBack(clientStore)
	manager.clients[sid] = element
	return clientStore
}

// Get will try to get the existing session
func (manager *Manager) Get(sid string) *ClientStore {
	log.Println("SESSION::GET", sid)
	manager.lock.RLock()
	defer manager.lock.RUnlock()
	if element, ok := manager.clients[sid]; ok {
		go manager.Update(sid)
		return element.Value.(*ClientStore)
	}
	return nil
}

func (manager *Manager) Destroy(sid string) {
	log.Println("SESSION::DESTROY", sid)
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if element, ok := manager.clients[sid]; ok {
		delete(manager.clients, sid)
		manager.list.Remove(element)
	}
}

func (manager *Manager) Update(sid string) {
	log.Println("SESSION::UPDATE", sid)
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if element, ok := manager.clients[sid]; ok {
		element.Value.(*ClientStore).lastAccessed = time.Now()
		manager.list.MoveToFront(element)
	}
}

// clean clients for expired sessions
func (manager *Manager) GC() {
	log.Println("SESSION::GC")
	manager.lock.Lock()
	defer manager.lock.Unlock()
	// iterate until all expired sessions are removed
	for {
		// stale sessions are found at the back of the list
		element := manager.list.Back()
		// list is empty
		if element == nil {
			break
		}
		if (element.Value.(*ClientStore).lastAccessed.Unix() + manager.maxLifetime) < time.Now().Unix() {
			log.Println("REMOVE", element.Value.(*ClientStore).sessionId)
			delete(manager.clients, element.Value.(*ClientStore).sessionId)
			manager.list.Remove(element)
		} else {
			break
		}
	}
	time.AfterFunc(time.Duration(manager.maxLifetime)*time.Second, func() { manager.GC() })
}

// generate a random id used for identifying sessions
func generateSessionId(strength int) string {
	b := make([]byte, strength)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(b)
}
