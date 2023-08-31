package event

import (
	"time"
)

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
		InvolvedObject InvolvedObject `json:"involvedObject"`
		Reason         string         `json:"reason"`
		Message        string         `json:"message"`
		Source         struct {
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
}

type InvolvedObject struct {
	Kind            string `json:"kind"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Uid             string `json:"uid"`
	ApiVersion      string `json:"apiVersion"`
	ResourceVersion string `json:"resourceVersion"`
}
