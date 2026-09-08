# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/api /out/migrate /out/worker /app/
COPY migrations /app/migrations
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app/api"]
