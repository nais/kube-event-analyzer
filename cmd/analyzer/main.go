package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/segmentio/kafka-go"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Config struct {
	Kafka      KafkaConfig
	Topic      string
	Kubeconfig string
}

type KafkaConfig struct {
	CaPath          string   `envconfig:"KAFKA_CA_PATH"`
	CertificatePath string   `envconfig:"KAFKA_CERTIFICATE_PATH"`
	PrivateKeyPath  string   `envconfig:"KAFKA_PRIVATE_KEY_PATH"`
	Brokers         []string `envconfig:"KAFKA_BROKERS"`
}

func main() {
	err := run()
	if err != nil {
		fmt.Printf("exiting because of error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := &Config{
		Kubeconfig: filepath.Join(homedir.HomeDir(), ".kube", "config"),
	}
	envconfig.MustProcess("", cfg)

	kubeClientConfig, err := clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	if err != nil {
		return err
	}
	kubeClientSet := kubernetes.NewForConfigOrDie(kubeClientConfig)
	_ = kubeClientSet

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT)
	defer cancel()

	cert, err := tls.LoadX509KeyPair(cfg.Kafka.CertificatePath, cfg.Kafka.PrivateKeyPath)
	if err != nil {
		return err
	}

	caCertPemFile, err := os.ReadFile(cfg.Kafka.CaPath)
	if err != nil {
		return err
	}
	caCertBlock, _ := pem.Decode(caCertPemFile)
	if caCertBlock == nil {
		return fmt.Errorf("unable to parse CA certificate file")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AddCert(caCert)

	dialer := &kafka.Dialer{
		Timeout: 5 * time.Second,
		TLS: &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            caCertPool,
			InsecureSkipVerify: false,
		},
	}

	kafkaReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     cfg.Kafka.Brokers,
		Topic:       cfg.Topic,
		Partition:   0,
		Dialer:      dialer,
		StartOffset: kafka.FirstOffset,
		MaxBytes:    10e6,
	})

	fmt.Printf("Kafka reader is configured.\n")

	defer func() {
		e := kafkaReader.Close()
		if e != nil {
			fmt.Printf("close Kafka reader: %s\n", e)
		} else {
			fmt.Printf("Kafka reader closed successfully.\n")
		}
	}()

	var totalCount int
	var totalBytes int
	var startTime = time.Now()

	reportEvents := func() {
		fmt.Printf("\r%d events (%d bytes) in %s", totalCount, totalBytes, time.Now().Sub(startTime))
	}

	var msg kafka.Message
	payload := &Payload{}
	kubeEvent := &KubeEvent{}
	counters := make(map[string]int)
	ticker := time.NewTicker(250 * time.Millisecond)

	defer func() {
		fmt.Println()
		reportEvents()
		fmt.Println()
		fmt.Println("Event summary")
		fmt.Println("=============")
		keys := make([]string, 0, len(counters))
		for k := range counters {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return counters[keys[i]] < counters[keys[j]]
		})
		for _, k := range keys {
			fmt.Printf("%30s: %8d\n", k, counters[k])
		}
	}()

	ingestMessage := func(msg kafka.Message) error {
		totalCount++
		totalBytes += len(msg.Value)

		err = json.Unmarshal(msg.Value, payload)
		if err != nil {
			return fmt.Errorf("offset %08d: parse message data: %w", msg.Offset, err)
		}

		err = json.Unmarshal([]byte(payload.Message), kubeEvent)
		if err != nil {
			return fmt.Errorf("offset %08d: parse Kubernetes event: %w", msg.Offset, err)
		}

		counters[kubeEvent.Event.Reason]++

		return nil
	}

	fmt.Printf("Waiting for messages...\n")

	for ctx.Err() == nil {
		select {
		case <-ticker.C:
			reportEvents()
		case <-ctx.Done():
			return nil
		default:
			msg, err = kafkaReader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() == nil {
					return err
				}
				return nil
			}
			err = ingestMessage(msg)
			if err != nil {
				fmt.Println() // break up status counter
				fmt.Println(err)
			}
		}
	}

	return nil
}

type Payload struct {
	Time    string `json:"time"`
	Message string `json:"message"` // JSON data inside a string
	Cluster string `json:"cluster"`
	Tenant  string `json:"tenant"`
}

type KubeEvent struct {
	Verb  string `json:"verb"`
	Event struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			Uid               string    `json:"uid"`
			ResourceVersion   string    `json:"resourceVersion"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
			ManagedFields     []struct {
				Manager    string    `json:"manager"`
				Operation  string    `json:"operation"`
				ApiVersion string    `json:"apiVersion"`
				Time       time.Time `json:"time"`
			} `json:"managedFields"`
		} `json:"metadata"`
		InvolvedObject struct {
			Kind            string `json:"kind"`
			Namespace       string `json:"namespace"`
			Name            string `json:"name"`
			Uid             string `json:"uid"`
			ApiVersion      string `json:"apiVersion"`
			ResourceVersion string `json:"resourceVersion"`
		} `json:"involvedObject"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
		Source  struct {
			Component string `json:"component"`
			Host      string `json:"host"`
		} `json:"source"`
		FirstTimestamp     time.Time   `json:"firstTimestamp"`
		LastTimestamp      time.Time   `json:"lastTimestamp"`
		Count              int         `json:"count"`
		Type               string      `json:"type"`
		EventTime          interface{} `json:"eventTime"`
		ReportingComponent string      `json:"reportingComponent"`
		ReportingInstance  string      `json:"reportingInstance"`
	} `json:"event"`
	OldEvent struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			Uid               string    `json:"uid"`
			ResourceVersion   string    `json:"resourceVersion"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
			ManagedFields     []struct {
				Manager    string    `json:"manager"`
				Operation  string    `json:"operation"`
				ApiVersion string    `json:"apiVersion"`
				Time       time.Time `json:"time"`
			} `json:"managedFields"`
		} `json:"metadata"`
		InvolvedObject struct {
			Kind            string `json:"kind"`
			Namespace       string `json:"namespace"`
			Name            string `json:"name"`
			Uid             string `json:"uid"`
			ApiVersion      string `json:"apiVersion"`
			ResourceVersion string `json:"resourceVersion"`
		} `json:"involvedObject"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
		Source  struct {
			Component string `json:"component"`
			Host      string `json:"host"`
		} `json:"source"`
		FirstTimestamp     time.Time   `json:"firstTimestamp"`
		LastTimestamp      time.Time   `json:"lastTimestamp"`
		Count              int         `json:"count"`
		Type               string      `json:"type"`
		EventTime          interface{} `json:"eventTime"`
		ReportingComponent string      `json:"reportingComponent"`
		ReportingInstance  string      `json:"reportingInstance"`
	} `json:"old_event"`
}
