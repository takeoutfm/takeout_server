// Copyright (C) 2020 The Takeout Authors.
//
// This file is part of Takeout.
//
// Takeout is free software: you can redistribute it and/or modify it under the
// terms of the GNU Affero General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// Takeout is distributed in the hope that it will be useful, but WITHOUT ANY
// WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
// FOR A PARTICULAR PURPOSE.  See the GNU Affero General Public License for
// more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Takeout.  If not, see <https://www.gnu.org/licenses/>.

package server

import (
	"net/http"
	"strings"

	"github.com/defsub/takeout/auth"
	"github.com/defsub/takeout/config"
	"github.com/defsub/takeout/lib/encoding/xspf"
	"github.com/defsub/takeout/lib/log"
	"github.com/defsub/takeout/music"
)

const (
	ApplicationJson = "application/json"
)

// remove?
func (handler *UserHandler) tracksHandler(w http.ResponseWriter, r *http.Request, m *music.Music) {
	var tracks []music.Track
	if v := r.URL.Query().Get("q"); v != "" {
		tracks = m.Search(strings.TrimSpace(v))
	}

	if len(tracks) > 0 {
		handler.doSpiff(m, "Takeout", tracks, w, r)
	} else {
		notFoundErr(w)
		return
	}
}

// remove?
func (handler *UserHandler) doSpiff(m *music.Music, title string, tracks []music.Track,
	w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-type", xspf.XMLContentType)

	encoder := xspf.NewXMLEncoder(w)
	encoder.Header(title)
	for _, t := range tracks {
		t.Location = []string{m.TrackURL(&t).String()}
		encoder.Encode(t)
	}
	encoder.Footer()
}

func (handler *Handler) doLogin(user, pass string) (http.Cookie, error) {
	return handler.auth.Login(user, pass)
}

func (handler *Handler) doCodeAuth(user, pass, value string) error {
	cookie, err := handler.auth.Login(user, pass)
	if err != nil {
		return err
	}
	err = handler.auth.AuthorizeCode(value, cookie.Value)
	if err != nil {
		return ErrInvalidCode
	}
	return nil
}

func (handler *Handler) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		user := r.Form.Get("user")
		pass := r.Form.Get("pass")
		cookie, err := handler.doLogin(user, pass)
		if err == nil {
			// success
			http.SetCookie(w, &cookie)
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}
	}
	authErr(w, ErrUnauthorized)
}

func (handler *Handler) linkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		user := r.Form.Get("user")
		pass := r.Form.Get("pass")
		value := r.Form.Get("code")
		err := handler.doCodeAuth(user, pass, value)
		if err == nil {
			// success
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}
	}
	http.Redirect(w, r, "/static/link.html", http.StatusTemporaryRedirect)
}

func (handler *Handler) authorizeBearer(w http.ResponseWriter, r *http.Request) *auth.User {
	value := r.Header.Get("Authorization")
	if value == "" {
		return nil
	}
	result := strings.Split(value, " ")
	var token string
	switch len(result) {
	case 1:
		// Authorization: <token>
		token = result[0]
	case 2:
		// Authorization: Bearer <token>
		if strings.EqualFold(result[0], "Bearer") {
			token = result[1]
		}
	}
	if len(token) == 0 {
		return nil
	}
	user, err := handler.auth.TokenUser(token)
	if err != nil {
		return nil
	}
	return user
}

func (handler *Handler) authorizeCookie(w http.ResponseWriter, r *http.Request) *auth.User {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		if cookie != nil {
			handler.auth.Expire(cookie)
			http.SetCookie(w, cookie)
		}
		http.Redirect(w, r, "/static/login.html", http.StatusTemporaryRedirect)
		return nil
	}

	valid := handler.auth.Valid(*cookie)
	if !valid {
		handler.auth.Logout(*cookie)
		handler.auth.Expire(cookie)
		http.SetCookie(w, cookie)
		authErr(w, ErrUnauthorized)
		return nil
	}

	user, err := handler.auth.UserAuth(*cookie)
	if err != nil {
		handler.auth.Logout(*cookie)
		authErr(w, ErrUnauthorized)
		handler.auth.Expire(cookie)
		http.SetCookie(w, cookie)
		return nil
	}

	handler.auth.Refresh(cookie)
	http.SetCookie(w, cookie)

	return user
}

func (handler *Handler) authorize(w http.ResponseWriter, r *http.Request) *auth.User {
	// TODO JWT
	// check for bearer
	user := handler.authorizeBearer(w, r)
	if user != nil {
		return user
	}
	// check for cookie
	return handler.authorizeCookie(w, r)
}

// after user authentication, configure available media
func (handler *Handler) configure(user *auth.User, w http.ResponseWriter) (*UserHandler, error) {
	mediaName, userConfig, err := mediaConfigFor(handler.config, user)
	if err != nil {
		return nil, err
	}
	media := makeMedia(mediaName, userConfig)
	return &UserHandler{
		user:     user,
		media:    media,
		config:   userConfig,
		template: handler.template,
	}, nil
}

func Serve(config *config.Config) {
	template := getTemplates(config)

	schedule(config)

	auth, err := makeAuth(config)
	if err != nil {
		log.CheckError(err)
	}

	hub, err := makeHub(config)

	makeHandler := func() *Handler {
		return &Handler{
			auth:     auth,
			config:   config,
			template: template,
		}
	}

	// makeHubHandler := func(w http.ResponseWriter, r *http.Request) *HubHandler {
	// 	handler := makeHandler()
	// 	user := handler.authorize(w, r)
	// 	if user == nil {
	// 		return nil
	// 	}
	// 	return &HubHandler{
	// 		auth:   auth,
	// 		config: config,
	// 		hub:    hub,
	// 	}
	// }

	makeUserHandler := func(w http.ResponseWriter, r *http.Request) *UserHandler {
		handler := makeHandler()
		user := handler.authorize(w, r)
		if user == nil {
			return nil
		}
		userHandler, err := handler.configure(user, w)
		if err != nil {
			serverErr(w, err)
			return nil
		}
		return userHandler
	}

	loginHandler := func(w http.ResponseWriter, r *http.Request) {
		makeHandler().loginHandler(w, r)
	}

	linkHandler := func(w http.ResponseWriter, r *http.Request) {
		makeHandler().linkHandler(w, r)
	}

	apiLoginHandler := func(w http.ResponseWriter, r *http.Request) {
		makeHandler().apiLogin(w, r)
	}

	hookHandler := func(w http.ResponseWriter, r *http.Request) {
		makeHandler().hookHandler(w, r)
	}

	tracksHandler := func(w http.ResponseWriter, r *http.Request) {
		// TODO keep this? auth? user config?
		// handler := makeHandler()
		// m := music.NewMusic(config)
		// err := m.Open()
		// if err != nil {
		// 	serverErr(w, err)
		// 	return
		// }
		// defer m.Close()
		// handler.tracksHandler(w, r, m)
	}

	viewHandler := func(w http.ResponseWriter, r *http.Request) {
		userHandler := makeUserHandler(w, r)
		if userHandler != nil {
			userHandler.viewHandler(w, r)
		}
	}

	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		userHandler := makeUserHandler(w, r)
		if userHandler != nil {
			userHandler.apiHandler(w, r)
		}
	}

	liveHandler := func(w http.ResponseWriter, r *http.Request) {
		hub.Handle(auth, w, r)
	}

	swaggerHandler := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/swagger.json", 302)
	}

	resFileServer := http.FileServer(mountResFS(resStatic))

	http.Handle("/static/", resFileServer)
	http.HandleFunc("/swagger.json", swaggerHandler)
	http.HandleFunc("/tracks", tracksHandler)
	http.HandleFunc("/", viewHandler)
	http.HandleFunc("/v", viewHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/link", linkHandler)
	http.HandleFunc("/api/login", apiLoginHandler)
	http.HandleFunc("/api/", apiHandler)
	http.HandleFunc("/hook/", hookHandler)
	http.HandleFunc("/live", liveHandler)
	log.Printf("listening on %s\n", config.Server.Listen)
	http.ListenAndServe(config.Server.Listen, nil)
}
