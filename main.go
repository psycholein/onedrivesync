package main

import (
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
	o := Onedrive{c.Download}
	o.Children(c.DownDir)
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
