package main

import (
	"context"
	"github.com/segmentio/kafka-go"
	"reflect"
	"testing"
)

type MockReader struct {
	mockMsg kafka.Message
	mockErr error
}

func (m *MockReader) ReadMessage(ctx context.Context) (kafka.Message, error) {
	return m.mockMsg, m.mockErr
}

func (m *MockReader) Close() error {
	return nil
}

func TestHTTPChecker(t *testing.T) {
	type args struct {
		check *HTTPCheck
	}
	tests := []struct {
		name string
		args args
		want HTTPCheckResult
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTTPChecker(tt.args.check); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("HTTPChecker() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_executeTextWebhook(t *testing.T) {
	type args struct {
		n       *Notifier
		message string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := executeTextWebhook(tt.args.n, tt.args.message); (err != nil) != tt.wantErr {
				t.Errorf("executeTextWebhook() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_getConfig(t *testing.T) {
	type args struct {
		file string
	}
	tests := []struct {
		name string
		args args
		want Config
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getConfig(tt.args.file); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_kafkaEventChecker(t *testing.T) {
	mockReader := &MockReader{
		mockMsg: kafka.Message{
			Partition:     0,
			Offset:        0,
			HighWaterMark: 0,
			Key:           []byte("testKey"),
			Value:         []byte("{\"filterField\":\"filterFieldValue\",\"extractField\":[\"extractValueElement1\",\"extractValueElement2\"],\"otherExtractField\":\"otherExtractValue\"}"),
			Topic:         "testTopic",
		},
		mockErr: nil,
	}

	type args struct {
		event *KafkaEvent
	}

	tests := []struct {
		name string
		args args
		want EventResult
	}{
		{
			name: "validEvent",
			args: args{
				event: &KafkaEvent{
					Host:   "testKafkaHost",
					Port:   "9092",
					Topic:  "testTopic",
					Notify: nil,
					Events: []struct {
						Name    string
						Key     string
						Filter  []map[string]string
						Extract []string
					}{
						{
							Name: "validEvent",
							Key:  "testKey",
							Filter: []map[string]string{
								{
									"filterField": "filterFieldValue",
								},
							},
							Extract: []string{
								"extractField",
								"otherExtractField",
							},
						},
					},
					Offset: 0,
				},
			},
			want: EventResult{
				status:  true,
				err:     nil,
				message: "validEvent: extractField: [extractValueElement1 extractValueElement2] otherExtractField: otherExtractValue ",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kafkaEventChecker(mockReader, tt.args.event); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("kafkaEventChecker() = %v, want %v", got, tt.want)
			} else {
				return
			}
		})
	}
}

func Test_kafkaTopicChecker(t *testing.T) {
	type args struct {
		check *KafkaTopicCheck
	}
	tests := []struct {
		name string
		args args
		want KafkaTopicResult
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kafkaTopicChecker(tt.args.check); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("kafkaTopicChecker() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_notify(t *testing.T) {
	type args struct {
		checkNotifiers []string
		notifiers      []Notifier
		message        string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := notify(tt.args.checkNotifiers, tt.args.notifiers, tt.args.message); (err != nil) != tt.wantErr {
				t.Errorf("notify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
