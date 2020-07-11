package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"time"
)

type Notifier struct {
	Name        string
	Url         string
	ContentType string `yaml:"content_type"`
	StatusCode  int    `yaml:"status_code"`
}

type Check struct {
	Name       string
	Parameters struct {
		Method     string
		Verb       string
		Url        string
		Code       int
		SkipVerify bool `yaml:"skip_verify"`
		Timeout    int
	}
	Status  bool
	Message string
}

type Config struct {
	Butterbot struct {
		Notifiers []Notifier
		Checks    []Check
	}
}

type CheckResult struct {
	status  bool
	message string
	error   error
}

func logError(error error) {
	fmt.Println("butterbot: error:", error)
}

func getConfig() Config {
	c := Config{}
	yamlFile, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		logError(err)
	}
	err = yaml.Unmarshal(yamlFile, &c)
	if err != nil {
		logError(err)
	}
	return c
}

func HTTPChecker(c *Config, check int) CheckResult {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.Butterbot.Checks[check].Parameters.SkipVerify},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Second,
	}
	if c.Butterbot.Checks[check].Parameters.Verb == "get" {
		response, err := client.Get(c.Butterbot.Checks[check].Parameters.Url)
		if err != nil {
			return CheckResult{
				status:  false,
				message: "HTTP GET request failed",
				error:   err,
			}
		}
		defer response.Body.Close()
		if response.StatusCode == c.Butterbot.Checks[check].Parameters.Code {
			return CheckResult{
				status:  true,
				message: "HTTP GET request succeeded",
				error:   nil,
			}
		} else {
			return CheckResult{
				status:  false,
				message: "received response.StatusCode [" + string(response.StatusCode) + "] other than configured: [" + string(c.Butterbot.Checks[check].Parameters.Code) + "]",
				error:   err,
			}
		}
	} else {
		return CheckResult{
			status:  false,
			message: "HTTP check verb is invalid, received [" + c.Butterbot.Checks[check].Parameters.Verb + "], expecting [get]",
			error:   nil,
		}
	}
}

func executeTextWebhook(n Notifier, message string) error {
	// TODO: can this be abstracted?
	if n.Name == "discord" {
		message = "{\"content\":\"" + message + "\"}"
	} else {
		fmt.Println("botterbot: warn: notifier name is invalid, expecting [discord]")
		return nil
	}
	r, err := http.Post(n.Url, n.ContentType, bytes.NewBufferString(message))
	if err != nil {
		return err
	} else {
		if r.StatusCode != n.StatusCode {
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(r.Body) // this could be problematic
			response := buf.String()
			fmt.Println(
				"butterbot: error: failed to execute",
				n.Name,
				"webhook: status:",
				r.StatusCode,
				"response:",
				response)
		}
		return nil
	}
}

func notify(config *Config, message string) error {
	for index, _ := range config.Butterbot.Notifiers {
		// this may be abstracted to other types of more
		// complex webhooks for notifiers
		err := executeTextWebhook(
			config.Butterbot.Notifiers[index],
			message)
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	fmt.Println("butterbot: info: started")
	config := getConfig()
	fmt.Println("butterbot: info: loaded config.yaml:")
	spew.Dump(config)
	for {
		for index, check := range config.Butterbot.Checks {
			result := CheckResult{
				status:  false,
				message: "",
				error:   nil,
			}
			if check.Parameters.Method == "http" {
				result = HTTPChecker(&config, index)
			}
			if result.status == true && result.error == nil {
				if config.Butterbot.Checks[index].Status == false {
					config.Butterbot.Checks[index].Message = config.Butterbot.Checks[index].Name + " is up"
					err := notify(&config, config.Butterbot.Checks[index].Message)
					if err != nil {
						logError(err)
					}
					config.Butterbot.Checks[index].Status = true
				}
			} else if result.status == false && result.error != nil {
				if config.Butterbot.Checks[index].Status == true {
					config.Butterbot.Checks[index].Message = config.Butterbot.Checks[index].Name + " is down"
					err := notify(&config, config.Butterbot.Checks[index].Message)
					if err != nil {
						logError(err)
					}
					config.Butterbot.Checks[index].Status = false
				}
			} else {
				fmt.Println("butterbot: error:", check.Parameters.Method, "check failed:", result.message)
			}
			time.Sleep(1 * time.Second)
		}
	}
}
