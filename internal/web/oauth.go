package web

import (
	"context"
	"crypto/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/oauth"
)

// signIn is one Microsoft device-code sign-in started from the mail form.
// The refresh token never goes to the browser: the form posts only the id.
type signIn struct {
	clientID string
	code     oauth.DeviceCode
	token    string
	err      string
	done     bool
	started  time.Time
}

type signInStore struct {
	mu sync.Mutex
	m  map[string]*signIn
}

// signIns is package-level because readMailForm (which turns the posted id
// into a token) has no Server; Delistd runs one web server per process.
var signIns = &signInStore{m: map[string]*signIn{}}

const signInTTL = 30 * time.Minute

func (st *signInStore) add(s *signIn) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	for id, old := range st.m {
		if time.Since(old.started) > signInTTL {
			delete(st.m, id)
		}
	}
	id := rand.Text()
	st.m[id] = s
	return id
}

func (st *signInStore) get(id string) (signIn, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.m[id]
	if !ok {
		return signIn{}, false
	}
	return *s, true
}

func (st *signInStore) finish(id, token, errMsg string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s, ok := st.m[id]; ok {
		s.token, s.err, s.done = token, errMsg, true
	}
}

// token is the refresh token of a finished sign-in started for clientID.
func (st *signInStore) token(id, clientID string) (string, bool) {
	s, ok := st.get(id)
	if !ok || !s.done || s.token == "" || s.clientID != clientID {
		return "", false
	}
	return s.token, true
}

// oauthView is partials/oauth-status.html's data.
type oauthView struct {
	ID       string
	Code     string
	URL      string
	Pending  bool
	Done     bool
	Error    string
	ClientID string
}

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	clientID := strings.TrimSpace(r.FormValue("oauth_client_id")) // "" = the shared default
	if config.OAuthClientID(clientID) == "" {
		s.renderPartial(w, "partials/oauth-status.html", oauthView{Error: "Paste your app's Application (client) ID first (step 5 above)."})
		return
	}
	c := oauth.NewClient(oauth.MicrosoftConsumers, config.OAuthClientID(clientID))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	dc, err := c.StartDeviceCode(ctx)
	if err != nil {
		s.renderPartial(w, "partials/oauth-status.html", oauthView{Error: err.Error()})
		return
	}
	id := signIns.add(&signIn{clientID: clientID, code: dc, started: time.Now()})
	go func() {
		pctx, stop := context.WithTimeout(context.Background(), time.Duration(dc.ExpiresIn)*time.Second+30*time.Second)
		defer stop()
		tok, err := c.PollDeviceCode(pctx, dc)
		switch {
		case err != nil:
			signIns.finish(id, "", err.Error())
		case tok.Refresh == "":
			signIns.finish(id, "", "Microsoft returned no refresh token; check the app allows offline_access (see the guide).")
		default:
			signIns.finish(id, tok.Refresh, "")
		}
	}()
	s.renderPartial(w, "partials/oauth-status.html", signInView(id, signIn{clientID: clientID, code: dc}))
}

func (s *Server) handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	si, ok := signIns.get(id)
	if !ok {
		s.renderPartial(w, "partials/oauth-status.html", oauthView{Error: "This sign-in expired. Start it again."})
		return
	}
	s.renderPartial(w, "partials/oauth-status.html", signInView(id, si))
}

func signInView(id string, si signIn) oauthView {
	v := oauthView{ID: id, Code: si.code.UserCode, URL: si.code.VerificationURI, ClientID: si.clientID}
	switch {
	case si.err != "":
		v.ID, v.Error = "", si.err
	case si.done:
		v.Done = true
	default:
		v.Pending = true
	}
	return v
}
