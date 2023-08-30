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
		for k, v := range counters {
			fmt.Printf("%30s: %8d\n", k, v)
		}
	}()

	fmt.Printf("Waiting for messages...\n")

	for ctx.Err() == nil {
		select {
		case <-ticker.C:
			reportEvents()
		case <-ctx.Done():
			return nil
		default:
			msg, err := kafkaReader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() == nil {
					return err
				}
				return nil
			}
			totalCount++
			totalBytes += len(msg.Value)

			err = json.Unmarshal(msg.Value, payload)
			if err != nil {
				fmt.Printf("\noffset %08d: parse message data: %s\n", msg.Offset, err)
				continue
			}

			err = json.Unmarshal([]byte(payload.Message), kubeEvent)
			if err != nil {
				fmt.Printf("\noffset %08d: parse Kubernetes event: %s\n", msg.Offset, err)
				continue
			}

			counters[kubeEvent.Event.Reason]++

			//fmt.Printf("msg %08d: %s\n", msg.Offset, kubeEvent.Event.Message)
			//fmt.Printf("msg %08d: %d bytes\n", msg.Offset, len(msg.Value))
			//fmt.Printf("msg %08d: %s\n", msg.Offset, string(msg.Value))
			//err = kafkaReader.CommitMessages(ctx, msg)
			//if err != nil {
			//return err
			//}
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
