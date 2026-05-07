FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o imei_checker ./backend/cmd/service/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/imei_checker .
COPY ./backend/configs ./backend/configs
EXPOSE 8080
CMD ["./imei_checker"]