package main

import (
	"fmt"
	"io"
	"log" //nolint:revive
	"net/http"
	"os"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

type Updater struct {
	FileName string
	Regex    string
	Replace  string
}

func (u Updater) Update() error {
	b, err := os.ReadFile(u.FileName)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(u.Regex)
	newContents := re.ReplaceAll(b, []byte(u.Replace))

	err = os.WriteFile(u.FileName, newContents, 0664)
	if err != nil {
		return err
	}

	return nil
}

// findHash will search the downloads table for the hashes matching the artifacts list
func findHashes(body io.Reader, version string) (map[string]string, error) {
	htmlTokens := html.NewTokenizer(body)
	artifacts := []string{
		fmt.Sprintf("go%s.linux-amd64.tar.gz", version),
		fmt.Sprintf("go%s.darwin-arm64.tar.gz", version),
		fmt.Sprintf("go%s.darwin-amd64.tar.gz", version),
	}

	var insideDownloadTable bool
	var currentRow string
	hashes := make(map[string]string)

	for {
		tokenType := htmlTokens.Next()

		//if it's an error token, we either reached
		//the end of the file, or the HTML was malformed
		if tokenType == html.ErrorToken {
			err := htmlTokens.Err()
			if err == io.EOF {
				//end of the file, break out of the loop
				break
			}
			return nil, htmlTokens.Err()
		}

		if tokenType == html.StartTagToken {
			//get the token
			token := htmlTokens.Token()
			if "table" == token.Data && len(token.Attr) == 1 && token.Attr[0].Val == "downloadtable" {
				insideDownloadTable = true
			}

			if insideDownloadTable && token.Data == "a" && len(token.Attr) == 2 {
				for _, f := range artifacts {
					// Check if the current row matches a desired file
					if strings.Contains(token.Attr[1].Val, f) {
						currentRow = f
						break
					}
				}
			}

			if currentRow != "" && token.Data == "tt" {
				//the next token should be the page title
				tokenType = htmlTokens.Next()
				//just make sure it's actually a text token
				if tokenType == html.TextToken {
					hashes[currentRow] = htmlTokens.Token().Data
					currentRow = ""
				}
			}
		}

		// Found a hash for each filename
		if len(hashes) == len(artifacts) {
			break
		}

		// Reached end of table
		if tokenType == html.EndTagToken && htmlTokens.Token().Data == "table" {
			return nil, fmt.Errorf("only found %d hashes expected %d: %v", len(hashes), len(artifacts), hashes)
		}
	}

	return hashes, nil
}

func getHashes(version string) (map[string]string, error) {
	resp, err := http.Get(`https://go.dev/dl/`)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return findHashes(resp.Body, version)
}

func main() {
	version := os.Args[1]

	hashes, err := getHashes(version)
	if err != nil {
		log.Panic(err)
	}

	updates := []Updater{
		{
			FileName: ".circleci/config.yml",
			Regex:    `(quay\.io\/influxdb\/telegraf-ci):(\d.\d*.\d)`,
			Replace:  fmt.Sprintf("$1:%s", version),
		},
		{
			FileName: "Makefile",
			Regex:    `(quay\.io\/influxdb\/telegraf-ci):(\d.\d*.\d)`,
			Replace:  fmt.Sprintf("$1:%s", version),
		},
		{
			FileName: "scripts/ci.docker",
			Regex:    `(FROM golang):(\d.\d*.\d)`,
			Replace:  fmt.Sprintf("$1:%s", version),
		},
		{
			FileName: "scripts/installgo_linux.sh",
			Regex:    `(GO_VERSION)=("\d.\d*.\d")`,
			Replace:  fmt.Sprintf("$1=\"%s\"", version),
		},
		{
			FileName: "scripts/installgo_mac.sh",
			Regex:    `(GO_VERSION)=("\d.\d*.\d")`,
			Replace:  fmt.Sprintf("$1=\"%s\"", version),
		},
		{
			FileName: "scripts/installgo_windows.sh",
			Regex:    `(GO_VERSION)=("\d.\d*.\d")`,
			Replace:  fmt.Sprintf("$1=\"%s\"", version),
		},
		{
			FileName: "scripts/installgo_linux.sh",
			Regex:    `(GO_VERSION_SHA)=".*"`,
			Replace:  fmt.Sprintf("$1=\"%s\"", hashes[fmt.Sprintf("go%s.linux-amd64.tar.gz", version)]),
		},
		{
			FileName: "scripts/installgo_mac.sh",
			Regex:    `(GO_VERSION_SHA_arm64)=".*"`,
			Replace:  fmt.Sprintf("$1=\"%s\"", hashes[fmt.Sprintf("go%s.darwin-arm64.tar.gz", version)]),
		},
		{
			FileName: "scripts/installgo_mac.sh",
			Regex:    `(GO_VERSION_SHA_amd64)=".*"`,
			Replace:  fmt.Sprintf("$1=\"%s\"", hashes[fmt.Sprintf("go%s.darwin-amd64.tar.gz", version)]),
		},
	}

	for _, u := range updates {
		err := u.Update()
		if err != nil {
			log.Panic(err)
		}
	}
}
