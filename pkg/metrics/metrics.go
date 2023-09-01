package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nais/kube-event-relay/pkg/event"
)

const (
	LabelKind             = "kind"
	LabelReason           = "reason"
	LabelNamespace        = "namespace"
	LabelSeverity         = "severity"
	LabelProcessingStatus = "status"
)

var (
	events = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "nais",
		Subsystem: "kube_events",
		Name:      "count",
		Help:      "Number of Kubernetes events processed, labeled by namespace, kind, reason and severity",
	}, []string{LabelNamespace, LabelKind, LabelReason, LabelSeverity})

	kafkalag = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "nais",
		Subsystem: "kube_events",
		Name:      "kafka_lag",
		Help:      "How many messages are left to process from the Kafka topic",
	})

	offset = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "nais",
		Subsystem: "kube_events",
		Name:      "kafka_offset",
		Help:      "Offset of most recent message processed from Kafka topic",
	})

	processed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "nais",
		Subsystem: "kube_events",
		Name:      "processed",
		Help:      "Number of processed messages, labeled by status",
	}, []string{LabelProcessingStatus})
)

func ReportEvent(ev event.KubeEvent) {
	events.With(prometheus.Labels{
		LabelReason:    ev.Event.Reason,
		LabelSeverity:  ev.Event.Type,
		LabelKind:      ev.Event.InvolvedObject.Kind,
		LabelNamespace: ev.Event.InvolvedObject.Namespace,
	}).Inc()
}

func ReportKafkaOffsets(current, lag int64) {
	offset.Set(float64(current))
	kafkalag.Set(float64(lag))
}

func ReportProcessed(success bool) {
	var status string
	if success {
		status = "ok"
	} else {
		status = "error"
	}
	processed.With(prometheus.Labels{
		LabelProcessingStatus: status,
	}).Inc()
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func init() {
	prometheus.MustRegister(
		events,
		kafkalag,
		offset,
		processed,
	)
}
