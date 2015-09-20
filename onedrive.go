package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

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

func (o Onedrive) submit(s string) (items []Item) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", api+s, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Authorization", "bearer "+o.Token)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	jsonParsed, err := gabs.ParseJSON(body)
	if err != nil {
		log.Fatal(err)
	}

	children, _ := jsonParsed.S("value").Children()
	for _, child := range children {
		name := child.Path("name").Data().(string)
		size := child.Path("size").Data().(float64)
		count, ok := child.Path("folder.childCount").Data().(float64)
		if !ok {
			count = -1
		}
		link, _ := child.Search("@content.downloadUrl").Data().(string)
		path, _ := child.Path("parentReference.path").Data().(string)

		item := Item{Name: name, Size: int64(size), Count: int(count), Link: link,
			Path: path, Folder: count > -1}
		items = append(items, item)
	}
	return
}

func (o Onedrive) Drives() []Item {
	return o.submit("/drive")
}

func (o Onedrive) Children(path string) []Item {
	return o.submit("/drive/root:" + path + ":/children")
}

func (o Onedrive) Mkdir(path string) {
}

func (o Onedrive) SyncWith(up Onedrive, downDir, upDir string) {
	items := o.Children(downDir)
	if len(items) == 0 {
		return
	}
	up.Mkdir(upDir)

	chunk := 1024 * 1024 * 10
	buffer := make([]byte, chunk)
	var b bytes.Buffer
	var size int64

	for _, item := range items {
		if item.Folder {
			fmt.Println("Todo: Create directory and call SyncWith")
		} else {
			client := &http.Client{}
			req, err := http.NewRequest("GET", item.Link, nil)
			if err != nil {
				log.Fatal(err)
			}
			req.Header.Add("Authorization", "bearer "+o.Token)
			resp, err := client.Do(req)
			if err != nil {
				log.Fatal(err)
			}

			uploadUrl := up.createSession(item.Name, upDir)
			if len(uploadUrl) == 0 {
				log.Fatal("No Upload-Session")
			}
			fmt.Println("Upload:", item.Name)

			size = 0
			for {
				num, err := io.ReadAtLeast(resp.Body, buffer, chunk)

				b.Reset()
				b.Write(buffer)
				b.Truncate(num)
				req, err2 := http.NewRequest("PUT", uploadUrl, &b)
				if err2 != nil {
					log.Fatal(err2)
				}

				r := fmt.Sprintf("bytes %d-%d/%d", size, size+int64(num)-1, item.Size)
				req.Header.Add("Authorization", "bearer "+up.Token)
				req.Header.Add("Content-Length", fmt.Sprintf("%d", num))
				req.Header.Add("Content-Range", r)
				res, err2 := client.Do(req)
				if err2 != nil {
					log.Fatal(err2)
				}
				res.Body.Close()
				size += int64(num)
				fmt.Println(size, "from", item.Size, "Status", res.StatusCode)
				if err != nil {
					break
				}
				if res.StatusCode >= 400 {
					log.Fatal("Upload-Error")
				}
			}
			resp.Body.Close()
		}
	}
}

func (o Onedrive) createSession(name, dir string) (url string) {
	client := &http.Client{}
	uri := api + "/drive/root:" + dir + "/" + name + ":/upload.createSession"
	req, err := http.NewRequest("POST", uri, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Authorization", "bearer "+o.Token)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	result, err := ioutil.ReadAll(resp.Body)

	jsonParsed, err := gabs.ParseJSON(result)
	if err != nil {
		log.Fatal(err)
	}

	url, _ = jsonParsed.Path("uploadUrl").Data().(string)
	return
}

// only Preview
func (o Onedrive) RemoteDownload(post, uri, name string) {
	dummy := "{\"@content.sourceUrl\": \"%s\", \"name\": \"%s\", \"file\": {}}"
	body := strings.NewReader(fmt.Sprintf(dummy, uri, name))

	client := &http.Client{}
	req, err := http.NewRequest("POST", post, body)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Authorization", "bearer "+o.Token)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Prefer", "respond-async")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	status, err := ioutil.ReadAll(resp.Body)

	fmt.Println(string(status))
}
