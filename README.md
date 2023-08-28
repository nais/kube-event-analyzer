# kube-event-analyzer

## Purpose

Analyze Kubernetes events coming through a Kafka topic from all NAIS clusters.

Having a stream of events readily available events may enable further projects such as:

* Live error detection can indicate cluster health.
* Live debugging of anomalies in team namespaces.
* Aggregated data reporting with most common errors.

## Architecture

The application connects to the `nav-infrastructure` Kafka cluster and streams events from there.
