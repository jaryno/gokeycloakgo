package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"time"

	"learn.oatuh.client/model"
)

var port = "8081"
var host = "http://localhost:" + port

var config = struct {
	appID               string
	appPassword         string
	authURL             string
	logoutURL           string
	afterLogoutRedirect string
	authCodeCallback    string
	tokenEndpoint       string
	servicesEndpoint    string
}{
	appID:               "billingApp",
	appPassword:         "b96d3964-b8c2-4302-af07-3ec440945611",
	authURL:             "http://localhost:8080/auth/realms/learningApp/protocol/openid-connect/auth",
	logoutURL:           "http://localhost:8080/auth/realms/learningApp/protocol/openid-connect/logout",
	afterLogoutRedirect: host,
	authCodeCallback:    host + "/authCodeRedirect",
	tokenEndpoint:       "http://localhost:8080/auth/realms/learningApp/protocol/openid-connect/token",
	servicesEndpoint:    "http://localhost:8082/billing/v1/services",
}

type AppVar struct {
	AuthCode     string
	SessionState string
	AccessToken  string
	RefreshToken string
	Scope        string
	Services     []string
}

var t = template.Must(template.ParseFiles("template/index.html"))
var tServices = template.Must(template.ParseFiles("template/index.html", "template/services.html"))

var appVar = AppVar{}

func main() {
	// fmt.Println("hello")
	http.HandleFunc("/", enabledLog(home))
	http.HandleFunc("/login", enabledLog(login))
	http.HandleFunc("/logout", enabledLog(logout))
	http.HandleFunc("/exchangeToken", enabledLog(exchangeToken))
	http.HandleFunc("/services", enabledLog(services))
	http.HandleFunc("/authCodeRedirect", enabledLog(authCodeRedirect))
	http.ListenAndServe(":"+port, nil)
}

func init() {
	log.SetFlags(log.Ltime)
}

func enabledLog(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		handlerName := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
		log.SetPrefix(handlerName + " ")
		log.Println("--> " + handlerName)
		log.Printf("request : %+v\n", r.RequestURI)
		// log.Printf("response : %+v\n", w)
		handler(w, r)
		log.Println("<-- " + handlerName)
	}
}

func home(w http.ResponseWriter, r *http.Request) {
	t.Execute(w, appVar)
}

func login(w http.ResponseWriter, r *http.Request) {
	// create a redirect URL for authentication endpoint
	req, err := http.NewRequest("GET", config.authURL, nil)
	if err != nil {
		log.Print(err)
		return
	}

	qs := url.Values{}
	qs.Add("state", "123")
	qs.Add("client_id", config.appID)
	qs.Add("response_type", "code")
	qs.Add("redirect_uri", config.authCodeCallback)

	req.URL.RawQuery = qs.Encode()

	http.Redirect(w, r, req.URL.String(), http.StatusFound)
}

func authCodeRedirect(w http.ResponseWriter, r *http.Request) {
	appVar.AuthCode = r.URL.Query().Get("code")
	appVar.SessionState = r.URL.Query().Get("session_state")
	r.URL.RawQuery = ""
	fmt.Printf("Request queries: %+v\n", appVar)

	http.Redirect(w, r, host, http.StatusFound)
}

func logout(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	q.Add("redirect_uri", config.afterLogoutRedirect)

	logoutURL, err := url.Parse(config.logoutURL)
	if err != nil {
		log.Println(err)
	}
	logoutURL.RawQuery = q.Encode()
	appVar = AppVar{}
	http.Redirect(w, r, logoutURL.String(), http.StatusFound)
}

func exchangeToken(w http.ResponseWriter, r *http.Request) {

	// Request
	form := url.Values{}
	form.Add("grant_type", "authorization_code")
	form.Add("code", appVar.AuthCode)
	form.Add("redirect_uri", config.authCodeCallback)
	form.Add("client_id", config.appID)
	req, err := http.NewRequest("POST", config.tokenEndpoint, strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	if err != nil {
		log.Println(err)
		return
	}

	req.SetBasicAuth(config.appID, config.appPassword)

	// Client
	c := http.Client{}
	res, err := c.Do(req)
	if err != nil {
		log.Println("couldn't get access token", err)
		return
	}

	// Proccess response
	byteBody, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	if err != nil {
		log.Println(err)
		return
	}

	accessTokenResponse := &model.AccessTokenResponse{}
	json.Unmarshal(byteBody, accessTokenResponse)

	appVar.AccessToken = accessTokenResponse.AccessToken
	appVar.RefreshToken = accessTokenResponse.RefreshToken
	appVar.Scope = accessTokenResponse.Scope
	log.Println(appVar.AccessToken)

	t.Execute(w, appVar)
}

func services(w http.ResponseWriter, r *http.Request) {

	// request
	req, err := http.NewRequest("GET", config.servicesEndpoint, nil)
	if err != nil {
		log.Println(err)
		tServices.Execute(w, appVar)
		return
	}

	req.Header.Add("Authorization", "Bearer "+appVar.AccessToken)
	log.Println(appVar.AccessToken)

	// client
	ctx, cancelFunc := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelFunc()

	c := http.Client{}
	res, err := c.Do(req.WithContext(ctx)) // fail if we have to wait for more than 0.5 second
	if err != nil {
		log.Println(err)
		tServices.Execute(w, appVar)
		return
	}

	byteBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Println(err)
		tServices.Execute(w, appVar)
		return
	}

	// proccess response
	if res.StatusCode != 200 {
		log.Println(string(byteBody))
		tServices.Execute(w, appVar)
		return
	}

	billingResponse := &model.Billing{}
	err = json.Unmarshal(byteBody, billingResponse)
	if err != nil {
		log.Println(err)
		tServices.Execute(w, appVar)
		return
	}
	appVar.Services = billingResponse.Services

	tServices.Execute(w, appVar)
}
