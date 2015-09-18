package main

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/Jeffail/gabs"
)

const (
	api = "https://api.onedrive.com/v1.0"
)

type Onedrive struct {
	Token string
}

type Item struct {
	Name   string
	Size   int64
	Count  int
	Link   string
	Path   string
	Folder bool
}

func (o Onedrive) submit(s string) {
	url := api + s + "?access_token=" + o.Token
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	//body = []byte(strings.Replace(string(body), "@content.", "", -1))
	jsonParsed, err := gabs.ParseJSON(body)
	if err != nil {
		return
	}

	children, _ := jsonParsed.S("value").Children()
	for _, child := range children {
		name := child.Path("name").Data().(string)
		size := child.Path("size").Data().(float64)
		count, _ := child.Path("folder.childCount").Data().(float64)
		link, _ := child.Search("@content.downloadUrl").Data().(string)
		path, _ := child.Path("parentReference.path").Data().(string)

		fmt.Println(path, count, link, name, int64(size), child, "\n")
	}
}

func (o Onedrive) Drives() {
	o.submit("/drive")
}

func (o Onedrive) Children(path string) {
	o.submit("/drive/root:" + path + ":/children")
}
