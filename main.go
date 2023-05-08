package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

func multiDownload(urlx, filename string) error {
	var (
		wg           sync.WaitGroup
		downloadSize int64
		downloaded   int64
	)

	// Get file size
	resp, err := http.Head(urlx)
	if err != nil {
		return err
	}
	downloadSize = resp.ContentLength

	// Create file to write to
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Set up concurrent downloads
	numWorkers := 5
	rangeSize := downloadSize / int64(numWorkers)
	for i := 0; i < numWorkers; i++ {
		start := rangeSize * int64(i)
		end := start + rangeSize - 1
		if i == numWorkers-1 {
			end = downloadSize - 1
		}

		wg.Add(1)
		go func(start, end int64) {
			defer wg.Done()

			req, err := http.NewRequest("GET", urlx, nil)
			if err != nil {
				fmt.Println("Error creating request:", err)
				return
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Println("Error downloading file:", err)
				return
			}
			defer resp.Body.Close()

			buf := make([]byte, 1024*1024) // 1MB buffer
			for {
				n, err := resp.Body.Read(buf)
				if err == io.EOF {
					break
				}
				if err != nil {
					fmt.Println("Error downloading file:", err)
					return
				}

				// Write to file
				n, err = file.WriteAt(buf[:n], start+downloaded)
				if err != nil {
					fmt.Println("Error writing to file:", err)
					return
				}
				downloaded += int64(n)

				// Print progress
				fmt.Printf("\rDownloading... %.2f%%\n", float64(downloaded)/float64(downloadSize)*100)
			}
		}(start, end)
	}

	wg.Wait()
	fmt.Println("\nDownload complete!")
	return nil
}
