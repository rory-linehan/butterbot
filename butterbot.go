package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	log "github.com/sirupsen/logrus"
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
		Port    string
		Topic   string
		Timeout int
	}
	Status          bool
	Offset          int64     `default:"0"`
	lastMessageTime time.Time `default:"time.Now()"`
	Message         string
}

type KafkaEvent struct {
	Host   string
	Port   string
	Topic  string
	Events []struct {
		Name    string
		Key     string
		Filter  []map[string]string
		Extract []string
		Notify  []string
	}
	Offset int64 `default:"0"`
}

type Config struct {
	KafkaTimeout int `yaml:"kafkaTimeout"`
	Butterbot    struct {
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
	status          bool
	err             error
	offset          int64
	lastMessageTime time.Time
}

type EventResult struct {
	status  bool
	err     error
	message string
}

func getConfig(file string) Config {
	handle, err := os.Open(file)
	if err != nil {
		log.Fatalf("failed to open file: %s", err)
	}
	defer handle.Close()

	content, err := io.ReadAll(handle)
	if err != nil {
		log.Fatalf("failed to read file: %s", err)
	}

	c := Config{}
	err = yaml.Unmarshal(content, &c)
	if err != nil {
		log.Error(err)
	}

	return c
}

func executeTextWebhook(n *Notifier, message string) error {
	if n.Type == "discord" {
		message = "{\"content\":\"" + message + "\"}"
	} else {
		log.Warn("butterbot: notifier type is invalid, expecting [discord]")
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
			log.Error(
				"failed to execute ",
				n.Type,
				n.Name,
				" webhook: status: ",
				r.StatusCode,
				" response: ",
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

func HTTPChecker(check *HTTPCheck) HTTPCheckResult {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: check.Parameters.SkipVerify},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(check.Parameters.Timeout) * time.Second,
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
				message: "received response.StatusCode [" + fmt.Sprint(response.StatusCode) + "] other than configured: [" + fmt.Sprint(check.Parameters.Code) + "]",
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

func kafkaTopicChecker(check *KafkaTopicCheck, kafkaTimeout int) KafkaTopicResult {
	offset := check.Offset
	result := KafkaTopicResult{
		status:          true,
		err:             nil,
		offset:          offset,
		lastMessageTime: check.lastMessageTime,
	}

	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": check.Parameters.Host + ":" + check.Parameters.Port,
		"group.id":          "butterbot",
		"auto.offset.reset": "smallest",
	})
	if err != nil {
		log.Error(err)
	} else {
		err = consumer.SubscribeTopics([]string{check.Parameters.Topic}, nil)
		if err != nil {
			log.Error(err)
		} else {
			for {
				_, err := consumer.ReadMessage(time.Second * time.Duration(kafkaTimeout))
				if err != nil {
					break
				} else {
					partitions, err := consumer.Assignment()
					if err != nil {
						log.Errorf("failed to get assignment: %s", err)
					} else {
						offsets, err := consumer.Position(partitions)
						if err != nil {
							log.Errorf("failed to get positions: %s", err)
						} else {
							for _, offset := range offsets {
								result.offset = int64(offset.Offset)
							}
							result.lastMessageTime = time.Now()
						}
					}
				}
			}
		}
	}

	if err := consumer.Close(); err != nil {
		log.Info("error: failed to close reader: ", err)
		result.status = false
		result.err = err
	}

	return result
}

func kafkaEventChecker(consumer *kafka.Consumer, kafkaEvent *KafkaEvent, notifiers []Notifier, kafkaTimeout int) EventResult {
	result := EventResult{
		status:  false,
		err:     nil,
		message: "",
	}
	for {
		message, err := consumer.ReadMessage(time.Second * time.Duration(kafkaTimeout))
		if err != nil {
			break
		} else {
			result.status = false
			for _, event := range kafkaEvent.Events {
				if string(message.Key) == event.Key {
					result.message = event.Name + ": "
					msg := make(map[string]interface{})
					err := json.Unmarshal(message.Value, &msg)
					if err != nil {
						log.Error("failed to unmarshal json message: ", err)
					}
					valid := true
					for _, filter := range event.Filter {
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
					if valid {
						log.Info("found valid event: " + string(message.Value))
						// extract fields from kafka event and construct notification string
						for _, field := range event.Extract {
							result.message = result.message + field + ": " + fmt.Sprintf("%v ", msg[field])
						}
						result.status = true
						log.Info(result)
						err := notify(event.Notify, notifiers, result.message)
						if err != nil {
							log.Error(err)
						}
					}
				}
			}
		}
	}
	if err := consumer.Close(); err != nil {
		log.Error(" failed to close reader: ", err)
	}
	return result
}

func main() {
	logLevel := flag.String("log-level", "info", "log level")
	configFile := flag.String("config-file", "config.yaml", "config file location")
	flag.Parse()

	log.SetFormatter(&log.JSONFormatter{})
	log.Info("given log level: ", *logLevel)
	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("invalid log level: %s", err)
	}
	log.Info("started")
	log.SetLevel(level)

	log.Debug(*configFile)
	config := getConfig(*configFile)
	log.Debug(config)

	for {
		log.Debug("running HTTP checks")
		for index, check := range config.Butterbot.HTTPChecks {
			result := HTTPChecker(&check)
			if result.status && result.err == nil {
				if !check.Status {
					check.Message = check.Name + " is up"
					err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
					if err != nil {
						log.Error(err)
					}
					config.Butterbot.HTTPChecks[index].Status = true
				}
			} else if !result.status && result.err != nil {
				log.Debug(result.err)
				if check.Status {
					check.Message = check.Name + " is down"
					err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
					if err != nil {
						log.Error(err)
					}
					config.Butterbot.HTTPChecks[index].Status = false
				}
			} else {
				log.Error(check.Name, " check failed: ", result.message)
			}
		}

		log.Debug("running kafka topic checks")
		for index, check := range config.Butterbot.KafkaTopicChecks {
			result := kafkaTopicChecker(&check, config.KafkaTimeout)
			// has the check result state diverged from the previous?
			if result.lastMessageTime.Equal(check.lastMessageTime) && result.offset == check.Offset {
				// is the current time greater than the last message time plus configured timeout?
				if time.Now().After(check.lastMessageTime.Add(time.Duration(check.Parameters.Timeout) * time.Second)) {
					if check.Status {
						check.Message = check.Name + " is down"
						err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
						if err != nil {
							log.Error(err)
						}
						config.Butterbot.KafkaTopicChecks[index].Status = false
					}
				}
			} else {
				if !check.Status {
					check.Message = check.Name + " is up"
					err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
					if err != nil {
						log.Error(err)
					}
					config.Butterbot.KafkaTopicChecks[index].Status = true
				}
			}
			config.Butterbot.KafkaTopicChecks[index].Offset = result.offset
			config.Butterbot.KafkaTopicChecks[index].lastMessageTime = result.lastMessageTime
		}

		log.Debug("running kafka event checks")
		for _, event := range config.Butterbot.KafkaEvents {
			consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
				"bootstrap.servers": event.Host + ":" + event.Port,
				"group.id":          "butterbot",
				"auto.offset.reset": "smallest",
			})
			if err != nil {
				log.Error(err)
			} else {
				err = consumer.SubscribeTopics([]string{event.Topic}, nil)
				if err != nil {
					log.Error(err)
				} else {
					result := kafkaEventChecker(consumer, &event, config.Butterbot.Notifiers, config.KafkaTimeout)
					if result.err != nil {
						log.Error(result.err)
					}
				}
			}
		}
	}
}
