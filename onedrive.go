package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jeffail/gabs"
	"github.com/pivotal-golang/bytefmt"
	"golang.org/x/oauth2"
)

const (
	api   = "https://api.onedrive.com/v1.0"
	chunk = 1024 * 1024 * 10
	mkdir = "{\"name\":\"%s\",\"folder\":{},\"@name.conflictBehavior\":\"%s\"}"
)

type onedriveItem struct {
	name   string
	size   int64
	count  int
	link   string
	path   string
	folder bool
	tmpUrl string
	hash   string
}

type jobItem struct {
	item  onedriveItem
	up    *onedrive
	upDir string
}

type onedrive struct {
	conf  *oauth2.Config
	token *oauth2.Token
	jobs  chan jobItem
	mutex *sync.Mutex
	wait  sync.WaitGroup
}

func NewOnedrive(conf *oauth2.Config, token *oauth2.Token) *onedrive {
	return &onedrive{conf: conf, token: token, mutex: &sync.Mutex{}}
}

func (o *onedrive) get(uri string) *http.Response {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		log.Fatal(err)
	}
	return resp
}

func (o *onedrive) submit(s string) (items []onedriveItem) {
	resp := o.get(s)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode >= 400 {
		return nil
	}
	jsonParsed, err := gabs.ParseJSON(body)
	if err != nil {
		log.Fatal(err)
	}

	children, err := jsonParsed.S("value").Children()
	if len(children) == 0 && err != nil {
		children = append(children, jsonParsed)
	}
	for _, child := range children {
		name := child.Path("name").Data().(string)
		size := child.Path("size").Data().(float64)
		count, ok := child.Path("folder.childCount").Data().(float64)
		if !ok {
			count = -1
		}
		folder := count > -1
		path, _ := child.Path("parentReference.path").Data().(string)
		link := path + "/" + name
		tmpUrl, _ := child.Search("@content.downloadUrl").Data().(string)
		hash, _ := child.Search("file.hashes.sha1Hash").Data().(string)

		item := onedriveItem{name, int64(size), int(count), link, path,
			folder, tmpUrl, hash}
		items = append(items, item)
	}
	more, ok := jsonParsed.Path("@odata.nextLink").Data().(string)
	if ok {
		items = append(items, o.submit(more)...)
	}
	return
}

func (o *onedrive) Drives() []onedriveItem {
	return o.submit(api + "/drive")
}

func (o *onedrive) Children(path string) []onedriveItem {
	return o.submit(api + "/drive/root:" + path + ":/children")
}

func (o *onedrive) Mkdir(path string) error {
	if path[0:1] == "/" {
		path = path[1:]
	}
	pathItems := strings.Split(path, "/")
	path = "/drive/root:"
	for _, pathItem := range pathItems {
		parentPath := path
		path += "/" + pathItem
		if !o.pathExists(path) {
			uri := api + parentPath + ":/children"
			body := fmt.Sprintf(mkdir, pathItem, "fail")
			buffer := strings.NewReader(body)
			req, err := http.NewRequest("POST", uri, buffer)
			if err != nil {
				return err
			}
			req.Header.Add("Content-Type", "application/json")
			resp, err := o.client().Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if err != nil || resp.StatusCode >= 400 {
				return err
			}
		}
	}
	return nil
}

func (o *onedrive) pathExists(path string) bool {
	result := o.submit(api + path)
	return result != nil && len(result) > 0 && result[0].folder
}

func (o *onedrive) startJobs(jobCount int) {
	o.jobs = make(chan jobItem)
	for i := 0; i < jobCount; i++ {
		go func() {
			o.wait.Add(1)
			for job := range o.jobs {
				for !o.syncFile(job.up, job.upDir, job.item) {
					fmt.Println("Upload-Error! Try again in 5 seconds")
					time.Sleep(5 * time.Second)
				}
			}
			o.wait.Done()
		}()
	}
}

func (o *onedrive) SyncWith(up *onedrive, downDir, upDir string, jobCount int) {
	items := o.Children(downDir)
	if len(items) == 0 {
		return
	}
	if jobCount > 0 {
		o.startJobs(jobCount)
	}
	err := up.Mkdir(upDir)
	if err != nil {
		log.Fatal("Can't create directory")
	}
	upItems := up.Children(upDir)

MAIN:
	for _, item := range items {
		if item.folder {
			fmt.Println("Directory:", item.name)
			o.SyncWith(up, downDir+"/"+item.name, upDir+"/"+item.name, 0)
		} else {
			for _, upItem := range upItems {
				if upItem.name == item.name && upItem.size == item.size {
					if upItem.hash == item.hash {
						size := bytefmt.ByteSize(uint64(item.size))
						fmt.Println("Online:", item.name, size)
						continue MAIN
					}
				}
			}
			o.jobs <- jobItem{up: up, upDir: upDir, item: item}
		}
	}
	if jobCount > 0 {
		close(o.jobs)
		o.wait.Wait()
	}
}

func (o *onedrive) client() (client *http.Client) {
	o.mutex.Lock()
	client = o.conf.Client(oauth2.NoContext, o.token)
	o.mutex.Unlock()
	return
}

func (o *onedrive) syncFile(up *onedrive, upDir string, item onedriveItem) bool {
	var b bytes.Buffer
	var size int64 = 0
	buffer := make([]byte, chunk)

	resp := o.get(api + item.link + ":/content")
	defer resp.Body.Close()

	uploadUrl, err := up.createSession(item.name, upDir)
	if err != nil || len(uploadUrl) == 0 {
		fmt.Println("Session:", err, uploadUrl)
		return false
	}
	fmt.Println("Upload:", item.name, "-", bytefmt.ByteSize(uint64(item.size)))

	tries := 0
	for {
		if tries > 3 {
			fmt.Println("Too many tries")
			return false
		}
		num, err := io.ReadAtLeast(resp.Body, buffer, chunk)

		b.Reset()
		b.Write(buffer)
		b.Truncate(num)
		req, err2 := http.NewRequest("PUT", uploadUrl, &b)
		if err2 != nil {
			return false
		}

		r := fmt.Sprintf("bytes %d-%d/%d", size, size+int64(num)-1, item.size)
		req.Header.Add("Content-Length", fmt.Sprintf("%d", num))
		req.Header.Add("Content-Range", r)
		res, err2 := up.client().Do(req)
		if err2 != nil {
			return false
		}
		res.Body.Close()

		size += int64(num)
		from := bytefmt.ByteSize(uint64(size))
		to := bytefmt.ByteSize(uint64(item.size))
		fmt.Println(from, "/", to, "Status:", res.StatusCode, "Name:", item.name)
		if err != nil || res.StatusCode >= 400 {
			if item.size > size || res.StatusCode >= 400 {
				fmt.Println("\nError:", res.StatusCode, err, r, "\n")
				resp.Body.Close()
				resp, size = o.resume(up, uploadUrl, item)
				if size == 0 {
					return false
				}
				tries++
				continue
			}
			break
		}
		tries = 0
	}
	fmt.Println("Uploaded:", item.name)
	return true
}

func (o *onedrive) resume(up *onedrive, url string, item onedriveItem) (*http.Response, int64) {
	resp := up.get(url)
	body, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	re := regexp.MustCompile("\\[\\\"([0-9]*)\\-")
	pos := re.FindSubmatch(body)
	if pos == nil {
		return nil, 0
	}
	size, err := strconv.ParseInt(string(pos[1]), 10, 64)
	if err != nil || size == 0 {
		return nil, 0
	}

	fmt.Println("Resume:", size)

	links := o.submit(api + item.link)
	if links == nil || len(links) == 0 {
		return nil, 0
	}

	req, err := http.NewRequest("GET", links[0].tmpUrl, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", size, item.size-1))
	resp, err = o.client().Do(req)
	if err != nil {
		log.Fatal(err)
	}
	return resp, size
}

func (o *onedrive) createSession(name, dir string) (string, error) {
	uri := api + "/drive/root:" + dir + "/" + name + ":/upload.createSession"
	req, err := http.NewRequest("POST", uri, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	jsonParsed, err := gabs.ParseJSON(result)
	if err != nil {
		return "", err
	}
	url, _ := jsonParsed.Path("uploadUrl").Data().(string)
	return url, nil
}
