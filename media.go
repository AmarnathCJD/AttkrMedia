package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const MEDIA_HOST = "https://www1.attacker.tv/"

type MediaBrowser struct {
	client *http.Client
}

func NewMediaBrowser() *MediaBrowser {
	return &MediaBrowser{client: &http.Client{Timeout: 10 * time.Second}}
}

func (mb *MediaBrowser) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/74.0.3729.169 Safari/537.36")
	return mb.client.Do(req)
}

type Media struct {
	Title    string `json:"title,omitempty"`
	MediaID  string `json:"media_id,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Year     string `json:"year,omitempty"`
	Duration string `json:"duration,omitempty"`
	Type     string `json:"type,omitempty"`
}

func (mb *MediaBrowser) SearchMedia(search string) ([]*Media, error) {
	req, _ := http.NewRequest("POST", MEDIA_HOST+"ajax/search", strings.NewReader("keyword="+url.QueryEscape(search)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	resp, err := mb.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}

	var media []*Media
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	doc.Find(".nav-item").Each(func(i int, s *goquery.Selection) {
		media_url := strings.Split(s.AttrOr("href", ""), "-")
		info_div := s.Find("div.film-infor")
		var m = &Media{
			Title:    s.Find("h3.film-name").Text(),
			MediaID:  media_url[len(media_url)-1],
			Poster:   s.Find("img").AttrOr("src", ""),
			Year:     info_div.Find("span").Eq(0).Text(),
			Duration: info_div.Find("span").Eq(1).Text(),
			Type:     info_div.Find("span").Eq(2).Text(),
		}

		if m.Title != "" {
			media = append(media, m)
		}
	})

	return media, nil
}

type MediaServer struct {
	Name    string `json:"name,omitempty"`
	MediaID string `json:"media_id,omitempty"`
}

func (m *Media) GetMediaServers() ([]*MediaServer, error) {
	req, _ := http.NewRequest("GET", MEDIA_HOST+"ajax/movie/episodes/"+m.MediaID, nil)
	if m.Type == "series" {
		req, _ = http.NewRequest("GET", MEDIA_HOST+"ajax/tv/episodes/"+m.MediaID, nil)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("getMediaServers status code: %d", resp.StatusCode)
	}
	var servers []*MediaServer
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	doc.Find(".nav-item").Each(func(i int, s *goquery.Selection) {
		var server = &MediaServer{
			Name:    strings.TrimSpace(s.Find("a").Text()),
			MediaID: strings.TrimSpace(s.Find("a").AttrOr("data-linkid", "")),
		}
		if server.Name != "" {
			servers = append(servers, server)
		}
	})
	return servers, nil
}

func (ms *MediaServer) GetEmbedURL() (string, error) {
	req, _ := http.NewRequest("GET", MEDIA_HOST+"ajax/sources/"+ms.MediaID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("getEmbedURL status code: %d", resp.StatusCode)
	}
	var embedData struct {
		Link string `json:"link"`
	}
	err = json.NewDecoder(resp.Body).Decode(&embedData)
	if err != nil {
		return "", err
	}

	return embedData.Link, nil
}

func (ms *MediaServer) DownloadURL(embed_url string) (string, error) {
	vidID := strings.Split(embed_url, "/")[4]
	vxURL := "https://rabbitstream.net/embed/m-download/" + vidID

	headReq, err := http.NewRequest("HEAD", vxURL, nil)
	if err != nil {
		return "", err
	}
	headResp, err := http.DefaultClient.Do(headReq)
	if err != nil {
		return "", err
	}
	finalURL, err := http.Get(headResp.Request.URL.String())
	if err != nil {
		return "", err
	}

	// Regex for https://streamlare.com/v/4RLg7lX31oJz2QAe
	rvc := regexp.MustCompile(`https://streamlare.com/v/.*`)
	body, err := io.ReadAll(finalURL.Body)
	if err != nil {
		return "", err
	}
	match := rvc.FindString(string(body))
	match = strings.Split(match, `"`)[0]
	vxID := strings.Split(match, "/")[4]

	postBody := strings.NewReader(`{"id":"` + vxID + `"}`)
	postResp, err := http.Post("https://slmaxed.com/api/video/download/get", "application/json", postBody)
	if err != nil {
		return "", err
	}

	var spt struct {
		Result struct {
			P1080 struct {
				URL string `json:"url"`
			} `json:"1080p"`
		} `json:"result"`
	}
	if err := json.NewDecoder(postResp.Body).Decode(&spt); err != nil {
		return "", err
	}
	downloadURL := spt.Result.P1080.URL
	return downloadURL, nil
}

func download(url, filename string) error {
	// chunks of 5MB
	fmt.Println("Downloading", filename, "In chunks of 5MB")
	total, err := getTotalBytes(url)
	if err != nil {
		return err
	}
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download status code: %d", resp.StatusCode)
	}
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	go func() {
		for {
			c, _ := f.Stat()
			fmt.Println(fmt.Printf("\rDownloaded %dMB of %dMB (%.2f%%)", c.Size()/1024/1024, total/1024/1024, calcPerc(total, c.Size())))
			time.Sleep(time.Second * 5)
		}
	}()
	_, err = io.Copy(f, resp.Body)
	fmt.Println("Downloaded", filename)
	return err
}

func calcPerc(total, current int64) float64 {
	return float64(current) / float64(total) * 100
}

func multigoruotinedownload(url, filename string) error {
	numGoRoutines := 10
	total, err := getTotalBytes(url)
	os.Create(filename)
	if err != nil {
		return err
	}
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	chunkSize := total / int64(numGoRoutines)
	var wg sync.WaitGroup
	for i := 0; i < numGoRoutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := int64(i) * chunkSize
			end := start + chunkSize
			if i == numGoRoutines-1 {
				end = total
			}
			fmt.Println("Downloading", filename, "from", start, "to", end)
			err := downloadChunk(url, filename, start, end)
			if err != nil {
				panic(err)
			}
		}(i)
	}
	go func() {
		for {
			c, _ := os.Stat(filename)
			fmt.Println(fmt.Printf("\rDownloaded %dMB of %dMB (%.2f%%)", c.Size()/1024/1024, total/1024/1024, calcPerc(total, c.Size())))
			time.Sleep(time.Second * 5)
		}
	}()
	wg.Wait()
	return nil
}

func downloadChunk(url, filename string, start, end int64) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	rangeHeader := fmt.Sprintf("bytes=%d-%d", start, end)
	req.Header.Set("Range", rangeHeader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(filename, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Seek(start, io.SeekStart)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	return nil
}

func getTotalBytes(url string) (int64, error) {
	resp, err := http.Head(url)
	if err != nil {
		return 0, err
	}
	fmt.Println(resp.Header)
	return resp.ContentLength, nil
}

func main() {
	mb := NewMediaBrowser()
	media, err := mb.SearchMedia("Lucifer")
	if err != nil {
		panic(err)
	}

	m := media[0]
	s, _ := m.GetMediaServers()
	for _, server := range s {
		if server.Name == "Vidcloud" {
			embedURL, _ := server.GetEmbedURL()
			downloadURL, _ := server.DownloadURL(embedURL)
			if err := multiDownload(downloadURL, m.Title+".mp4"); err != nil {
				panic(err)
			}
			break
		}
	}
}
