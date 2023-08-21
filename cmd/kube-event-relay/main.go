package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Config struct {
	Kubeconfig string
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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT)
	defer cancel()

	watcher, err := kubeClientSet.EventsV1().Events("").Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	var totalCount int
	var totalBytes int
	var startTime = time.Now()

	defer func() {
		fmt.Printf("\n%d events (%d bytes) in %s\n", totalCount, totalBytes, time.Now().Sub(startTime))
	}()

	for event := range watcher.ResultChan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		totalCount++

		var rawData []byte
		var written int

		rawData, err = json.Marshal(event)
		if err != nil {
			return err
		}
		written, err = os.Stdout.Write(rawData)
		os.Stdout.Write([]byte("\n"))
		totalBytes += written
	}

	return nil
}
