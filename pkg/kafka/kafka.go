package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/segmentio/kafka-go"
)

type ReaderOpts struct {
	CertificatePath string
	PrivateKeyPath  string
	CaPath          string
	Topic           string
	Brokers         []string
	GroupID         string
	DialTimeout     time.Duration
}

// Initialize a Kafka reader and authenticate with TLS client certificate.
func NewReader(cfg ReaderOpts) (*kafka.Reader, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertificatePath, cfg.PrivateKeyPath)
	if err != nil {
		return nil, err
	}

	caCertPemFile, err := os.ReadFile(cfg.CaPath)
	if err != nil {
		return nil, err
	}
	caCertBlock, _ := pem.Decode(caCertPemFile)
	if caCertBlock == nil {
		return nil, fmt.Errorf("PEM data not found in CA certificate file %s", caCertPemFile)
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AddCert(caCert)

	dialer := &kafka.Dialer{
		Timeout: cfg.DialTimeout,
		TLS: &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            caCertPool,
			InsecureSkipVerify: false,
		},
	}

	kafkaReader := kafka.NewReader(kafka.ReaderConfig{
		GroupID:     cfg.GroupID,
		Brokers:     cfg.Brokers,
		Topic:       cfg.Topic,
		Partition:   0,
		Dialer:      dialer,
		StartOffset: kafka.LastOffset,
		MaxBytes:    10e6,
	})

	return kafkaReader, nil
}
