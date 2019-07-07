FROM golang:1.12 as builder
WORKDIR /src
COPY . .
# Statically link binary, from https://github.com/golang/go/issues/9344#issuecomment-156317219
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-s' -o gbfs_exporter

FROM alpine:latest
RUN apk --no-cache add ca-certificates

COPY --from=builder /src/gbfs_exporter /bin/

EXPOSE      9607
ENTRYPOINT  ["/bin/gbfs_exporter"]
