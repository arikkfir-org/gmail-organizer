# syntax=docker/dockerfile:1

FROM golang:1.25.1-alpine AS builder
ENV CGO_ENABLED=0
ENV GOOS=linux
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o ./dispatcher ./cmd/dispatcher

FROM gcr.io/distroless/base:nonroot@sha256:06c713c675e983c5aea030592b1d635954218d29c4db2f8ec66912da1b87e228 AS dispatcher
WORKDIR /
COPY --from=builder /app/dispatcher /usr/local/bin/dispatcher
USER 65532:65532
ENV GOTRACEBACK=single
ENTRYPOINT ["/usr/local/bin/dispatcher"]
