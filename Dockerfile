# Build a minimal Go binary and provide Docker image
FROM golang:1.21-alpine AS build
WORKDIR /src
COPY . .
RUN apk add --no-cache git build-base
RUN go build -o /app/gallery main.go

FROM alpine:3.18
RUN apk add --no-cache ca-certificates
COPY --from=build /app/gallery /gallery
COPY static /static
COPY templates /templates
VOLUME ["/uploads"]
EXPOSE 8080
ENTRYPOINT ["/gallery"]
