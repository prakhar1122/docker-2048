FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /api main.go

FROM alpine:3.20
COPY --from=builder /api /api
EXPOSE 8080
CMD ["/api"]
