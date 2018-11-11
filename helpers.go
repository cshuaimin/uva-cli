package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

const (
	ansic = iota + 1
	java
	cpp
	pascal
	cpp11
	python3
)

func exists(file string) bool {
	_, err := os.Stat(file)
	return !os.IsNotExist(err)
}

func parseFilename(s string) (pid int, name string, ext string) {
	regex := regexp.MustCompile(`(\d+)\.([\w+-_]+)\.(\w+)`)
	match := regex.FindStringSubmatch(s)
	if len(match) != 4 {
		panic("filename pattern does not match")
	}
	pid, err := strconv.Atoi(match[1])
	if err != nil {
		panic(err)
	}
	name = string(match[2])
	ext = string(match[3])
	return
}

func (info problemInfo) getFilename(ext string) string {
	slug := strings.Replace(info.Title, " ", "-", -1)
	return fmt.Sprintf("%d.%s.%s", info.ID, slug, ext)
}

func download(url, file, msg string) {
	defer spin(msg)()
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	f, err := os.Create(file)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err = io.Copy(f, resp.Body); err != nil {
		panic(err)
	}
}

var config struct {
	Output string
	Answer string
	Diff   []string
	Test   map[string]struct {
		Compile, Run []string
	}
}

func loadConfig() {
	configFile := dataPath + "config.yml"
	if !exists(configFile) {
		download("https://github.com/cshuaimin/uva/raw/master/config.yml", configFile, "Downloading default config.yml")
	}
	f, err := os.Open(configFile)
	if err != nil {
		panic(err)
	}
	if err = yaml.NewDecoder(f).Decode(&config); err != nil {
		panic(err)
	}
	config.Diff = append(config.Diff, config.Answer)
	config.Diff = append(config.Diff, config.Output)
}

func renderCmd(cmd []string, sourceFile string) *exec.Cmd {
	if len(cmd) > 0 {
		for i, v := range cmd {
			if v == "{}" {
				cmd[i] = sourceFile
			}
		}
		return exec.Command(cmd[0], cmd[1:]...)
	}
	return nil
}
