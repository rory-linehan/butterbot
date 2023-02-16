package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/segmentio/kafka-go"
	"gopkg.in/yaml.v2"
)

type Notifier struct {
	Type        string
	Name        string
	Url         string
	ContentType string `yaml:"content_type"`
	StatusCode  int    `yaml:"status_code"`
}

type HTTPCheck struct {
	Name       string
	Notify     []string
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

type KafkaTopicCheck struct {
	Name       string
	Notify     []string
	Parameters struct {
		Host    string
		Topic   string
		Timeout int
	}
	Status      bool
	Offset      int64     `default:"0"`
	LastMessage time.Time `default:"time.Now()"`
	Message     string
}

type KafkaEvent struct {
	Name       string
	Notify     []string
	Parameters struct {
		Host    string
		Topic   string
		Key     string
		Filter  []map[string]string
		Extract []string
	}
	Offset int64 `default:"0"`
}

type Config struct {
	Butterbot struct {
		Notifiers        []Notifier
		HTTPChecks       []HTTPCheck
		KafkaTopicChecks []KafkaTopicCheck
		KafkaEvents      []KafkaEvent
	}
}

type HTTPCheckResult struct {
	status  bool
	message string
	err     error
}

type KafkaTopicResult struct {
	status      bool
	err         error
	offset      int64
	lastMessage time.Time
}

type EventResult struct {
	status  bool
	err     error
	message string
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

func HTTPChecker(check *HTTPCheck) HTTPCheckResult {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: check.Parameters.SkipVerify},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Second,
	}
	if check.Parameters.Verb == "get" {
		response, err := client.Get(check.Parameters.Url)
		if err != nil {
			return HTTPCheckResult{
				status:  false,
				message: "HTTP GET request failed",
				err:     err,
			}
		}
		defer response.Body.Close()
		if response.StatusCode == check.Parameters.Code {
			return HTTPCheckResult{
				status:  true,
				message: "HTTP GET request succeeded",
				err:     nil,
			}
		} else {
			return HTTPCheckResult{
				status:  false,
				message: "received response.StatusCode [" + string(response.StatusCode) + "] other than configured: [" + string(check.Parameters.Code) + "]",
				err:     err,
			}
		}
	} else {
		return HTTPCheckResult{
			status:  false,
			message: "HTTP check verb is invalid, received [" + check.Parameters.Verb + "], expecting [get]",
			err:     nil,
		}
	}
}

func kafkaTopicChecker(check *KafkaTopicCheck) KafkaTopicResult {
	offset := int64(check.Offset)
	result := KafkaTopicResult{
		status:      true,
		err:         nil,
		offset:      offset,
		lastMessage: check.LastMessage,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{check.Parameters.Host + ":9092"},
		Topic:     check.Parameters.Topic,
		Partition: 0,
		MaxBytes:  10e6, // 10MB
	})
	r.SetOffset(offset)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Second*1))
		_, err := r.ReadMessage(ctx)
		cancel()
		if err != nil {
			break
		} else {
			result.offset = r.Offset()
			result.lastMessage = time.Now()
		}
	}

	if err := r.Close(); err != nil {
		fmt.Println("butterbot: error: failed to close reader:", err)
		result.status = false
		result.err = err
	}

	return result
}

func kafkaEventChecker(event *KafkaEvent) EventResult {
	result := EventResult{
		status:  true,
		err:     nil,
		message: event.Name + ": ",
	}
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{event.Parameters.Host + ":9092"},
		Topic:   event.Parameters.Topic,
		GroupID: "butterbot",
	})

	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Second*1))
		message, err := r.ReadMessage(ctx)
		cancel()
		if err != nil {
			result.status = false
			break
		} else {
			if string(message.Key) == event.Parameters.Key {
				msg := make(map[string]interface{})
				err := json.Unmarshal(message.Value, &msg)
				if err != nil {
					fmt.Println("butterbot: error: failed to unmarshal json message:", err)
				}
				json.Unmarshal(message.Value, &msg)
				valid := true
				for _, filter := range event.Parameters.Filter {
					keys := make([]string, 0, len(filter))
					for k := range filter {
						keys = append(keys, k)
					}
					for _, key := range keys {
						if msg[key] != filter[key] {
							valid = false
						}
					}
				}
				if !valid {
					fmt.Println("invalid event: " + string(message.Value))
					result.status = false
				} else {
					fmt.Println("found valid event: " + string(message.Value))
					// extract fields from kafka event and construct notification string
					for _, field := range event.Parameters.Extract {
						result.message = result.message + ", " + field + ": " + msg[field].(string)
					}
				}
			}
		}
	}

	if err := r.Close(); err != nil {
		fmt.Println("butterbot: error: failed to close reader:", err)
	}

	return result
}

func executeTextWebhook(n *Notifier, message string) error {
	if n.Type == "discord" {
		message = "{\"content\":\"" + message + "\"}"
	} else {
		fmt.Println("botterbot: warn: notifier type is invalid, expecting [discord]")
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
				n.Type,
				n.Name,
				"webhook: status:",
				r.StatusCode,
				"response:",
				response,
			)
		}
		return nil
	}
}

func notify(checkNotifiers []string, notifiers []Notifier, message string) error {
	for _, checkNotify := range checkNotifiers {
		for _, notifier := range notifiers {
			if checkNotify == notifier.Name {
				err := executeTextWebhook(
					&notifier,
					message,
				)
				if err != nil {
					return err
				} else {
					return nil
				}
			}
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
		for index, check := range config.Butterbot.HTTPChecks {
			result := HTTPChecker(&check)
			if result.status && result.err == nil {
				if !check.Status {
					check.Message = check.Name + " is up"
					err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
					if err != nil {
						logError(err)
					}
					config.Butterbot.HTTPChecks[index].Status = true
				}
			} else if !result.status && result.err != nil {
				if check.Status {
					check.Message = check.Name + " is down"
					err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
					if err != nil {
						logError(err)
					}
					config.Butterbot.HTTPChecks[index].Status = false
				}
			} else {
				fmt.Println("butterbot: error:", check.Name, "check failed:", result.message)
			}
		}

		for index, check := range config.Butterbot.KafkaTopicChecks {
			result := kafkaTopicChecker(&check)
			// has the check result state diverged from the previous?
			if result.lastMessage.Equal(check.LastMessage) && result.offset == check.Offset {
				// is the current time greater than the last message time plus configured timeout?
				if time.Now().After(check.LastMessage.Add(time.Duration(check.Parameters.Timeout) * time.Second)) {
					if check.Status {
						check.Message = check.Name + " is down"
						err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
						if err != nil {
							logError(err)
						}
						config.Butterbot.KafkaTopicChecks[index].Status = false
					}
				}
			} else {
				if !check.Status {
					check.Message = check.Name + " is up"
					err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
					if err != nil {
						logError(err)
					}
					config.Butterbot.KafkaTopicChecks[index].Status = true
				}
			}
			config.Butterbot.KafkaTopicChecks[index].Offset = result.offset
			config.Butterbot.KafkaTopicChecks[index].LastMessage = result.lastMessage
		}

		for _, event := range config.Butterbot.KafkaEvents {
			result := kafkaEventChecker(&event)
			if result.status {
				fmt.Println(result)
				err := notify(event.Notify, config.Butterbot.Notifiers, result.message)
				if err != nil {
					logError(err)
				}
			} else if result.err != nil {
				logError(result.err)
			}
		}

		time.Sleep(1 * time.Second)
	}
}
