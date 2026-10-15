# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mail-daily-digest .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/mail-daily-digest /usr/local/bin/mail-daily-digest

ENTRYPOINT ["mail-daily-digest"]
