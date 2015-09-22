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
	"sync"
	"time"

	"github.com/Jeffail/gabs"
	"github.com/pivotal-golang/bytefmt"
	"golang.org/x/oauth2"
)

const (
	api   = "https://api.onedrive.com/v1.0"
	chunk = 1024 * 1024 * 10
)

type onedriveItem struct {
	name   string
	size   int64
	count  int
	link   string
	path   string
	folder bool
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
}

func NewOnedrive(conf *oauth2.Config, token *oauth2.Token) *onedrive {
	return &onedrive{conf: conf, token: token, mutex: &sync.Mutex{}}
}

func (o *onedrive) get(uri, r string) *http.Response {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		log.Fatal(err)
	}
	if len(r) > 0 {
		req.Header.Add("Range", r)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		log.Fatal(err)
	}
	return resp
}

func (o *onedrive) submit(s string) (items []onedriveItem) {
	resp := o.get(api+s, "")
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
		path, _ := child.Path("parentReference.path").Data().(string)
		link := api + path + "/" + name + ":/content"

		item := onedriveItem{name, int64(size), int(count), link, path, count > -1}
		items = append(items, item)
	}
	return
}

func (o *onedrive) Drives() []onedriveItem {
	return o.submit("/drive")
}

func (o *onedrive) Children(path string) []onedriveItem {
	return o.submit("/drive/root:" + path + ":/children")
}

func (o *onedrive) Mkdir(path string) {
}

func (o *onedrive) startJobs(jobCount int) {
	o.jobs = make(chan jobItem)
	for i := 0; i < jobCount; i++ {
		go func() {
			for job := range o.jobs {
				for !o.syncFile(job.up, job.upDir, job.item) {
					fmt.Println("Upload-Error! Try again in 5 seconds")
					time.Sleep(5 * time.Second)
				}
			}
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
	up.Mkdir(upDir) // TODO
	upItems := up.Children(upDir)
	for _, item := range items {
		if item.folder {
			fmt.Println("Todo: Call SyncWith with new folder")
		} else {
			if item.size == 0 {
				continue
			}
			found := false
			for _, upItem := range upItems {
				if upItem.name == item.name && upItem.size == item.size {
					found = true
				}
			}
			if found {
				fmt.Println("Online:", item.name, bytefmt.ByteSize(uint64(item.size)))
				continue
			}
			o.jobs <- jobItem{up: up, upDir: upDir, item: item}
		}
	}
	if jobCount > 0 {
		close(o.jobs)
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

	resp := o.get(item.link, "")
	defer resp.Body.Close()

	uploadUrl, err := up.createSession(item.name, upDir)
	if err != nil || len(uploadUrl) == 0 {
		fmt.Println("Session:", err, uploadUrl)
		return false
	}
	fmt.Println("Upload:", item.name, "-", bytefmt.ByteSize(uint64(item.size)), resp)

	tries := 0
	for {
		if tries > 3 {
			fmt.Println("Too many tries")
			return false
		}
		num, err := io.ReadAtLeast(resp.Body, buffer, chunk)
		if num == 0 {
			fmt.Println("Num: 0")
			return false
		}

		b.Reset()
		b.Write(buffer)
		b.Truncate(num)
		req, err2 := http.NewRequest("PUT", uploadUrl, &b)
		if err2 != nil {
			fmt.Println("PUT:", err2)
			return false
		}

		r := fmt.Sprintf("bytes %d-%d/%d", size, size+int64(num)-1, item.size)
		req.Header.Add("Content-Length", fmt.Sprintf("%d", num))
		req.Header.Add("Content-Range", r)
		res, err2 := up.client().Do(req)
		if err2 != nil {
			fmt.Println("Do:", err2)
			return false
		}
		body, _ := ioutil.ReadAll(res.Body)
		res.Body.Close()

		size += int64(num)
		from := bytefmt.ByteSize(uint64(size))
		to := bytefmt.ByteSize(uint64(item.size))
		fmt.Println(from, "/", to, "Status:", res.StatusCode, "Name:", item.name)
		if err != nil || res.StatusCode >= 400 {
			if item.size > size || res.StatusCode >= 400 {
				fmt.Println("Error:", res.StatusCode, err, string(body), r)
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
	fmt.Println(item.name, "uploaded!")
	return true
}

func (o *onedrive) resume(up *onedrive, url string, item onedriveItem) (*http.Response, int64) {
	resp := up.get(url, "")
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

	fmt.Println("Resume-size:", size)
	resp = o.get(item.link, fmt.Sprintf("bytes=%d-%d", size, item.size-1))
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
