# kube-event-relay

## Purpose

Relay Kubernetes events into a Kafka topic.

Having a stream of events readily available events may enable further projects such as:

* Live error detection can indicate cluster health.
* Live debugging of anomalies in team namespaces.
* Aggregated data reporting with most common errors.

## Architecture

The application streams cluster events from Kubernetes, and applies filtering as needed.
It should be possible to get Events as Protobuf directly from K8s.
The filtered events are sent out on a Kafka topic.
Data rate could be as much as 10 kB/s for the prod-gcp cluster, which amounts to about 864 MB/day.
Retention times can be set to one day to a couple of months, depending on required usage.