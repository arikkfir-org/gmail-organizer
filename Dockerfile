# syntax=docker/dockerfile:1

# Stage 1: download modules (cache layer)
FROM golang:1.24-alpine AS deps
ENV CGO_ENABLED=0
ENV GOOS=linux
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Stage 2: build the sync binary
FROM golang:1.24-alpine AS build-sync
ENV CGO_ENABLED=0
ENV GOOS=linux
COPY --from=deps /go /go
WORKDIR /app
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build go build -o ./sync ./cli

# Stage 3: final runtime image
FROM gcr.io/distroless/base:nonroot AS sync
WORKDIR /
COPY --from=build-sync /app/sync /usr/local/bin/sync
USER 65532:65532
ENV GOTRACEBACK=single
ENTRYPOINT ["/usr/local/bin/sync"]
