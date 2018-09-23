package main

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/net/publicsuffix"
)

var (
	dataPath           = os.Getenv("HOME") + "/.local/share/uva-cli"
	cookieFile         = dataPath + "/cookiejar.gob"
	trueProblemIDsFile = dataPath + "/true-problem-ids.gob"
	uvaURL, _          = url.Parse(baseURL)
)

const (
	baseURL = "https://uva.onlinejudge.org"
	green   = "\033[0;32m"
	yellow  = "\033[0;33m"
	gray    = "\033[1;30m"
	end     = "\033[0m"
)

func exists(file string) bool {
	_, err := os.Stat(file)
	return !os.IsNotExist(err)
}

func spin(text string) func() {
	dots := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	for i := 0; i < len(dots); i++ {
		dots[i] = fmt.Sprintf("%s%s%s", green, dots[i], end)
	}
	text = fmt.Sprintf("%s%s%s", gray, text, end)
	stop := make(chan struct{})
	done := make(chan struct{})
	fmt.Printf("%s %s", dots[0], text)
	go func() {
		for i := 1; ; i++ {
			select {
			case <-time.After(100 * time.Millisecond):
				fmt.Printf("\r%s %s", dots[i%len(dots)], text)
			case <-stop:
				fmt.Printf("\r%s\r", strings.Repeat(" ", len(text)+2))
				done <- struct{}{}
				return
			}
		}
	}()
	return func() {
		stop <- struct{}{}
		// Wait and make sure the spinner is erased.
		<-done
	}
}

func volumeToCategory(volume int) int {
	switch {
	case volume <= 9:
		return volume + 2
	case 10 <= volume && volume <= 12:
		return volume + 235
	case 13 <= volume && volume <= 15:
		return volume + 433
	case volume == 16:
		return 825
	case volume == 17:
		return 859
	}
	return -1
}

func crawlTrueProblemIDs() []int {
	defer spin("Downloading problem list")()
	const VOLUMES = 17
	resultChan := make(chan int)
	var wg sync.WaitGroup
	wg.Add(VOLUMES)
	for i := 1; i <= VOLUMES; i++ {
		go func(volume int) {
			category := volumeToCategory(volume)
			resp, err := http.Get(fmt.Sprintf("%s%s%s", baseURL,
				"/index.php?option=com_onlinejudge&Itemid=8&category=",
				strconv.Itoa(category)))
			if err != nil {
				panic(err)
			}
			doc, err := goquery.NewDocumentFromResponse(resp)
			doc.Find("#col3_content_wrapper > table:nth-child(4) > tbody > tr").
				Each(func(i int, s *goquery.Selection) {
					href, ok := s.Attr("href")
					if !ok {
						panic("Failed to query selector")
					}
					start := len(href) - 1
					for href[start-1] != '=' {
						start--
					}
					pid, err := strconv.Atoi(href[start:])
					if err != nil {
						panic("Failed when atoi")
					}
					resultChan <- pid
				})
			wg.Done()
		}(i)
	}
	go func() {
		wg.Wait()
		close(resultChan)
	}()
	var problemIDs []int
	for pid := range resultChan {
		problemIDs = append(problemIDs, pid)
	}
	sort.Ints(problemIDs)

	f, err := os.Create(trueProblemIDsFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	gob.NewEncoder(f).Encode(problemIDs)
	return problemIDs
}

func getTrueProblemID(pid int) int {
	var trueIDs []int
	if exists(trueProblemIDsFile) {
		f, err := os.Open(trueProblemIDsFile)
		if err != nil {
			panic(err)
		}
		if err := gob.NewDecoder(f).Decode(&trueIDs); err != nil {
			panic(err)
		}
	} else {
		trueIDs = crawlTrueProblemIDs()
	}
	return trueIDs[pid-100]
}

func doLogin(username, password string) error {
	defer spin("Signing in uva.onlinejudge.org")()
	resp, err := http.Get(baseURL)
	if err != nil {
		return err
	}
	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		return err
	}
	form := url.Values{}
	doc.Find("#mod_loginform > table > tbody > tr:nth-child(1) > td > input").
		Each(func(i int, s *goquery.Selection) {
			name, _ := s.Attr("name")
			value := s.AttrOr("value", "")
			form.Set(name, value)
		})
	form.Set("username", username)
	form.Set("passwd", password)
	r, err := http.PostForm(
		baseURL+"/index.php?option=com_comprofiler&task=login", form)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}
	const failed = "Incorrect username or password"
	if strings.Contains(string(body), failed) {
		return errors.New(failed)
	}
	return nil
}

func login(c *cli.Context) error {
	if !exists(dataPath) {
		if err := os.Mkdir(dataPath, 0755); err != nil {
			return err
		}
	}
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return err
	}
	http.DefaultClient.Jar = jar

	if !exists(cookieFile) {
		var username string
		fmt.Print("Username: ")
		fmt.Scanln(&username)
		fmt.Print("Password: ")
		password, err := terminal.ReadPassword(0)
		fmt.Print("\n")
		if err != nil {
			return err
		}
		if err := doLogin(username, string(password)); err != nil {
			return err
		}
		f, err := os.Create(cookieFile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := gob.NewEncoder(f).Encode(jar.Cookies(uvaURL)); err != nil {
			return err
		}
	} else {
		f, err := os.Open(cookieFile)
		if err != nil {
			return err
		}
		var cookies []*http.Cookie
		if err := gob.NewDecoder(f).Decode(&cookies); err != nil {
			return err
		}
		jar.SetCookies(uvaURL, cookies)
	}
	return nil
}

func submit(problemID int, file string) (string, error) {
	var category int = problemID / 100
	problemID = getTrueProblemID(problemID)
	form := url.Values{
		"problemid": {strconv.Itoa(problemID)},
		"category":  {strconv.Itoa(category)},
		"language":  {"3"}, // TODO
	}
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	code, err := ioutil.ReadAll(f)
	if err != nil {
		return "", err
	}
	form.Set("codeupl", string(code))

	// Prevent HTTP 301 redirect
	http.DefaultClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { http.DefaultClient.CheckRedirect = nil }()
	defer spin("Sending code to judge")()
	resp, err := http.PostForm(baseURL+
		"/index.php?option=com_onlinejudge&Itemid=8&page=save_submission", form)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	location := resp.Header["Location"][0]
	start := len(location) - 1
	for location[start-1] != '+' {
		start--
	}
	submitID := location[start:]
	return submitID, nil
}

func main() {
	app := cli.NewApp()
	app.Usage = "A cli tool to enjoy uva oj!"
	app.UsageText = "uva [command]"
	app.Commands = []cli.Command{
		{
			Name:    "login",
			Aliases: []string{"l"},
			Usage:   "login to uva oj",
			Action:  login,
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
	}
}