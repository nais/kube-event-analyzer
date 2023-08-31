package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nais/kube-event-relay/pkg/event"
)

const (
	LabelKind      = "kind"
	LabelReason    = "reason"
	LabelNamespace = "namespace"
	LabelName      = "name"
	LabelSeverity  = "severity"
)

var (
	events = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   "nais",
		Subsystem:   "",
		Name:        "kube_events",
		Help:        "",
		ConstLabels: nil,
	}, []string{LabelNamespace, LabelKind, LabelReason, LabelSeverity})
)

func ReportEvent(ev event.KubeEvent) {
	events.With(prometheus.Labels{
		LabelReason:    ev.Event.Reason,
		LabelSeverity:  ev.Event.Type,
		LabelKind:      ev.Event.InvolvedObject.Kind,
		LabelNamespace: ev.Event.InvolvedObject.Namespace,
		//LabelName:      ev.Event.InvolvedObject.Name,
	}).Inc()
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func init() {
	prometheus.MustRegister(events)
}
