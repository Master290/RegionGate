FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/regiongate ./cmd/regiongate

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S regiongate \
    && adduser -S -G regiongate regiongate
COPY --from=build /out/regiongate /usr/local/bin/regiongate

USER regiongate
EXPOSE 25565
ENTRYPOINT ["/usr/local/bin/regiongate"]
