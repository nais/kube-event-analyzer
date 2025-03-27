FROM golang:1.24-alpine as builder
RUN apk add --no-cache git make curl build-base
ENV GOOS=linux
WORKDIR /src
COPY . /src/
RUN go mod download
RUN go build -o bin/kube-event-metric-exporter cmd/metric-exporter/*.go

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
RUN export PATH=$PATH:/app
WORKDIR /app
COPY --from=builder /src/bin/kube-event-metric-exporter /app/kube-event-metric-exporter
CMD ["/app/kube-event-metric-exporter"]
