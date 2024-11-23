FROM golang:1.22.7-bookworm

RUN --mount=type=cache,target=/var/cache/apt \
    apt-get update && apt-get install -y build-essential

RUN go install github.com/mitranim/gow@latest

WORKDIR /usr/src/app

COPY go.* .

RUN go mod tidy
RUN go mod verify
RUN go mod download

COPY . .