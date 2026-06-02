FROM golang:1.26-alpine AS build

WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/fitnes-api ./cmd/fitness-platform

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=build /out/fitnes-api /app/fitnes-api
COPY web/templates /app/web/templates
COPY migrations /app/migrations

EXPOSE 8080

CMD ["/app/fitnes-api"]
