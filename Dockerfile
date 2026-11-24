# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/resumo-planilhas ./cmd/resumoplanilhas

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=America/Manaus \
    SERVER_MODE=true \
    PORT=8080 \
    DATA_DIR=/data

WORKDIR /app
COPY --from=builder /out/resumo-planilhas /app/resumo-planilhas

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/app/resumo-planilhas"]
