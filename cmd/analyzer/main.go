package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Config struct {
	Kafka      KafkaConfig
	Kubeconfig string
}

type KafkaConfig struct {
	CaPath          string
	CertificatePath string
	PrivateKeyPath  string
	Brokers         string
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

	<-ctx.Done()
	return nil
}
