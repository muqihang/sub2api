FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -buildid=" -o /out/nexus-agent-guard .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/nexus-agent-guard /nexus-agent-guard
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/nexus-agent-guard"]
