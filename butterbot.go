package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
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
	Status      bool
	Offset      int64     `default:"0"`
	LastMessage time.Time `default:"time.Now()"`
	Message     string
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
	notify  []string
}

type MessageReader interface {
	ReadMessage(ctx context.Context) (kafka.Message, error)
	Close() error
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
			log.Debug(err)
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

func kafkaTopicChecker(check *KafkaTopicCheck) KafkaTopicResult {
	offset := int64(check.Offset)
	result := KafkaTopicResult{
		status:      true,
		err:         nil,
		offset:      offset,
		lastMessage: check.LastMessage,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{check.Parameters.Host + ":" + check.Parameters.Port},
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
		log.Info("error: failed to close reader:", err)
		result.status = false
		result.err = err
	}

	return result
}

func kafkaEventChecker(reader MessageReader, kafkaEvent *KafkaEvent) EventResult {
	result := EventResult{
		status:  false,
		err:     nil,
		message: "",
		notify:  []string{},
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Second*1))
		message, err := reader.ReadMessage(ctx)
		cancel()
		if err != nil {
			break
		} else {
			for _, event := range kafkaEvent.Events {
				if string(message.Key) == event.Key {
					result.message = event.Name + ": "
					result.notify = event.Notify
					msg := make(map[string]interface{})
					err := json.Unmarshal(message.Value, &msg)
					if err != nil {
						log.Info("error: failed to unmarshal json message:", err)
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
						break
					}
				}
			}
			if result.status {
				break
			}
		}
	}
	if err := reader.Close(); err != nil {
		log.Info("error: failed to close reader:", err)
	}
	return result
}

func executeTextWebhook(n *Notifier, message string) error {
	if n.Type == "discord" {
		message = "{\"content\":\"" + message + "\"}"
	} else {
		log.Info("botterbot: warn: notifier type is invalid, expecting [discord]")
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
			log.Info(
				"error: failed to execute",
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
				if check.Status {
					check.Message = check.Name + " is down"
					err := notify(check.Notify, config.Butterbot.Notifiers, check.Message)
					if err != nil {
						log.Error(err)
					}
					config.Butterbot.HTTPChecks[index].Status = false
				}
			} else {
				log.Info("error:", check.Name, "check failed:", result.message)
			}
		}

		log.Debug("running kafka topic checks")
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
			config.Butterbot.KafkaTopicChecks[index].LastMessage = result.lastMessage
		}

		log.Debug("running kafka event checks")
		for _, event := range config.Butterbot.KafkaEvents {
			reader := kafka.NewReader(kafka.ReaderConfig{
				Brokers: []string{event.Host + ":" + event.Port},
				Topic:   event.Topic,
				GroupID: "butterbot",
			})
			result := kafkaEventChecker(reader, &event)
			if result.status {
				log.Info(result)
				err := notify(result.notify, config.Butterbot.Notifiers, result.message)
				if err != nil {
					log.Error(err)
				}
			} else if result.err != nil {
				log.Error(result.err)
			}
		}

		time.Sleep(1 * time.Second)
	}
}
