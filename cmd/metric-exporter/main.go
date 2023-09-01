package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/segmentio/kafka-go"
	"k8s.io/apimachinery/pkg/util/json"

	"github.com/nais/kube-event-relay/pkg/event"
	kafka2 "github.com/nais/kube-event-relay/pkg/kafka"
	"github.com/nais/kube-event-relay/pkg/metrics"
)

type Config struct {
	Kafka          KafkaConfig `envconfig:"KAFKA"`
	MetricsAddress string      `envconfig:"METRICS_ADDRESS" default:"127.0.0.1:8675"`
	Cluster        string      `envconfig:"NAIS_CLUSTER_NAME" required:"true"`
}

type KafkaConfig struct {
	// These four environment variables are injected into the pod by NAIS
	CaPath          string   `envconfig:"CA_PATH" required:"true"`
	CertificatePath string   `envconfig:"CERTIFICATE_PATH" required:"true"`
	PrivateKeyPath  string   `envconfig:"PRIVATE_KEY_PATH" required:"true"`
	Brokers         []string `envconfig:"BROKERS" required:"true"`

	// These must be configured manually
	Topic       string        `envconfig:"TOPIC" required:"true"`
	GroupID     string        `envconfig:"GROUP_ID"`
	DialTimeout time.Duration `envconfig:"DIAL_TIMEOUT" default:"5s"`
}

func main() {
	err := run()
	if err != nil {
		fmt.Printf("exiting because of error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := &Config{}
	err := envconfig.Process("", cfg)
	if err != nil {
		envconfig.Usage("", cfg)
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT)
	defer cancel()

	kafkaOpts := kafka2.ReaderOpts{
		Brokers:         cfg.Kafka.Brokers,
		CaPath:          cfg.Kafka.CaPath,
		CertificatePath: cfg.Kafka.CertificatePath,
		DialTimeout:     cfg.Kafka.DialTimeout,
		GroupID:         cfg.Kafka.GroupID,
		PrivateKeyPath:  cfg.Kafka.PrivateKeyPath,
		Topic:           cfg.Kafka.Topic,
	}
	fmt.Printf("Kubernetes event prometheus exporter\n")
	fmt.Printf("Configuration: %#v\n", kafkaOpts)
	kafkaReader, err := kafka2.NewReader(kafkaOpts)
	if err != nil {
		return err
	}

	fmt.Printf("Kafka reader is configured.\n")

	defer func() {
		e := kafkaReader.Close()
		if e != nil {
			fmt.Printf("close Kafka reader: %s\n", e)
		} else {
			fmt.Printf("Kafka reader closed successfully.\n")
		}
	}()

	go func() {
		fmt.Printf("Starting metrics server on %s...\n", cfg.MetricsAddress)
		metricsHandler := metrics.Handler()
		e := http.ListenAndServe(cfg.MetricsAddress, metricsHandler)
		cancel()
		if e == http.ErrServerClosed {
			return
		}
		fmt.Printf("metrics handler: %s\n", e)
	}()

	ingestMessage := func(msg kafka.Message) error {
		payload := &Payload{}
		err = json.Unmarshal(msg.Value, payload)
		if err != nil {
			return fmt.Errorf("offset %08d: parse message data: %w", msg.Offset, err)
		}

		if payload.Cluster != cfg.Cluster {
			return nil
		}

		kubeEvent := &event.KubeEvent{}
		err = json.Unmarshal([]byte(payload.Message), kubeEvent)
		if err != nil {
			return fmt.Errorf("offset %08d: parse Kubernetes event: %w", msg.Offset, err)
		}

		metrics.ReportEvent(*kubeEvent)

		return nil
	}

	fmt.Printf("Processing messages...\n")

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return nil
		default:
			var msg kafka.Message
			msg, err = kafkaReader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() == nil {
					return err
				}
				return nil
			}
			err = ingestMessage(msg)
			metrics.ReportKafkaOffsets(msg.Offset, kafkaReader.Lag())
			metrics.ReportProcessed(err == nil)
			if err != nil {
				fmt.Printf("error at offset %d: %s", msg.Offset, err)
			}
		}
	}

	return nil
}

// The event log streamer uses this message format on the Kafka topic
type Payload struct {
	Time    string `json:"time"`
	Message string `json:"message"` // JSON data inside a string
	Cluster string `json:"cluster"`
	Tenant  string `json:"tenant"`
}
