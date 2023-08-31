package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/segmentio/kafka-go"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/nais/kube-event-relay/pkg/event"
	"github.com/nais/kube-event-relay/pkg/metrics"
)

type Config struct {
	Kafka          KafkaConfig
	Topic          string
	Kubeconfig     string
	MetricsAddress string `envconfig:"METRICS_ADDRESS" default:"127.0.0.1:8675"`
}

type KafkaConfig struct {
	CaPath          string   `envconfig:"KAFKA_CA_PATH"`
	CertificatePath string   `envconfig:"KAFKA_CERTIFICATE_PATH"`
	PrivateKeyPath  string   `envconfig:"KAFKA_PRIVATE_KEY_PATH"`
	Brokers         []string `envconfig:"KAFKA_BROKERS"`
}

type Filter struct {
	Name      regexp.Regexp
	Namespace regexp.Regexp
}

type Filters []Filter

func (f Filter) Match(event event.KubeEvent) bool {
	return f.Namespace.MatchString(event.Event.InvolvedObject.Namespace) && f.Name.MatchString(event.Event.InvolvedObject.Name)
}

func (filters Filters) MatchAny(event event.KubeEvent) bool {
	for _, f := range filters {
		if f.Match(event) {
			return true
		}
	}
	return false
}

func mockFilters() Filters {
	return Filters{
		{
			Name:      deref(regexp.MustCompilePOSIX(`^gemini-?`)),
			Namespace: deref(regexp.MustCompilePOSIX(`^aura$`)),
		},
	}
}

func deref[T any](x *T) T {
	return *x
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

	go func() {
		fmt.Printf("Starting metrics server on %s...\n", cfg.MetricsAddress)
		metricsHandler := metrics.Handler()
		err := http.ListenAndServe(cfg.MetricsAddress, metricsHandler)
		// ListenAndServe always returns a non-nil error. After Shutdown or Close,
		// the returned error is ErrServerClosed.
		cancel()
		if err == http.ErrServerClosed {
			return
		}
		fmt.Printf("metrics handler: %s\n", err)
	}()

	var totalCount int
	var totalBytes int
	var acceptedCount int
	var startTime = time.Now()

	reportEvents := func() {
		totalEventsInTopic := totalCount + int(kafkaReader.Lag())
		percentComplete := float64(totalCount) / float64(totalEventsInTopic) * 100
		fmt.Printf(
			"\r[%5.1f%%] %d/%d/%d events (%.1f MB) in %s",
			percentComplete,
			acceptedCount,
			totalCount,
			totalEventsInTopic,
			float64(totalBytes)/1024/1024,
			time.Now().Sub(startTime).Truncate(time.Millisecond*100),
		)
	}

	var msg kafka.Message
	counters := make(map[string]int)
	ticker := time.NewTicker(250 * time.Millisecond)
	trackedResources := make(map[event.InvolvedObject]History)

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
			fmt.Printf("%35s: %8d\n", k, counters[k])
		}

		fmt.Println()
		fmt.Println("Tracked object summary")
		fmt.Println("======================")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		for k, v := range trackedResources {
			for i := range v {
				fmt.Printf("%s %s/%s [%04d] ", k.Kind, k.Namespace, k.Name, i)
				enc.Encode(v[i])
			}
		}

	}()

	filters := mockFilters()

	ingestMessage := func(msg kafka.Message) error {
		totalCount++
		totalBytes += len(msg.Value)

		payload := &Payload{}
		err = json.Unmarshal(msg.Value, payload)
		if err != nil {
			return fmt.Errorf("offset %08d: parse message data: %w", msg.Offset, err)
		}

		kubeEvent := &event.KubeEvent{}
		err = json.Unmarshal([]byte(payload.Message), kubeEvent)
		if err != nil {
			return fmt.Errorf("offset %08d: parse Kubernetes event: %w", msg.Offset, err)
		}

		metrics.ReportEvent(*kubeEvent)

		return nil

		if !filters.MatchAny(*kubeEvent) {
			return nil
		}
		acceptedCount++

		fmt.Printf(
			"\r%s %s %s: %s -> %s\n",
			kubeEvent.Event.LastTimestamp.Format(time.RFC3339),
			kubeEvent.Event.InvolvedObject.Kind,
			kubeEvent.Event.InvolvedObject.Name,
			kubeEvent.Event.Reason,
			kubeEvent.Event.Message,
		)

		counters[kubeEvent.Event.Reason]++
		history, ok := trackedResources[kubeEvent.Event.InvolvedObject]
		if !ok {
			history = make(History, 0)
		}
		history = append(history, *kubeEvent)
		trackedResources[kubeEvent.Event.InvolvedObject] = history

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

// The event log streamer uses this message format on the Kafka topic
type Payload struct {
	Time    string `json:"time"`
	Message string `json:"message"` // JSON data inside a string
	Cluster string `json:"cluster"`
	Tenant  string `json:"tenant"`
}

type History []event.KubeEvent
