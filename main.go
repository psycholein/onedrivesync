package main

import (
	"fmt"
	"gotank/libs/yaml"
	"io/ioutil"
	"log"
	"net/http"

	"golang.org/x/oauth2"
)

type config struct {
	Clientid string
	Secret   string
	DownDir  string
	UpDir    string
}

func main() {
	c := config{}
	readYaml("config.yml", &c)

	downClient := getClient(c.Clientid, c.Secret, "Download")
	upClient := getClient(c.Clientid, c.Secret, "Upload")

	down := Onedrive{downClient}
	up := Onedrive{upClient}
	down.SyncWith(up, c.DownDir, c.UpDir, 5)
}

func getClient(clientid, secret, msg string) *http.Client {
	conf := &oauth2.Config{
		ClientID:     clientid,
		ClientSecret: secret,
		Scopes:       []string{"onedrive.readwrite"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.live.com/oauth20_authorize.srf",
			TokenURL: "https://login.live.com/oauth20_token.srf",
		},
	}
	url := conf.AuthCodeURL("state", oauth2.AccessTypeOffline)
	fmt.Printf("Visit the URL for %s: %v", msg, url)
	fmt.Print("\nCode:")
	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatal(err)
	}
	tok, err := conf.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatal(err)
	}
	client := conf.Client(oauth2.NoContext, tok)
	return client
}

func readYaml(filename string, data interface{}) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	err = yaml.Unmarshal(b, data)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
}
