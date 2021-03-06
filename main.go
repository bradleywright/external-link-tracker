package main

import (
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/alext/tablecloth"
	"labix.org/v2/mgo"
)

func getenvDefault(key string, defaultVal string) string {
	val := os.Getenv(key)
	if val == "" {
		val = defaultVal
	}

	return val
}

var (
	pubAddr         = getenvDefault("LINK_TRACKER_PUBADDR", ":8080")
	apiAddr         = getenvDefault("LINK_TRACKER_APIADDR", ":8081")
	mgoSession      *mgo.Session
	mgoSessionOnce  sync.Once
	mgoDatabaseName = getenvDefault("LINK_TRACKER_MONGO_DB", "external_link_tracker")
	mgoURL          = getenvDefault("LINK_TRACKER_MONGO_URL", "localhost")
)

func getMgoSession() *mgo.Session {
	mgoSessionOnce.Do(func() {
		var err error
		mgoSession, err = mgo.Dial(mgoURL)
		if err != nil {
			panic(err) // no, not really
		}
	})
	return mgoSession.Clone()
}

func catchListenAndServe(addr string, handler http.Handler, ident string, wg *sync.WaitGroup) {
	defer wg.Done()
	err := tablecloth.ListenAndServe(addr, handler, ident)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {

	// in order to ensure the database doesn't grown to enormous sizes, cap the
	// collection to a week
	session := getMgoSession()
	database := session.DB(mgoDatabaseName)
	index := mgo.Index{
		Key:         []string{"date_time"},
		ExpireAfter: 24 * time.Hour * 7, // expire after a week
		Background:  true,
	}
	database.C("hits").EnsureIndex(index)

	if wd := os.Getenv("GOVUK_APP_ROOT"); wd != "" {
		tablecloth.WorkingDir = wd
	}

	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/g", ExternalLinkTrackerHandler)

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/url", AddExternalURL)
	apiMux.HandleFunc("/healthcheck", healthcheck)

	wg := &sync.WaitGroup{}
	wg.Add(2)
	go catchListenAndServe(pubAddr, publicMux, "redirects", wg)
	log.Println("external-link-tracker: listening for redirects on " + pubAddr)

	go catchListenAndServe(apiAddr, apiMux, "api", wg)
	log.Println("external-link-tracker: listening for writes on " + apiAddr)

	wg.Wait()
}
