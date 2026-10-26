package discogs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// App is the household's Discogs app ("Joshua", its consumer key and
// secret from laib, HAUS_DISCOGS_KEY and HAUS_DISCOGS_SECRET), through
// which each person connects their own account: Discogs asks them in the
// browser, Haus keeps their token. OAuth 1.0a with PLAINTEXT signatures,
// over https only, as Discogs allows.
type App struct {
	Key, Secret string
	Base        string // https://api.discogs.com
	Web         string // https://www.discogs.com, where a person says yes
	HTTP        *http.Client
}

// AppFromEnv is the app from HAUS_DISCOGS_KEY and HAUS_DISCOGS_SECRET, or
// nil without both.
func AppFromEnv() *App {
	k, s := strings.TrimSpace(os.Getenv("HAUS_DISCOGS_KEY")), strings.TrimSpace(os.Getenv("HAUS_DISCOGS_SECRET"))
	if k == "" || s == "" {
		return nil
	}
	return &App{Key: k, Secret: s, Base: "https://api.discogs.com", Web: "https://www.discogs.com"}
}

// oauthHeader is the OAuth 1.0a header, PLAINTEXT: the signature is the
// two secrets joined, which https carries safely.
func oauthHeader(key, secret, token, tokenSecret string, extra map[string]string) string {
	nonce := make([]byte, 12)
	rand.Read(nonce)
	p := map[string]string{"oauth_consumer_key": key, "oauth_nonce": hex.EncodeToString(nonce), "oauth_signature_method": "PLAINTEXT",
		"oauth_timestamp": strconv.FormatInt(time.Now().Unix(), 10), "oauth_version": "1.0",
		"oauth_signature": url.QueryEscape(secret) + "&" + url.QueryEscape(tokenSecret)}
	if token != "" {
		p["oauth_token"] = token
	}
	for k, v := range extra {
		p[k] = v
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := p[k]
		if k != "oauth_signature" {
			v = url.QueryEscape(v)
		}
		parts = append(parts, k+`="`+v+`"`)
	}
	return "OAuth " + strings.Join(parts, ", ")
}

// form posts to an OAuth endpoint and reads its form-encoded answer.
func (a *App) form(ctx context.Context, path, token, tokenSecret string, extra map[string]string) (url.Values, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.Base, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", oauthHeader(a.Key, a.Secret, token, tokenSecret, extra))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	h := a.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode >= 300 {
		return nil, &Error{res.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 200)]))}
	}
	return url.ParseQuery(string(raw))
}

// Anfrage starts a person's connection: a request token, and where to send
// them to say yes; Discogs sends them back to callback.
func (a *App) Anfrage(ctx context.Context, callback string) (token, secret, authorize string, err error) {
	v, err := a.form(ctx, "/oauth/request_token", "", "", map[string]string{"oauth_callback": callback})
	if err != nil {
		return "", "", "", err
	}
	token, secret = v.Get("oauth_token"), v.Get("oauth_token_secret")
	if token == "" || secret == "" {
		return "", "", "", errors.New("discogs: no request token")
	}
	return token, secret, strings.TrimRight(a.Web, "/") + "/oauth/authorize?oauth_token=" + url.QueryEscape(token), nil
}

// Zugang ends it: the request token, its secret and the verifier Discogs
// sent back give the person's access token and its secret.
func (a *App) Zugang(ctx context.Context, token, secret, verifier string) (string, string, error) {
	v, err := a.form(ctx, "/oauth/access_token", token, secret, map[string]string{"oauth_verifier": verifier})
	if err != nil {
		return "", "", err
	}
	if v.Get("oauth_token") == "" || v.Get("oauth_token_secret") == "" {
		return "", "", errors.New("discogs: no access token")
	}
	return v.Get("oauth_token"), v.Get("oauth_token_secret"), nil
}

// Client is Discogs as one person, with their access token.
func (a *App) Client(token, secret string) *Client {
	return &Client{Base: a.Base, HTTP: a.HTTP, ConsumerKey: a.Key, ConsumerSecret: a.Secret, OAuthToken: token, OAuthSecret: secret}
}

// Konto is a person's connected Discogs account: its name and its token
// (kept by Haus alone, never shown).
type Konto struct {
	Name   string    `json:"name"`
	Token  string    `json:"token"`
	Secret string    `json:"secret"`
	Seit   time.Time `json:"seit"`
}

// Regale is every person's shelf: their connected account, its collection
// read as a Regal, and the connections on their way.
type Regale struct {
	App *App
	Dir string // HAUS_DATA: discogs-konten.json, 0600

	mu      sync.Mutex
	regale  map[string]*Regal
	pending map[string]pendingConn // request token → who and its secret
}

type pendingConn struct {
	user, secret string
	at           time.Time
}

const kontenFile = "discogs-konten.json"

func (r *Regale) load() map[string]Konto {
	out := map[string]Konto{}
	if b, err := os.ReadFile(filepath.Join(r.Dir, kontenFile)); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

func (r *Regale) save(all map[string]Konto) error {
	b, _ := json.MarshalIndent(all, "", "  ")
	p := filepath.Join(r.Dir, kontenFile)
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// Konto is the person's connected account, if any.
func (r *Regale) Konto(user string) (Konto, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.load()[user]
	return k, ok
}

// For is the person's shelf; false when they have not connected Discogs.
func (r *Regale) For(user string) (*Regal, bool) {
	if r == nil || user == "" {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if rg, ok := r.regale[user]; ok {
		return rg, true
	}
	k, ok := r.load()[user]
	if !ok {
		return nil, false
	}
	c := r.App.Client(k.Token, k.Secret)
	c.user = k.Name
	rg := &Regal{C: c}
	if r.regale == nil {
		r.regale = map[string]*Regal{}
	}
	r.regale[user] = rg
	return rg, true
}

// Verbinden starts user's connection: where to send them to say yes.
func (r *Regale) Verbinden(ctx context.Context, user, callback string) (string, error) {
	token, secret, authorize, err := r.App.Anfrage(ctx, callback)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = map[string]pendingConn{}
	}
	for t, p := range r.pending { // a connection not finished in 15 minutes is gone
		if time.Since(p.at) > 15*time.Minute {
			delete(r.pending, t)
		}
	}
	r.pending[token] = pendingConn{user: user, secret: secret, at: time.Now()}
	return authorize, nil
}

// ErrFremd is a return from Discogs that is not this person's connection
// (another person's, an old one, or none Haus started).
var ErrFremd = errors.New("Diese Verbindung hat Haus nicht für dich begonnen. Tipp noch einmal auf „Mit Discogs verbinden“.")

// Fertig finishes user's connection with what Discogs sent back, and keeps
// the account: its name answers.
func (r *Regale) Fertig(ctx context.Context, user, token, verifier string) (string, error) {
	r.mu.Lock()
	p, ok := r.pending[token]
	if ok {
		delete(r.pending, token)
	}
	r.mu.Unlock()
	if !ok || p.user != user || time.Since(p.at) > 15*time.Minute {
		return "", ErrFremd
	}
	at, as, err := r.App.Zugang(ctx, token, p.secret, verifier)
	if err != nil {
		return "", err
	}
	c := r.App.Client(at, as)
	name, err := c.User(ctx)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.load()
	all[user] = Konto{Name: name, Token: at, Secret: as, Seit: time.Now()}
	delete(r.regale, user)
	return name, r.save(all)
}

// Trennen forgets user's account (Discogs keeps the app's access until the
// person revokes it there).
func (r *Regale) Trennen(user string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.load()
	delete(all, user)
	delete(r.regale, user)
	return r.save(all)
}
