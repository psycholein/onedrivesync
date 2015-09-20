package main

import (
	"fmt"
	"gotank/libs/yaml"
	"io/ioutil"
	"log"

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

	downToken := getToken(c.Clientid, c.Secret, "Download")
	upToken := getToken(c.Clientid, c.Secret, "Upload")

	down := Onedrive{downToken}
	up := Onedrive{upToken}
	down.SyncWith(up, c.DownDir, c.UpDir)
}

func getToken(clientid, secret, msg string) string {
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
	return tok.AccessToken
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
