FROM golang:1 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /intercom-server .

FROM gcr.io/distroless/static
COPY --from=build /intercom-server /intercom-server
WORKDIR /data
ENTRYPOINT ["/intercom-server", "-config", "/data/intercom-config.json"]
