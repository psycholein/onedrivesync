package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/Jeffail/gabs"
	"github.com/pivotal-golang/bytefmt"
	"golang.org/x/oauth2"
)

const (
	api   = "https://api.onedrive.com/v1.0"
	chunk = 1024 * 1024 * 10
)

type Onedrive struct {
	Conf  *oauth2.Config
	Token *oauth2.Token
}

type Item struct {
	Name   string
	Size   int64
	Count  int
	Link   string
	Path   string
	Folder bool
}

type Sync struct {
	item  Item
	up    Onedrive
	upDir string
}

var jobs chan Sync

func (o Onedrive) submit(s string) (items []Item) {
	req, err := http.NewRequest("GET", api+s, nil)
	if err != nil {
		log.Fatal(err)
	}
	client := o.Conf.Client(oauth2.NoContext, o.Token)
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

func (o Onedrive) startJobs(jobCount int) {
	jobs = make(chan Sync)
	for i := 0; i < jobCount; i++ {
		go func() {
			for job := range jobs {
				for !o.syncFile(job.up, job.upDir, job.item) {
					fmt.Println("Upload-Error! Try again in 5 seconds")
					time.Sleep(5 * time.Second)
				}
			}
		}()
	}
}

func (o Onedrive) SyncWith(up Onedrive, downDir, upDir string, jobCount int) {
	items := o.Children(downDir)
	if len(items) == 0 {
		return
	}
	if jobCount > 0 {
		o.startJobs(jobCount)
	}
	up.Mkdir(upDir) // TODO
	upItems := up.Children(upDir)
	for _, item := range items {
		if item.Folder {
			fmt.Println("Todo: Call SyncWith")
		} else {
			found := false
			for _, upItem := range upItems {
				if upItem.Name == item.Name && upItem.Size == item.Size {
					found = true
				}
			}
			if found {
				fmt.Println("Found:", item.Name)
				continue
			}
			jobs <- Sync{up: up, upDir: upDir, item: item}
		}
	}
	if jobCount > 0 {
		close(jobs)
	}
}

func (o Onedrive) syncFile(up Onedrive, upDir string, item Item) bool {
	var b bytes.Buffer
	var size int64
	buffer := make([]byte, chunk)

	req, err := http.NewRequest("GET", item.Link, nil)
	if err != nil {
		return false
	}
	client := o.Conf.Client(oauth2.NoContext, o.Token)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	uploadUrl, err := up.createSession(item.Name, upDir)
	if err != nil || len(uploadUrl) == 0 {
		return false
	}
	fmt.Println("Upload:", item.Name, "Size:", item.Size)

	for {
		tries := 0
		num, err := io.ReadAtLeast(resp.Body, buffer, chunk)

	AGAIN:
		b.Reset()
		b.Write(buffer)
		b.Truncate(num)
		req, err2 := http.NewRequest("PUT", uploadUrl, &b)
		if err2 != nil {
			return false
		}

		client := up.Conf.Client(oauth2.NoContext, up.Token)
		r := fmt.Sprintf("bytes %d-%d/%d", size, size+int64(num)-1, item.Size)
		req.Header.Add("Content-Length", fmt.Sprintf("%d", num))
		req.Header.Add("Content-Range", r)
		res, err2 := client.Do(req)
		if err2 != nil {
			return false
		}
		res.Body.Close()

		size += int64(num)
		from := bytefmt.ByteSize(uint64(size))
		to := bytefmt.ByteSize(uint64(item.Size))
		fmt.Println(from, "/", to, "Status:", res.StatusCode, "Name:", item.Name)
		if res.StatusCode >= 400 {
			if tries < 3 {
				size -= int64(num)
				tries++
				goto AGAIN
			}
			fmt.Println(res, num, size)
			return false
		}
		if err != nil {
			if item.Size != size {
				fmt.Println(err, num, size)
				return false
			}
			break
		}
	}
	fmt.Println(item.Name, "uploaded!")
	return true
}

func (o Onedrive) createSession(name, dir string) (string, error) {
	uri := api + "/drive/root:" + dir + "/" + name + ":/upload.createSession"
	req, err := http.NewRequest("POST", uri, nil)
	if err != nil {
		return "", err
	}
	client := o.Conf.Client(oauth2.NoContext, o.Token)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result, err := ioutil.ReadAll(resp.Body)

	jsonParsed, err := gabs.ParseJSON(result)
	if err != nil {
		return "", err
	}
	url, _ := jsonParsed.Path("uploadUrl").Data().(string)
	return url, nil
}
