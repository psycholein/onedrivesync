package main

import (
	"fmt"
	"gotank/libs/yaml"
	"io/ioutil"
	"log"
)

type config struct {
	Download string
	DownDir  string
	Upload   string
	UpDir    string
}

func main() {
	c := config{}
	readYaml("config.yml", &c)
	down := Onedrive{c.Download}
	fmt.Println(down.Children(c.DownDir))

	up := Onedrive{c.Upload}
	fmt.Println(up.Children(c.UpDir))

	down.SyncWith(up, c.DownDir, c.UpDir)
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
