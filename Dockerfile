FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN go build -o encore-migrator cmd/migrate/migrate.go

FROM alpine:latest

COPY --from=builder /app/encore-migrator /encore-migrator

ENTRYPOINT ["/encore-migrator"]